package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guilhermebr/gox/pkg/health"
)

func ok(context.Context) error   { return nil }
func fail(context.Context) error { return errors.New("connection refused") }

func TestLivenessAggregatesChecks(t *testing.T) {
	r := health.NewRegistry()
	r.AddLiveness("db", ok)
	r.AddLiveness("cache", fail)

	rep := r.Healthy(context.Background())
	if rep.Status != health.StatusFail {
		t.Fatalf("Status = %q, want fail", rep.Status)
	}
	if rep.Checks["db"].Status != health.StatusOK {
		t.Fatalf("db = %+v", rep.Checks["db"])
	}
	if c := rep.Checks["cache"]; c.Status != health.StatusFail || c.Error != "connection refused" {
		t.Fatalf("cache = %+v", c)
	}
}

func TestEmptyRegistryIsHealthyAndReadyOnceMarked(t *testing.T) {
	r := health.NewRegistry()
	if rep := r.Healthy(context.Background()); rep.Status != health.StatusOK {
		t.Fatalf("Healthy = %+v", rep)
	}
	if rep := r.Ready(context.Background()); rep.Status != health.StatusFail {
		t.Fatalf("Ready before SetReady = %+v; readiness must be gated until the app has started", rep)
	}
	r.SetReady(true)
	if rep := r.Ready(context.Background()); rep.Status != health.StatusOK {
		t.Fatalf("Ready after SetReady = %+v", rep)
	}
	r.SetReady(false)
	if rep := r.Ready(context.Background()); rep.Status != health.StatusFail || rep.Checks["app"].Error != "not ready" {
		t.Fatalf("Ready after SetReady(false) = %+v", rep)
	}
}

func TestReadinessRunsChecksOnlyWhenGateIsOpen(t *testing.T) {
	r := health.NewRegistry()
	calls := 0
	r.AddReadiness("db", func(context.Context) error { calls++; return nil })

	r.Ready(context.Background())
	if calls != 0 {
		t.Fatal("readiness checks ran while the app was not ready")
	}
	r.SetReady(true)
	if rep := r.Ready(context.Background()); rep.Status != health.StatusOK || calls != 1 {
		t.Fatalf("Ready = %+v, calls = %d", rep, calls)
	}
}

func TestHandlersReturnJSONAndStatusCodes(t *testing.T) {
	r := health.NewRegistry()
	r.AddLiveness("db", ok)
	r.AddReadiness("db", fail)
	r.SetReady(true)

	t.Run("healthz 200", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r.LivenessHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("code = %d body = %s", rec.Code, rec.Body)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("content-type = %q", ct)
		}
		var rep health.Report
		if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		if rep.Status != health.StatusOK {
			t.Fatalf("report = %+v", rep)
		}
	})
	t.Run("readyz 503", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r.ReadinessHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("code = %d body = %s", rec.Code, rec.Body)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("Cache-Control = %q", cc)
		}
	})
}

func TestChecksAreBoundedByTimeout(t *testing.T) {
	r := health.NewRegistry(health.WithTimeout(20 * time.Millisecond))
	r.AddLiveness("slow", func(ctx context.Context) error {
		select {
		case <-time.After(time.Second):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	started := time.Now()
	rep := r.Healthy(context.Background())
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("check was not bounded by the timeout")
	}
	if rep.Checks["slow"].Status != health.StatusFail {
		t.Fatalf("slow = %+v", rep.Checks["slow"])
	}
}

func TestDuplicateCheckNamePanics(t *testing.T) {
	r := health.NewRegistry()
	r.AddLiveness("db", ok)
	defer func() {
		if recover() == nil {
			t.Fatal("registering the same liveness name twice should panic")
		}
	}()
	r.AddLiveness("db", ok)
}
