package s3_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/storage"
	"github.com/guilhermebr/gox/providers/s3"
)

// fakeS3 answers every call with 200 and keeps the requests it saw.
type fakeS3 struct {
	mu   sync.Mutex
	reqs []*http.Request
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.reqs = append(f.reqs, r.Clone(context.Background()))
	f.mu.Unlock()
	w.Header().Set("ETag", `"etag"`)
	w.WriteHeader(http.StatusOK)
}

// last is the most recent request with the method.
func (f *fakeS3) last(t *testing.T, method string) *http.Request {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.reqs) - 1; i >= 0; i-- {
		if f.reqs[i].Method == method {
			return f.reqs[i]
		}
	}
	t.Fatalf("no %s request reached the server", method)
	return nil
}

// bucketAgainst runs a service whose bucket is served by handler and returns it.
func bucketAgainst(t *testing.T, handler http.Handler, env map[string]string) storage.Bucket {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	setArgs(t)
	t.Setenv("SHOP_S3_ENDPOINT", srv.URL)
	t.Setenv("SHOP_S3_BUCKET", "docs")
	t.Setenv("SHOP_S3_ACCESS_KEY_ID", "AKIA")
	t.Setenv("SHOP_S3_SECRET_ACCESS_KEY", "secret")
	for k, v := range env {
		t.Setenv(k, v)
	}
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

func signedHeaders(t *testing.T, r *http.Request) string {
	t.Helper()
	auth := r.Header.Get("Authorization")
	i := strings.Index(auth, "SignedHeaders=")
	if i < 0 {
		t.Fatalf("Authorization = %q", auth)
	}
	rest := auth[i+len("SignedHeaders="):]
	if j := strings.Index(rest, ","); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// Google Cloud Storage appends ",gzip(gfe)" to Accept-Encoding before it
// checks the signature, and refuses the SDK's x-amz-checksum headers.
func TestGCSRequestsLeaveAcceptEncodingUnsignedAndSendNoChecksum(t *testing.T) {
	f := &fakeS3{}
	b := bucketAgainst(t, f, map[string]string{"SHOP_S3_COMPAT": "gcs"})

	head := f.last(t, http.MethodHead) // HeadBucket at start
	if sh := signedHeaders(t, head); strings.Contains(sh, "accept-encoding") {
		t.Errorf("HeadBucket signed Accept-Encoding: %s", sh)
	}
	if got := head.Header.Get("Accept-Encoding"); got != "identity" {
		t.Errorf("HeadBucket Accept-Encoding = %q, want identity so the client never gunzips objects", got)
	}

	if err := b.Put(context.Background(), "a.txt", strings.NewReader("hello"), 5, storage.PutOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	put := f.last(t, http.MethodPut)
	if sh := signedHeaders(t, put); strings.Contains(sh, "accept-encoding") {
		t.Errorf("PutObject signed Accept-Encoding: %s", sh)
	}
	for name := range put.Header {
		if strings.HasPrefix(strings.ToLower(name), "x-amz-checksum") {
			t.Errorf("PutObject sent %s, which GCS rejects", name)
		}
	}
	if _, _, err := b.PresignPut(context.Background(), "b.txt", storage.PresignPutOptions{}); err != nil {
		t.Fatalf("PresignPut: %v", err)
	}
}

func TestS3RequestsKeepTheSDKDefaults(t *testing.T) {
	f := &fakeS3{}
	b := bucketAgainst(t, f, nil)
	if err := b.Put(context.Background(), "a.txt", strings.NewReader("hello"), 5, storage.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	put := f.last(t, http.MethodPut)
	if sh := signedHeaders(t, put); !strings.Contains(sh, "accept-encoding") {
		t.Errorf("an S3 endpoint must keep the SDK's signing: %s", sh)
	}
	if put.Header.Get("X-Amz-Checksum-Crc32") == "" {
		t.Error("an S3 endpoint must keep the SDK's upload checksum")
	}
}

func TestCompatIsGCSForTheGoogleEndpoint(t *testing.T) {
	cfg := s3.Config{Endpoint: "https://storage.googleapis.com", Compat: "auto"}
	if !cfg.GCS() {
		t.Error("auto must pick gcs for storage.googleapis.com")
	}
	cfg.Endpoint = "http://localhost:9000"
	if cfg.GCS() {
		t.Error("auto must not pick gcs for MinIO")
	}
	cfg.Compat = "gcs"
	if !cfg.GCS() {
		t.Error("gcs must win over the endpoint")
	}
	cfg = s3.Config{Endpoint: "https://storage.googleapis.com", Compat: "s3"}
	if cfg.GCS() {
		t.Error("s3 must win over the endpoint")
	}
}
