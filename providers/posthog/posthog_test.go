package posthog_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/posthog/posthog-go"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/providers/posthog"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func setArgs(t *testing.T) {
	t.Helper()
	old := os.Args
	os.Args = []string{"svc"}
	t.Cleanup(func() { os.Args = old })
}

func TestQueuedEventsAreFlushedWhenTheAppStops(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var rd io.Reader = r.Body
		if r.Header.Get("Content-Encoding") == "gzip" {
			if gz, err := gzip.NewReader(r.Body); err == nil {
				rd = gz
			}
		}
		b, _ := io.ReadAll(rd)
		mu.Lock()
		bodies = append(bodies, r.URL.Path+" "+string(b))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"status":1}`))
	}))
	defer api.Close()
	setArgs(t)
	t.Setenv("SHOP_POSTHOG_PROJECT_KEY", "phc_test")
	t.Setenv("SHOP_POSTHOG_HOST", api.URL)
	a, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), posthog.Enable())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	for !a.Health().IsReady() {
		time.Sleep(5 * time.Millisecond)
	}
	if err := posthog.From(a).Enqueue(sdk.Capture{DistinctId: "user_01", Event: "invoice paid"}); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	all := strings.Join(bodies, "\n")
	if !strings.Contains(all, "invoice paid") || !strings.Contains(all, "phc_test") {
		t.Fatalf("the event queued before shutdown must be delivered: %q", all)
	}
}

// Without a project key the app still starts: the client is the SDK's
// no-op one, which drops every event, sends nothing and logs one info line
// (never the SDK's own error about the empty key).
func TestWithoutAProjectKeyTheClientIsOff(t *testing.T) {
	var calls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"status":1}`))
	}))
	defer api.Close()
	setArgs(t)
	t.Setenv("SHOP_POSTHOG_HOST", api.URL)
	var logs syncBuffer
	a, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))), posthog.Enable())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	for !a.Health().IsReady() {
		time.Sleep(5 * time.Millisecond)
	}
	if err := posthog.From(a).Enqueue(sdk.Capture{DistinctId: "user_01", Event: "invoice paid"}); !errors.Is(err, sdk.ErrSDKDisabled) {
		t.Fatalf("Enqueue = %v, want ErrSDKDisabled", err)
	}
	if on, err := posthog.From(a).IsFeatureEnabled(sdk.FeatureFlagPayload{Key: "new-receipt", DistinctId: "user_01"}); on != false || !errors.Is(err, sdk.ErrSDKDisabled) {
		t.Fatalf("IsFeatureEnabled = %v, %v", on, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("the client is off but made %d requests", n)
	}
	out := logs.String()
	if !strings.Contains(out, "posthog off") || strings.Contains(out, "level=ERROR") {
		t.Fatalf("logs: %s", out)
	}
}

func TestASecretKeyNeedsTheProjectKey(t *testing.T) {
	setArgs(t)
	t.Setenv("SHOP_POSTHOG_SECRET_KEY", "phs_secret")
	if _, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), posthog.Enable()); err == nil || !strings.Contains(err.Error(), "POSTHOG_PROJECT_KEY") {
		t.Fatalf("err = %v", err)
	}
}

func TestFromPanicsWithoutEnable(t *testing.T) {
	setArgs(t)
	a, _ := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()))
	defer func() {
		if r := recover(); r != "gox: posthog.From called but posthog.Enable() was not passed to gox.New" {
			t.Fatalf("panic = %v", r)
		}
	}()
	posthog.From(a)
}

// syncBuffer is a bytes.Buffer safe for the logger's goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
