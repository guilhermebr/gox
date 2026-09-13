package supabase_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/supabase"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func setArgs(t *testing.T) {
	t.Helper()
	old := os.Args
	os.Args = []string{"svc"}
	t.Cleanup(func() { os.Args = old })
}

func TestEnableRequiresURLAndKey(t *testing.T) {
	setArgs(t)
	_, err := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()), supabase.Enable())
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"BILLING_SUPABASE_URL", "supabase.Enable()"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err %q lacks %q", err, want)
		}
	}
}

func TestEnableBuildsClientAndReadinessProbesAuthHealth(t *testing.T) {
	setArgs(t)
	var healthy atomic.Bool
	healthy.Store(true)
	var apikey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/v1/health" {
			apikey = r.Header.Get("apikey")
			if healthy.Load() {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	t.Setenv("BILLING_SUPABASE_URL", srv.URL)
	t.Setenv("BILLING_SUPABASE_KEY", "anon-key")

	a, err := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()), supabase.Enable())
	if err != nil {
		t.Fatal(err)
	}
	if supabase.From(a) == nil {
		t.Fatal("From returned nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for !a.Health().IsReady() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	defer func() {
		cancel()
		<-done
	}()

	if rep := a.Health().Ready(context.Background()); rep.Checks["supabase"].Status != "ok" {
		t.Fatalf("readiness = %+v", rep)
	}
	if apikey != "anon-key" {
		t.Fatalf("health probe sent apikey %q", apikey)
	}
	healthy.Store(false)
	if rep := a.Health().Ready(context.Background()); rep.Checks["supabase"].Status != "fail" {
		t.Fatalf("readiness after outage = %+v", rep)
	}
}

func TestFromPanicsWithoutEnable(t *testing.T) {
	setArgs(t)
	a, _ := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()))
	defer func() {
		want := "gox: supabase.From called but supabase.Enable() was not passed to gox.New"
		if r := recover(); r != want {
			t.Fatalf("panic = %v", r)
		}
	}()
	supabase.From(a)
}
