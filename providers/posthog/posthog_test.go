package posthog_test

import (
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
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

func TestEnableNamesTheMissingKeyAndFromPanics(t *testing.T) {
	setArgs(t)
	if _, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), posthog.Enable()); err == nil || !strings.Contains(err.Error(), "SHOP_POSTHOG_PROJECT_KEY") {
		t.Fatalf("err = %v", err)
	}
	a, _ := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()))
	defer func() {
		if r := recover(); r != "gox: posthog.From called but posthog.Enable() was not passed to gox.New" {
			t.Fatalf("panic = %v", r)
		}
	}()
	posthog.From(a)
}
