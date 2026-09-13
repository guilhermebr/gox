package log_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/guilhermebr/gox/pkg/log"
)

func newJSON(t *testing.T, cfg log.Config, opts ...log.Option) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	l, err := log.New(cfg, append(opts, log.WithWriter(&buf))...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return l, &buf
}

func lastLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &m); err != nil {
		t.Fatalf("not JSON: %q: %v", lines[len(lines)-1], err)
	}
	return m
}

func TestFormatFollowsEnvironmentWhenAuto(t *testing.T) {
	t.Run("production is json", func(t *testing.T) {
		l, buf := newJSON(t, log.Config{Level: "info", Format: "auto", Environment: "production"})
		l.Info("hello", "k", "v")
		m := lastLine(t, buf)
		if m["msg"] != "hello" || m["k"] != "v" {
			t.Fatalf("line = %v", m)
		}
	})
	t.Run("development is text", func(t *testing.T) {
		var buf bytes.Buffer
		l, err := log.New(log.Config{Level: "info", Format: "auto", Environment: "development"}, log.WithWriter(&buf))
		if err != nil {
			t.Fatal(err)
		}
		l.Info("hello", "k", "v")
		if got := buf.String(); !strings.Contains(got, "msg=hello") || !strings.Contains(got, "k=v") {
			t.Fatalf("text line = %q", got)
		}
	})
	t.Run("explicit format wins over environment", func(t *testing.T) {
		l, buf := newJSON(t, log.Config{Level: "info", Format: "json", Environment: "development"})
		l.Info("hello")
		if m := lastLine(t, buf); m["msg"] != "hello" {
			t.Fatalf("line = %v", m)
		}
	})
}

func TestLevelFilters(t *testing.T) {
	l, buf := newJSON(t, log.Config{Level: "warn", Format: "json", Environment: "production"})
	l.Info("dropped")
	l.Warn("kept")
	if got := buf.String(); strings.Contains(got, "dropped") || !strings.Contains(got, "kept") {
		t.Fatalf("output = %q", got)
	}
}

func TestInvalidLevelOrFormatIsAnError(t *testing.T) {
	if _, err := log.New(log.Config{Level: "loud", Format: "json"}); err == nil {
		t.Fatal("invalid level accepted")
	}
	if _, err := log.New(log.Config{Level: "info", Format: "xml"}); err == nil {
		t.Fatal("invalid format accepted")
	}
}

func TestRequestIDFromContextIsStampedOnEveryRecord(t *testing.T) {
	l, buf := newJSON(t, log.Config{Level: "info", Format: "json", Environment: "production"})
	ctx := log.WithRequestID(context.Background(), "req-123")
	if got := log.RequestID(ctx); got != "req-123" {
		t.Fatalf("RequestID = %q", got)
	}
	l.InfoContext(ctx, "handled")
	if m := lastLine(t, buf); m[log.KeyRequestID] != "req-123" {
		t.Fatalf("line = %v, want %s", m, log.KeyRequestID)
	}
	l.Info("no context")
	if m := lastLine(t, buf); m[log.KeyRequestID] != nil {
		t.Fatalf("line without context should not carry a request id: %v", m)
	}
}

func TestContextAttrsHookAddsFields(t *testing.T) {
	type traceKey struct{}
	hook := func(ctx context.Context) []slog.Attr {
		if v, ok := ctx.Value(traceKey{}).(string); ok {
			return []slog.Attr{slog.String(log.KeyTraceID, v)}
		}
		return nil
	}
	l, buf := newJSON(t, log.Config{Level: "info", Format: "json", Environment: "production"}, log.WithContextAttrs(hook))
	l.InfoContext(context.WithValue(context.Background(), traceKey{}, "abc"), "traced")
	if m := lastLine(t, buf); m[log.KeyTraceID] != "abc" {
		t.Fatalf("line = %v", m)
	}
}

func TestFromContextFallsBackToDefault(t *testing.T) {
	if log.FromContext(context.Background()) != slog.Default() {
		t.Fatal("FromContext without a logger should return slog.Default()")
	}
	l, _ := newJSON(t, log.Config{Level: "info", Format: "json"})
	ctx := log.WithContext(context.Background(), l)
	if log.FromContext(ctx) != l {
		t.Fatal("FromContext did not return the stored logger")
	}
}

func TestStandardKeysAreStable(t *testing.T) {
	want := map[string]string{
		log.KeyRequestID: "request_id",
		log.KeyTraceID:   "trace_id",
		log.KeySpanID:    "span_id",
		log.KeyError:     "error",
		log.KeyComponent: "component",
		log.KeyDuration:  "duration",
	}
	for got, exp := range want {
		if got != exp {
			t.Errorf("key %q, want %q", got, exp)
		}
	}
}
