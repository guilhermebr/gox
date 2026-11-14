// Package storagetest is an in-memory storage.Bucket for tests. Like S3, it
// refuses a direct upload whose type, size or MD5 differs from what the
// presigned URL was signed for, so a test exercises the same contract the
// browser meets in production.
package storagetest

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // S3 Content-MD5 is an integrity check, not security
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/guilhermebr/gox/pkg/storage"
)

// BaseURL prefixes every URL the bucket presigns; the key follows it.
const BaseURL = "https://bucket.test/"

// ErrUploadRefused is returned by Upload when the store would answer 403 or
// 400: no presigned URL for the key, or a body that breaks its constraints.
var ErrUploadRefused = errors.New("storagetest: upload refused")

type object struct {
	body        []byte
	contentType string
	modified    time.Time
}

// Bucket is an in-memory storage.Bucket, safe for concurrent use.
type Bucket struct {
	mu      sync.Mutex
	objects map[string]object
	signed  map[string]storage.PresignPutOptions
	deleted []string
	links   []storage.PresignGetOptions
}

var _ storage.Bucket = (*Bucket)(nil)

// New returns an empty bucket.
func New() *Bucket {
	return &Bucket{objects: map[string]object{}, signed: map[string]storage.PresignPutOptions{}}
}

// Put stores an object as a server-side write would.
func (b *Bucket) Put(_ context.Context, key string, r io.Reader, _ int64, o storage.PutOptions) error {
	body, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objects[key] = object{body: body, contentType: o.ContentType, modified: time.Now()}
	return nil
}

// Get returns a stored object, or storage.ErrNotFound.
func (b *Bucket) Get(_ context.Context, key string) (io.ReadCloser, storage.Info, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := b.objects[key]
	if !ok {
		return nil, storage.Info{}, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(o.body)), info(key, o), nil
}

// Stat describes a stored object, or returns storage.ErrNotFound.
func (b *Bucket) Stat(_ context.Context, key string) (storage.Info, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := b.objects[key]
	if !ok {
		return storage.Info{}, storage.ErrNotFound
	}
	return info(key, o), nil
}

func info(key string, o object) storage.Info {
	return storage.Info{Key: key, Size: int64(len(o.body)), ContentType: o.contentType, LastModified: o.modified}
}

// Delete removes an object. A missing key is not an error, as on S3.
func (b *Bucket) Delete(_ context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.objects, key)
	b.deleted = append(b.deleted, key)
	return nil
}

// PresignPut records what the URL allows and returns BaseURL+key with the
// headers the client must send.
func (b *Bucket) PresignPut(_ context.Context, key string, o storage.PresignPutOptions) (string, http.Header, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.signed[key] = o
	h := http.Header{}
	if o.ContentType != "" {
		h.Set("Content-Type", o.ContentType)
	}
	if o.ContentMD5 != "" {
		h.Set("Content-Md5", o.ContentMD5)
	}
	if o.ContentLength > 0 {
		h.Set("Content-Length", strconv.FormatInt(o.ContentLength, 10))
	}
	return BaseURL + key, h, nil
}

// PresignGet returns a fake link to the key and records its options.
func (b *Bucket) PresignGet(_ context.Context, key string, o storage.PresignGetOptions) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.links = append(b.links, o)
	return BaseURL + key + "?download=1", nil
}

// Upload is the client's PUT to a URL from PresignPut. It returns
// ErrUploadRefused when there is no presigned upload for the URL or when
// the Content-Type header, the body's size or its MD5 differs from a
// constraint the URL was signed with.
func (b *Bucket) Upload(url string, headers http.Header, body []byte) error {
	key := strings.TrimPrefix(url, BaseURL)
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := b.signed[key]
	switch {
	case !ok:
		return errors.Join(ErrUploadRefused, errors.New("403: no presigned upload for this URL"))
	case o.ContentType != "" && headers.Get("Content-Type") != o.ContentType,
		o.ContentLength > 0 && int64(len(body)) != o.ContentLength:
		return errors.Join(ErrUploadRefused, errors.New("403 SignatureDoesNotMatch"))
	case o.ContentMD5 != "" && Checksum(body) != o.ContentMD5:
		return errors.Join(ErrUploadRefused, errors.New("400 BadDigest"))
	}
	b.objects[key] = object{body: body, contentType: headers.Get("Content-Type"), modified: time.Now()}
	return nil
}

// Has reports whether key holds an object.
func (b *Bucket) Has(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.objects[key]
	return ok
}

// Deleted returns the keys passed to Delete, in order.
func (b *Bucket) Deleted() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.deleted...)
}

// Links returns the options of every PresignGet, in order.
func (b *Bucket) Links() []storage.PresignGetOptions {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]storage.PresignGetOptions(nil), b.links...)
}

// Checksum is the base64 MD5 a client declares for body in Content-MD5.
func Checksum(body []byte) string {
	sum := md5.Sum(body) //nolint:gosec // see import
	return base64.StdEncoding.EncodeToString(sum[:])
}
