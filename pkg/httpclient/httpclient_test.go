package httpclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/guilhermebr/gox/pkg/httpclient"
	"github.com/guilhermebr/gox/pkg/log"
)

func TestDefaultHeadersAndRequestIDPropagation(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Header.Clone() }))
	defer srv.Close()

	c := httpclient.New(httpclient.Config{Timeout: time.Second}, httpclient.WithUserAgent("billing/1.2.3"))
	if c.Timeout != time.Second {
		t.Fatalf("Timeout = %v", c.Timeout)
	}
	ctx := log.WithRequestID(context.Background(), "req-77")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got.Get("User-Agent") != "billing/1.2.3" {
		t.Fatalf("User-Agent = %q", got.Get("User-Agent"))
	}
	if got.Get("X-Request-ID") != "req-77" {
		t.Fatalf("X-Request-ID = %q; the request id must propagate downstream", got.Get("X-Request-ID"))
	}
}

func TestTraceContextIsInjected(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.Header.Clone() }))
	defer srv.Close()

	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()
	c := httpclient.New(httpclient.Config{Timeout: time.Second},
		httpclient.WithTracerProvider(tp), httpclient.WithPropagator(propagation.TraceContext{}))

	ctx, span := tp.Tracer("t").Start(context.Background(), "parent")
	defer span.End()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got.Get("traceparent") == "" {
		t.Fatal("traceparent header missing; outbound calls must carry trace context")
	}
}

func TestWithBearerDerivesAClientWithoutMutatingTheShared(t *testing.T) {
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
	}))
	defer srv.Close()

	shared := httpclient.New(httpclient.Config{Timeout: time.Second})
	asUser := httpclient.WithBearer(shared, "tok-user-1")
	if asUser == shared {
		t.Fatal("WithBearer must return a new client")
	}

	for _, c := range []*http.Client{asUser, shared} {
		resp, err := c.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	if auths[0] != "Bearer tok-user-1" {
		t.Fatalf("derived client sent %q", auths[0])
	}
	if auths[1] != "" {
		t.Fatalf("shared client leaked the token: %q", auths[1])
	}
}

func TestRetryIsOptInAndOnlyForIdempotentRequests(t *testing.T) {
	newServer := func() (*httptest.Server, *atomic.Int32) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		return srv, &calls
	}

	t.Run("no retry by default", func(t *testing.T) {
		srv, calls := newServer()
		defer srv.Close()
		resp, err := httpclient.New(httpclient.Config{Timeout: time.Second}).Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable || calls.Load() != 1 {
			t.Fatalf("status=%d calls=%d", resp.StatusCode, calls.Load())
		}
	})
	t.Run("GET is retried when enabled", func(t *testing.T) {
		srv, calls := newServer()
		defer srv.Close()
		c := httpclient.New(httpclient.Config{Timeout: time.Second}, httpclient.WithRetry(3, time.Millisecond))
		resp, err := c.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || calls.Load() != 3 {
			t.Fatalf("status=%d calls=%d", resp.StatusCode, calls.Load())
		}
	})
	t.Run("POST is never retried", func(t *testing.T) {
		srv, calls := newServer()
		defer srv.Close()
		c := httpclient.New(httpclient.Config{Timeout: time.Second}, httpclient.WithRetry(3, time.Millisecond))
		resp, err := c.Post(srv.URL, "text/plain", nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable || calls.Load() != 1 {
			t.Fatalf("status=%d calls=%d", resp.StatusCode, calls.Load())
		}
	})
}
