package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"runtime/debug"
	"sync"
	"time"
)

// PeriodicOption configures a Periodic component.
type PeriodicOption func(*periodic)

// WithPeriodicLogger sets the logger used to report job failures.
func WithPeriodicLogger(l *slog.Logger) PeriodicOption {
	return func(p *periodic) { p.log = l }
}

// WithInitialDelay fixes the delay before the first run. By default the first
// run is staggered by a random delay of up to min(interval, 5s) so that
// several replicas started together do not fire in lockstep.
func WithInitialDelay(d time.Duration) PeriodicOption {
	return func(p *periodic) { p.initial = &d }
}

// Periodic returns a Component that calls fn every interval until stopped.
// Errors and panics are logged and the loop continues; a job cannot take the
// service down. Stop cancels the job's context and waits for an in-flight run
// to return.
func Periodic(name string, interval time.Duration, fn func(context.Context) error, opts ...PeriodicOption) Component {
	if interval <= 0 {
		panic(fmt.Sprintf("lifecycle: Periodic(%q): interval must be > 0", name))
	}
	p := &periodic{name: name, interval: interval, fn: fn, log: slog.Default()}
	for _, o := range opts {
		o(p)
	}
	return p
}

type periodic struct {
	name     string
	interval time.Duration
	initial  *time.Duration
	fn       func(context.Context) error
	log      *slog.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (p *periodic) Name() string { return p.name }

func (p *periodic) Start(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		return fmt.Errorf("lifecycle: periodic %s: already started", p.name)
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.wg.Go(func() { p.loop(ctx) })
	return nil
}

func (p *periodic) Stop(ctx context.Context) error {
	p.mu.Lock()
	cancel := p.cancel
	p.cancel = nil
	p.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("lifecycle: periodic %s: %w", p.name, ctx.Err())
	}
}

func (p *periodic) firstDelay() time.Duration {
	if p.initial != nil {
		return *p.initial
	}
	limit := min(p.interval, 5*time.Second)
	return rand.N(limit + 1) //nolint:gosec // jitter, not security
}

func (p *periodic) loop(ctx context.Context) {
	timer := time.NewTimer(p.firstDelay())
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		p.runOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *periodic) runOnce(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			p.log.Error("periodic job panicked",
				slog.String("component", p.name),
				slog.Any("panic", r),
				slog.String("stack", string(debug.Stack())))
		}
	}()
	if err := p.fn(ctx); err != nil && !errors.Is(err, context.Canceled) {
		p.log.Error("periodic job failed",
			slog.String("component", p.name),
			slog.String("error", err.Error()))
	}
}
