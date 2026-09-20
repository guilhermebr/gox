package temporal_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/worker"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/providers/temporal"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func setArgs(t *testing.T) {
	t.Helper()
	old := os.Args
	os.Args = []string{"svc"}
	t.Cleanup(func() { os.Args = old })
}

func newApp(t *testing.T) *gox.App {
	t.Helper()
	setArgs(t)
	a, err := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()), temporal.Enable())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestFromPanicsWithoutEnable(t *testing.T) {
	setArgs(t)
	a, _ := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()))
	defer func() {
		if r := recover(); r != "gox: temporal.From called but temporal.Enable() was not passed to gox.New" {
			t.Fatalf("panic = %v", r)
		}
	}()
	temporal.From(a)
}

func TestNewNeedsNoServerAndWorkersAreOnePerTaskQueue(t *testing.T) {
	a := newApp(t) // the client connects at Run, like the postgres pool
	if temporal.From(a) == nil {
		t.Fatal("From must return the client right after New")
	}
	w1 := temporal.Worker(a, "billing", worker.Options{})
	w2 := temporal.Worker(a, "billing", worker.Options{})
	if w1 == nil || w1 != w2 {
		t.Fatal("one worker per task queue")
	}
	if temporal.Worker(a, "reports", worker.Options{}) == w1 {
		t.Fatal("another task queue is another worker")
	}
}

func TestRunFailsFastWhenTheServerIsUnreachable(t *testing.T) {
	t.Setenv("BILLING_TEMPORAL_ADDRESS", "127.0.0.1:1")
	t.Setenv("BILLING_TEMPORAL_CONNECT_TIMEOUT", "1s")
	a := newApp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err := a.RunContext(ctx)
	if err == nil || !strings.Contains(err.Error(), "temporal") || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("RunContext = %v", err)
	}
	if a.Health().IsReady() {
		t.Fatal("must not report ready")
	}
}

func TestConfigIsValidated(t *testing.T) {
	cases := map[string]map[string]string{
		"BILLING_TEMPORAL_TLS":     {"BILLING_TEMPORAL_TLS": "sometimes"},
		"BILLING_TEMPORAL_TLS_KEY": {"BILLING_TEMPORAL_TLS_CERT": "-----BEGIN CERTIFICATE-----\nx\n-----END CERTIFICATE-----"},
		"BILLING_TEMPORAL_ADDRESS": {"BILLING_TEMPORAL_ADDRESS": "https://temporal.example:7233"},
	}
	for want, env := range cases {
		t.Run(want, func(t *testing.T) {
			setArgs(t)
			for k, v := range env {
				t.Setenv(k, v)
			}
			_, err := gox.New("billing", gox.WithoutAdminServer(), gox.WithLogger(quiet()), temporal.Enable())
			if err == nil || !strings.Contains(err.Error(), strings.TrimPrefix(want, "BILLING_")) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
