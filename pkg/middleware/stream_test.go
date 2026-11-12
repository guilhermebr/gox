package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guilhermebr/gox/pkg/middleware"
)

func TestStreamCaptureLetsAStreamingRouteOutliveTheTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /stream", middleware.Streaming(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !middleware.IsStreaming(r) {
			t.Error("IsStreaming = false inside a Streaming route")
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
	resolve := func(r *http.Request) http.Handler { h, _ := mux.Handler(r); return h }
	h := middleware.Chain(middleware.StreamCapture(resolve), middleware.Timeout(10*time.Millisecond))(mux)

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

func TestStreamingIsInvisibleWithoutStreamCapture(t *testing.T) {
	h := middleware.Streaming(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if middleware.IsStreaming(r) {
			t.Error("IsStreaming = true without StreamCapture")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q", rec.Body)
	}
}
