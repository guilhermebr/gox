package middleware_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox/pkg/middleware"
)

func TestRouteCaptureLetsAStreamingRouteOutliveTheTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /stream", middleware.Streaming(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !middleware.IsStreaming(r) {
			t.Error("IsStreaming = false inside a Streaming route")
		}
		if got := middleware.Route(r); got != "GET /stream" {
			t.Errorf("Route = %q", got)
		}
		time.Sleep(50 * time.Millisecond)
		if r.Context().Err() != nil {
			t.Errorf("stream context canceled: %v", r.Context().Err())
		}
		_, _ = w.Write([]byte("late"))
		_ = http.NewResponseController(w).Flush()
	})))
	mux.HandleFunc("GET /plain", func(_ http.ResponseWriter, r *http.Request) {
		if middleware.IsStreaming(r) {
			t.Error("IsStreaming = true for a plain route")
		}
		<-r.Context().Done()
	})
	h := middleware.Chain(middleware.RouteCapture(mux.Handler), middleware.Timeout(10*time.Millisecond))(mux)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "late" || !rec.Flushed {
		t.Fatalf("stream = %d %q flushed=%v", rec.Code, rec.Body, rec.Flushed)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/plain", nil))
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("plain = %d, want 504", rec.Code)
	}
}

// A Streaming marker hidden behind another handler would be buffered and cut
// by Timeout; it fails loudly with the fix instead.
func TestStreamingInsideAnotherHandlerPanicsWithTheFix(t *testing.T) {
	stream := middleware.Streaming(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("never"))
	}))
	requireRole := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) })
	}
	sub := http.NewServeMux()
	sub.Handle("GET /sub/stream", stream)

	mux := http.NewServeMux()
	mux.Handle("GET /wrapped", requireRole(stream))
	mux.Handle("GET /prefixed/", http.StripPrefix("/prefixed", stream))
	mux.Handle("/sub/", sub)
	h := middleware.RouteCapture(mux.Handler)(mux)

	for _, path := range []string{"/wrapped", "/prefixed/x", "/sub/stream"} {
		func() {
			defer func() {
				msg := fmt.Sprint(recover())
				if !strings.Contains(msg, "register middleware.Streaming(h) on the app's mux itself") {
					t.Errorf("%s: panic = %q, want one naming the fix", path, msg)
				}
			}()
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		}()
	}
}

func TestStreamingWorksWithoutRouteCapture(t *testing.T) {
	h := middleware.Streaming(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if middleware.IsStreaming(r) {
			t.Error("IsStreaming = true without RouteCapture")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q", rec.Body)
	}
}

// A middleware that rewrites the path after RouteCapture sends the request to
// a route RouteCapture did not see; the panic names that cause too.
func TestStreamingReachedAfterAPathRewritePanicsNamingTheRewrite(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /stream", middleware.Streaming(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	mux.HandleFunc("GET /plain", func(http.ResponseWriter, *http.Request) {})
	rewrite := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Path = "/stream"
			next.ServeHTTP(w, r)
		})
	}
	h := middleware.Chain(middleware.RouteCapture(mux.Handler), rewrite)(mux)

	defer func() {
		if msg := fmt.Sprint(recover()); !strings.Contains(msg, "rewrote the method or path after RouteCapture") {
			t.Errorf("panic = %q, want one naming the rewrite", msg)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/plain", nil))
}
