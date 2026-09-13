package lifecycle_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/guilhermebr/gox/pkg/lifecycle"
)

// recorder is a Component that appends start/stop events to a shared log so
// tests can assert on ordering across components.
type recorder struct {
	name     string
	events   *[]string
	mu       *sync.Mutex
	startErr error
	stopErr  error
	stopWait time.Duration // simulate a slow Stop
	run      func(ctx context.Context) error
	stops    int
}

func (r *recorder) Name() string { return r.name }

func (r *recorder) Start(context.Context) error {
	r.record("start " + r.name)
	return r.startErr
}

func (r *recorder) Stop(ctx context.Context) error {
	r.mu.Lock()
	r.stops++
	r.mu.Unlock()
	if r.stopWait > 0 {
		select {
		case <-time.After(r.stopWait):
		case <-ctx.Done():
			r.record("stop-timeout " + r.name)
			return ctx.Err()
		}
	}
	r.record("stop " + r.name)
	return r.stopErr
}

func (r *recorder) record(ev string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.events = append(*r.events, ev)
}

// runner wraps a recorder so it also implements lifecycle.Runner.
type runner struct{ *recorder }

func (r *runner) Run(ctx context.Context) error { return r.run(ctx) }

type harness struct {
	events []string
	mu     sync.Mutex
}

func (h *harness) comp(name string) *recorder {
	return &recorder{name: name, events: &h.events, mu: &h.mu}
}

func (h *harness) snapshot() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.events...)
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestManagerStartsByStageAndStopsInReverse(t *testing.T) {
	h := &harness{}
	m := lifecycle.NewManager(lifecycle.WithLogger(quietLogger()))

	// Registered out of order on purpose: stage decides, not registration.
	m.Add(30, h.comp("server"))
	m.Add(10, h.comp("db"))
	m.Add(20, h.comp("client-a"))
	m.Add(20, h.comp("client-b")) // same stage keeps registration order

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	want := []string{
		"start db", "start client-a", "start client-b", "start server",
		"stop server", "stop client-b", "stop client-a", "stop db",
	}
	if got := h.snapshot(); !equal(got, want) {
		t.Fatalf("events\n got: %v\nwant: %v", got, want)
	}
}

func TestManagerRollsBackStartedComponentsWhenOneFails(t *testing.T) {
	h := &harness{}
	m := lifecycle.NewManager(lifecycle.WithLogger(quietLogger()))

	boom := errors.New("boom")
	failing := h.comp("cache")
	failing.startErr = boom

	m.Add(10, h.comp("db"))
	m.Add(20, failing)
	m.Add(30, h.comp("server"))

	err := m.Start(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("Start error = %v, want wrapping %v", err, boom)
	}
	if err == nil || err.Error() != "lifecycle: start cache: boom" {
		t.Fatalf("Start error message = %q", err)
	}

	want := []string{"start db", "start cache", "stop db"}
	if got := h.snapshot(); !equal(got, want) {
		t.Fatalf("events\n got: %v\nwant: %v", got, want)
	}
}

func TestManagerBoundsEachStopByTimeoutAndCollectsErrors(t *testing.T) {
	h := &harness{}
	m := lifecycle.NewManager(
		lifecycle.WithLogger(quietLogger()),
		lifecycle.WithStopTimeout(20*time.Millisecond),
	)

	slow := h.comp("slow")
	slow.stopWait = time.Second
	angry := h.comp("angry")
	angry.stopErr = errors.New("refused")

	m.Add(10, angry)
	m.Add(20, slow)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	started := time.Now()
	err := m.Stop(context.Background())
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("Stop took %v; the per-component timeout was not applied", elapsed)
	}
	if err == nil {
		t.Fatal("Stop returned nil; want the collected errors")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Stop error should include the slow component's deadline error, got %v", err)
	}
	if !errors.Is(err, angry.stopErr) {
		t.Errorf("Stop error should include angry's error, got %v", err)
	}

	// The slow one is stopped first (reverse order) and times out; angry still
	// gets its own fresh budget afterwards.
	want := []string{"start angry", "start slow", "stop-timeout slow", "stop angry"}
	if got := h.snapshot(); !equal(got, want) {
		t.Fatalf("events\n got: %v\nwant: %v", got, want)
	}
}

func TestManagerStopIsIdempotent(t *testing.T) {
	h := &harness{}
	m := lifecycle.NewManager(lifecycle.WithLogger(quietLogger()))
	c := h.comp("db")
	m.Add(10, c)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if c.stops != 1 {
		t.Fatalf("component stopped %d times, want 1", c.stops)
	}
}

func TestManagerWaitReturnsFatalRunnerError(t *testing.T) {
	h := &harness{}
	m := lifecycle.NewManager(lifecycle.WithLogger(quietLogger()))

	fatal := errors.New("listener died")
	r := &runner{recorder: h.comp("smtp")}
	r.run = func(ctx context.Context) error {
		select {
		case <-time.After(10 * time.Millisecond):
			return fatal
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.Add(10, r)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := m.Wait(ctx)
	if !errors.Is(err, fatal) {
		t.Fatalf("Wait = %v, want %v", err, fatal)
	}
	if err.Error() != "lifecycle: smtp: listener died" {
		t.Fatalf("Wait error message = %q", err)
	}
}

func TestManagerWaitReturnsNilWhenContextIsDone(t *testing.T) {
	h := &harness{}
	m := lifecycle.NewManager(lifecycle.WithLogger(quietLogger()))

	r := &runner{recorder: h.comp("worker")}
	r.run = func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err() // canceled by Stop: not fatal
	}
	m.Add(10, r)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Wait(ctx); err != nil {
		t.Fatalf("Wait after ctx done = %v, want nil", err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestManagerRunnerFinishingCleanlyIsNotFatal(t *testing.T) {
	h := &harness{}
	m := lifecycle.NewManager(lifecycle.WithLogger(quietLogger()))

	r := &runner{recorder: h.comp("one-shot")}
	r.run = func(context.Context) error { return nil }
	m.Add(10, r)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := m.Wait(ctx); err != nil {
		t.Fatalf("Wait = %v, want nil after a clean Run return", err)
	}
}

func TestManagerComponentsListsInStartOrder(t *testing.T) {
	h := &harness{}
	m := lifecycle.NewManager(lifecycle.WithLogger(quietLogger()))
	m.Add(20, h.comp("b"))
	m.Add(10, h.comp("a"))

	got := m.Components()
	if len(got) != 2 || got[0].Name() != "a" || got[1].Name() != "b" {
		names := make([]string, len(got))
		for i, c := range got {
			names[i] = c.Name()
		}
		t.Fatalf("Components = %v, want [a b]", names)
	}
}

func TestManagerRejectsAddAfterStart(t *testing.T) {
	h := &harness{}
	m := lifecycle.NewManager(lifecycle.WithLogger(quietLogger()))
	m.Add(10, h.comp("a"))
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Add after Start should panic")
		}
	}()
	m.Add(10, h.comp("late"))
}

func TestManagerStopDoesNotHangOnARunnerThatIgnoresCancellation(t *testing.T) {
	h := &harness{}
	m := lifecycle.NewManager(
		lifecycle.WithLogger(quietLogger()),
		lifecycle.WithStopTimeout(20*time.Millisecond),
	)

	block := make(chan struct{})
	r := &runner{recorder: h.comp("stubborn")}
	r.run = func(context.Context) error {
		<-block // never looks at ctx
		return nil
	}
	m.Add(10, r)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- m.Stop(context.Background()) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Stop returned nil; want an error naming the runner that did not exit")
		}
		if got := err.Error(); got != "lifecycle: stubborn: Run did not return within 20ms after Stop" {
			t.Fatalf("Stop error = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop hung on a runner that ignores cancellation")
	}
	close(block)
}
