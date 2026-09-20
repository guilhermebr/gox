package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/lifecycle"
	"github.com/guilhermebr/gox/pkg/storage"
)

type key struct{}

// Enable declares the bucket: it registers the S3 config section and a
// component that checks at boot that the bucket is reachable with the
// configured credentials (a typo fails the start, not the first upload)
// and keeps reporting it through /readyz.
func Enable() gox.Option {
	return func(b *gox.Builder) error {
		cfg := &Config{}
		b.ConfigSection("S3", cfg, "s3.Enable()")
		b.Component(gox.StageClient, func(a *gox.App) (lifecycle.Component, error) {
			load := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.Region)}
			if cfg.AccessKeyID != "" {
				load = append(load, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")))
			}
			if a.HasHTTPClient() {
				load = append(load, awsconfig.WithHTTPClient(a.HTTPClient()))
			}
			awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), load...)
			if err != nil {
				return nil, fmt.Errorf("s3: %w", err)
			}
			client := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
				o.UsePathStyle = cfg.pathStyle()
				if cfg.Endpoint != "" {
					o.BaseEndpoint = aws.String(cfg.Endpoint)
				}
			})
			bk := &bucket{cfg: cfg, client: client, presign: awss3.NewPresignClient(client)}
			b.Set(key{}, storage.Bucket(bk))
			return bk, nil
		})
		return nil
	}
}

// From returns the bucket. It panics if Enable was not passed to gox.New.
func From(a *gox.App) storage.Bucket {
	return gox.MustValue[storage.Bucket](a, key{}, "s3.From", "s3.Enable()")
}

type bucket struct {
	cfg     *Config
	client  *awss3.Client
	presign *awss3.PresignClient
}

func (b *bucket) Name() string { return "s3" }

func (b *bucket) Start(ctx context.Context) error { return b.Ready(ctx) }

func (b *bucket) Stop(context.Context) error { return nil }

// Ready reports whether the bucket answers with these credentials.
func (b *bucket) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := b.client.HeadBucket(ctx, &awss3.HeadBucketInput{Bucket: &b.cfg.Bucket}); err != nil {
		return fmt.Errorf("s3: bucket %q is not reachable: %w", b.cfg.Bucket, err)
	}
	return nil
}

func (b *bucket) Put(ctx context.Context, k string, r io.Reader, size int64, opts storage.PutOptions) error {
	in := &awss3.PutObjectInput{Bucket: &b.cfg.Bucket, Key: &k, Body: r}
	if size >= 0 {
		in.ContentLength = &size
	}
	if opts.ContentType != "" {
		in.ContentType = &opts.ContentType
	}
	if _, err := b.client.PutObject(ctx, in); err != nil {
		return fmt.Errorf("s3: put %s: %w", k, err)
	}
	return nil
}

func (b *bucket) Get(ctx context.Context, k string) (io.ReadCloser, storage.Info, error) {
	out, err := b.client.GetObject(ctx, &awss3.GetObjectInput{Bucket: &b.cfg.Bucket, Key: &k})
	if err != nil {
		return nil, storage.Info{}, b.wrap("get", k, err)
	}
	return out.Body, storage.Info{
		Key: k, Size: aws.ToInt64(out.ContentLength), ContentType: aws.ToString(out.ContentType),
		ETag: aws.ToString(out.ETag), LastModified: aws.ToTime(out.LastModified),
	}, nil
}

func (b *bucket) Stat(ctx context.Context, k string) (storage.Info, error) {
	out, err := b.client.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: &b.cfg.Bucket, Key: &k})
	if err != nil {
		return storage.Info{}, b.wrap("stat", k, err)
	}
	return storage.Info{
		Key: k, Size: aws.ToInt64(out.ContentLength), ContentType: aws.ToString(out.ContentType),
		ETag: aws.ToString(out.ETag), LastModified: aws.ToTime(out.LastModified),
	}, nil
}

func (b *bucket) Delete(ctx context.Context, k string) error {
	if _, err := b.client.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: &b.cfg.Bucket, Key: &k}); err != nil {
		return b.wrap("delete", k, err)
	}
	return nil
}

func (b *bucket) PresignPut(ctx context.Context, k string, opts storage.PresignPutOptions) (string, http.Header, error) {
	in := &awss3.PutObjectInput{Bucket: &b.cfg.Bucket, Key: &k}
	if opts.ContentType != "" {
		in.ContentType = &opts.ContentType
	}
	if opts.ContentMD5 != "" {
		in.ContentMD5 = &opts.ContentMD5
	}
	if opts.ContentLength > 0 {
		in.ContentLength = &opts.ContentLength
	}
	req, err := b.presign.PresignPutObject(ctx, in, awss3.WithPresignExpires(b.expiry(opts.Expires)))
	if err != nil {
		return "", nil, fmt.Errorf("s3: presign put %s: %w", k, err)
	}
	headers := req.SignedHeader.Clone()
	headers.Del("Host") // the client's HTTP stack sets it
	return req.URL, headers, nil
}

func (b *bucket) PresignGet(ctx context.Context, k string, opts storage.PresignGetOptions) (string, error) {
	in := &awss3.GetObjectInput{Bucket: &b.cfg.Bucket, Key: &k}
	if opts.Filename != "" {
		kind := "attachment"
		if opts.Inline {
			kind = "inline"
		}
		in.ResponseContentDisposition = aws.String(mime.FormatMediaType(kind, map[string]string{"filename": opts.Filename}))
	}
	if opts.ContentType != "" {
		in.ResponseContentType = &opts.ContentType
	}
	req, err := b.presign.PresignGetObject(ctx, in, awss3.WithPresignExpires(b.expiry(opts.Expires)))
	if err != nil {
		return "", fmt.Errorf("s3: presign get %s: %w", k, err)
	}
	return req.URL, nil
}

func (b *bucket) expiry(d time.Duration) time.Duration {
	if d <= 0 {
		return b.cfg.PresignExpiry
	}
	return d
}

// wrap turns the store's "no such key" answers into storage.ErrNotFound.
func (b *bucket) wrap(op, k string, err error) error {
	var noKey *types.NoSuchKey
	var notFound *types.NotFound
	var api smithy.APIError
	if errors.As(err, &noKey) || errors.As(err, &notFound) || (errors.As(err, &api) && (api.ErrorCode() == "NotFound" || api.ErrorCode() == "NoSuchKey")) {
		return fmt.Errorf("s3: %s %s: %w", op, k, storage.ErrNotFound)
	}
	return fmt.Errorf("s3: %s %s: %w", op, k, err)
}
