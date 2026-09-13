package lifecycle_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/guilhermebr/gox/pkg/lifecycle"
)

func TestPeriodicRunsOnTheIntervalAndStopsCleanly(t *testing.T) {
	var runs atomic.Int32
	p := lifecycle.Periodic("tick", 10*time.Millisecond, func(context.Context) error {
		runs.Add(1)
		return nil
	}, lifecycle.WithPeriodicLogger(quietLogger()), lifecycle.WithInitialDelay(0))

	if p.Name() != "tick" {
		t.Fatalf("Name = %q", p.Name())
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	got := runs.Load()
	if got < 3 {
		t.Fatalf("ran %d times in 60ms at a 10ms interval; want at least 3", got)
	}
	time.Sleep(30 * time.Millisecond)
	if runs.Load() != got {
		t.Fatal("job kept running after Stop")
	}
}

func TestPeriodicSurvivesErrorsAndPanics(t *testing.T) {
	var runs atomic.Int32
	p := lifecycle.Periodic("flaky", 5*time.Millisecond, func(context.Context) error {
		n := runs.Add(1)
		switch n {
		case 1:
			return errors.New("transient")
		case 2:
			panic("boom")
		}
		return nil
	}, lifecycle.WithPeriodicLogger(quietLogger()), lifecycle.WithInitialDelay(0))

	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if runs.Load() < 4 {
		t.Fatalf("ran %d times; an error and a panic must not end the loop", runs.Load())
	}
}

func TestPeriodicStopWaitsForAnInFlightRun(t *testing.T) {
	entered := make(chan struct{})
	var finished atomic.Bool
	p := lifecycle.Periodic("slow", time.Hour, func(ctx context.Context) error {
		close(entered)
		<-ctx.Done() // the job honors cancellation
		finished.Store(true)
		return ctx.Err()
	}, lifecycle.WithPeriodicLogger(quietLogger()), lifecycle.WithInitialDelay(0))

	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-entered
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !finished.Load() {
		t.Fatal("Stop returned before the in-flight run observed cancellation")
	}
}

func TestPeriodicStopIsIdempotentAndSafeBeforeStart(t *testing.T) {
	p := lifecycle.Periodic("noop", time.Hour, func(context.Context) error { return nil },
		lifecycle.WithPeriodicLogger(quietLogger()))
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := p.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}
