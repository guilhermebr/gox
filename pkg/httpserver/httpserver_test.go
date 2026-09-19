package httpserver_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/health"
	"github.com/guilhermebr/gox/pkg/httpserver"
	"github.com/guilhermebr/gox/pkg/middleware"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func envelope(t *testing.T, rec *httptest.ResponseRecorder) errors.Envelope {
	t.Helper()
	var env errors.Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body %q is not an envelope: %v", rec.Body, err)
	}
	return env
}

func TestHandlerRendersNotFoundAndMethodNotAllowedAsEnvelopes(t *testing.T) {
	mux := httpserver.NewMux()
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("item " + r.PathValue("id")))
	})
	h := httpserver.Handler(mux)

	t.Run("matched route passes through", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items/7", nil))
		if rec.Code != http.StatusOK || rec.Body.String() != "item 7" {
			t.Fatalf("code=%d body=%q", rec.Code, rec.Body.String())
		}
	})
	t.Run("unknown path is a not_found envelope", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d", rec.Code)
		}
		if env := envelope(t, rec); env.Code != "not_found" {
			t.Fatalf("envelope = %+v", env)
		}
	})
	t.Run("wrong method is a 405 envelope with Allow", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/items/7", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("code = %d body = %s", rec.Code, rec.Body)
		}
		if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "GET") {
			t.Fatalf("Allow = %q", allow)
		}
		if env := envelope(t, rec); env.Code != "unimplemented" {
			t.Fatalf("envelope = %+v", env)
		}
	})
}

func TestRegisterHealthServesProbesOnThePublicMux(t *testing.T) {
	reg := health.NewRegistry()
	mux := httpserver.NewMux()
	httpserver.RegisterHealth(mux, reg)
	h := httpserver.Handler(mux)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz before ready = %d", rec.Code)
	}
	reg.SetReady(true)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/readyz after ready = %d", rec.Code)
	}
}

func start(t *testing.T, srv *httpserver.Server) (base string, runDone <-chan error) {
	t.Helper()
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Run(context.Background()) }()
	return "http://" + srv.Addr(), done
}

func TestServerServesAndDrainsOnStop(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("hi")) })
	srv := httpserver.New(httpserver.Config{Addr: "127.0.0.1:0"}, handler, httpserver.WithLogger(quiet()))
	if srv.Name() != "http" {
		t.Fatalf("Name = %q", srv.Name())
	}
	base, done := start(t, srv)

	resp, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(b) != "hi" {
		t.Fatalf("body = %q", b)
	}

	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v after a clean Stop", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after Stop")
	}
}

func TestServerStopHardClosesWhenDrainExceedsDeadline(t *testing.T) {
	entered := make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	srv := httpserver.New(httpserver.Config{Addr: "127.0.0.1:0"}, handler, httpserver.WithLogger(quiet()))
	base, done := start(t, srv)

	go func() {
		resp, err := http.Get(base + "/slow")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-entered

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	began := time.Now()
	err := srv.Stop(ctx)
	if time.Since(began) > time.Second {
		t.Fatal("Stop did not hard-close after the deadline")
	}
	if err == nil {
		t.Fatal("Stop should report that the drain exceeded its deadline")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the hard close")
	}
}

func TestServerStartFailsFastOnBusyPortAndStopBeforeStartIsSafe(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	srv := httpserver.New(httpserver.Config{Addr: ln.Addr().String()}, http.NotFoundHandler(), httpserver.WithLogger(quiet()))
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
	err = srv.Start(context.Background())
	if err == nil || !strings.HasPrefix(err.Error(), "http: listen") {
		t.Fatalf("Start on a busy port = %v", err)
	}
}

func TestHandlerPublishesTheMatchedRoute(t *testing.T) {
	mux := httpserver.NewMux()
	mux.HandleFunc("GET /items/{id}", func(http.ResponseWriter, *http.Request) {})
	var got string
	probe := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			got = middleware.Route(r)
		})
	}
	resolve := func(r *http.Request) string {
		_, pattern := mux.Handler(r)
		return pattern
	}
	middleware.Chain(middleware.RouteCapture(resolve), probe, middleware.Timeout(time.Second))(httpserver.Handler(mux)).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/items/3", nil))
	if got != "GET /items/{id}" {
		t.Fatalf("Route = %q", got)
	}
}
