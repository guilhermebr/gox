//go:build integration

package s3_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/storage"
	"github.com/guilhermebr/gox/providers/s3"
)

// Needs an S3-compatible server and an existing bucket, for example MinIO:
//
//	S3_TEST_ENDPOINT=http://127.0.0.1:9000 S3_TEST_BUCKET=gox-test S3_TEST_ACCESS_KEY_ID=minioadmin S3_TEST_SECRET_ACCESS_KEY=minioadmin
func bucket(t *testing.T) storage.Bucket {
	t.Helper()
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_TEST_ENDPOINT not set")
	}
	setArgs(t)
	t.Setenv("SHOP_S3_ENDPOINT", endpoint)
	t.Setenv("SHOP_S3_BUCKET", os.Getenv("S3_TEST_BUCKET"))
	t.Setenv("SHOP_S3_ACCESS_KEY_ID", os.Getenv("S3_TEST_ACCESS_KEY_ID"))
	t.Setenv("SHOP_S3_SECRET_ACCESS_KEY", os.Getenv("S3_TEST_SECRET_ACCESS_KEY"))
	a, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), s3.Enable())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	for !a.Health().IsReady() {
		select {
		case err := <-done:
			t.Fatalf("the app stopped: %v", err)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return s3.From(a)
}

func TestObjectsRoundTrip(t *testing.T) {
	b := bucket(t)
	ctx := context.Background()
	key := "tests/round-trip.txt"
	body := []byte("hello object storage")
	if err := b.Put(ctx, key, bytes.NewReader(body), int64(len(body)), storage.PutOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	info, err := b.Stat(ctx, key)
	if err != nil || info.Size != int64(len(body)) || info.ContentType != "text/plain" || info.ETag == "" || info.LastModified.IsZero() {
		t.Fatalf("Stat = %+v %v", info, err)
	}
	r, _, err := b.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	_ = r.Close()
	if !bytes.Equal(got, body) {
		t.Fatalf("Get = %q", got)
	}
	if err := b.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Stat(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Stat after Delete = %v", err)
	}
	if _, _, err := b.Get(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get after Delete = %v", err)
	}
}

func put(t *testing.T, url string, headers http.Header, body []byte) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	req.Header = headers.Clone()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestDirectUploadIsBoundToItsChecksumAndType(t *testing.T) {
	b := bucket(t)
	ctx := context.Background()
	key := "tests/direct-upload.pdf"
	body := []byte("%PDF-1.7 pretend")
	sum := md5.Sum(body)
	url, headers, err := b.PresignPut(ctx, key, storage.PresignPutOptions{
		ContentType: "application/pdf", ContentMD5: base64.StdEncoding.EncodeToString(sum[:]), ContentLength: int64(len(body)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if headers.Get("Content-Type") != "application/pdf" || headers.Get("Content-MD5") == "" {
		t.Fatalf("the client must be told which headers to send: %v", headers)
	}
	if code := put(t, url, headers, []byte("something else entirely")); code < 400 {
		t.Fatalf("another body was accepted: %d", code)
	}
	if code := put(t, url, headers, body); code != http.StatusOK {
		t.Fatalf("upload = %d", code)
	}
	if info, err := b.Stat(ctx, key); err != nil || info.Size != int64(len(body)) {
		t.Fatalf("Stat = %+v %v", info, err)
	}

	link, err := b.PresignGet(ctx, key, storage.PresignGetOptions{Filename: `lease "final".pdf`, Expires: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, body) {
		t.Fatalf("download = %d %q", resp.StatusCode, got)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") || !strings.Contains(cd, "lease") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	_ = b.Delete(ctx, key)
}

func TestAMissingBucketFailsTheBoot(t *testing.T) {
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_TEST_ENDPOINT not set")
	}
	setArgs(t)
	t.Setenv("SHOP_S3_ENDPOINT", endpoint)
	t.Setenv("SHOP_S3_BUCKET", "gox-no-such-bucket")
	t.Setenv("SHOP_S3_ACCESS_KEY_ID", os.Getenv("S3_TEST_ACCESS_KEY_ID"))
	t.Setenv("SHOP_S3_SECRET_ACCESS_KEY", os.Getenv("S3_TEST_SECRET_ACCESS_KEY"))
	a, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), s3.Enable())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.RunContext(ctx); err == nil || !strings.Contains(err.Error(), "gox-no-such-bucket") {
		t.Fatalf("RunContext = %v", err)
	}
}
