// Package lifecycle defines the Component contract every gox-managed piece of
// a service implements, and a Manager that starts components in stage order,
// stops them in reverse with a per-component budget, and surfaces the first
// fatal error from any long-running component.
//
// The root gox package owns the only Manager a service has. Feature packages
// contribute components; they never drive the Manager directly.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"time"
)

// Component is something with a start and a stop. Start must return quickly
// and leave long work to goroutines or to Run (see Runner). Stop must be
// idempotent and respect the context deadline.
type Component interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// HealthChecker is implemented by components that can report liveness.
type HealthChecker interface {
	Healthy(ctx context.Context) error
}

// ReadyChecker is implemented by components that can report readiness.
type ReadyChecker interface {
	Ready(ctx context.Context) error
}

// Runner is implemented by components whose work is a blocking loop: an HTTP
// listener, an SMTP server, a worker pool. The Manager calls Run in its own
// goroutine after Start succeeds. Run returning a non-nil error other than the
// cancellation of its context is fatal for the whole service.
type Runner interface {
	Run(ctx context.Context) error
}

// DefaultStopTimeout bounds each component's Stop when no other value is set.
const DefaultStopTimeout = 30 * time.Second

// Option configures a Manager.
type Option func(*Manager)

// WithLogger sets the logger used for phase logging.
func WithLogger(l *slog.Logger) Option {
	return func(m *Manager) { m.log = l }
}

// WithStopTimeout bounds each component's Stop call. Every component gets a
// fresh budget; a slow one does not eat into the next one's time.
func WithStopTimeout(d time.Duration) Option {
	return func(m *Manager) { m.stopTimeout = d }
}

type entry struct {
	stage int
	seq   int
	comp  Component
}

// Manager owns an ordered set of components.
type Manager struct {
	log         *slog.Logger
	stopTimeout time.Duration

	mu      sync.Mutex
	entries []entry
	started []entry
	state   state

	runCtx    context.Context
	runCancel context.CancelFunc
	fatal     chan error
	runners   []runnerHandle
}

// runnerHandle tracks one Runner goroutine so Stop can wait for it by name.
type runnerHandle struct {
	name string
	done chan struct{}
}

type state int

const (
	stateNew state = iota
	stateStarted
	stateStopped
)

// NewManager creates an empty Manager.
func NewManager(opts ...Option) *Manager {
	m := &Manager{
		log:         slog.Default(),
		stopTimeout: DefaultStopTimeout,
		fatal:       make(chan error, 1),
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Add registers a component at a stage. Lower stages start first; within a
// stage, registration order is kept. Adding after Start is a programming
// error and panics.
func (m *Manager) Add(stage int, c Component) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != stateNew {
		panic(fmt.Sprintf("lifecycle: Add(%q) after Start", c.Name()))
	}
	m.entries = append(m.entries, entry{stage: stage, seq: len(m.entries), comp: c})
}

// Components returns the registered components in start order.
func (m *Manager) Components() []Component {
	m.mu.Lock()
	defer m.mu.Unlock()
	ordered := m.ordered()
	out := make([]Component, len(ordered))
	for i, e := range ordered {
		out[i] = e.comp
	}
	return out
}

func (m *Manager) ordered() []entry {
	out := append([]entry(nil), m.entries...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].stage != out[j].stage {
			return out[i].stage < out[j].stage
		}
		return out[i].seq < out[j].seq
	})
	return out
}

// Start starts every component in stage order. If one fails, the components
// already started are stopped in reverse and the error is returned, wrapped
// with the failing component's name.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.state != stateNew {
		m.mu.Unlock()
		return errors.New("lifecycle: Start called twice")
	}
	m.state = stateStarted
	m.runCtx, m.runCancel = context.WithCancel(context.Background())
	ordered := m.ordered()
	m.mu.Unlock()

	for _, e := range ordered {
		began := time.Now()
		if err := e.comp.Start(ctx); err != nil {
			m.log.Error("component failed to start",
				slog.String("component", e.comp.Name()),
				slog.String("error", err.Error()))
			stopErr := m.Stop(ctx)
			err = fmt.Errorf("lifecycle: start %s: %w", e.comp.Name(), err)
			if stopErr != nil {
				err = errors.Join(err, stopErr)
			}
			return err
		}
		m.mu.Lock()
		m.started = append(m.started, e)
		m.mu.Unlock()
		m.log.Info("component started",
			slog.String("component", e.comp.Name()),
			slog.Duration("duration", time.Since(began)))
		if r, ok := e.comp.(Runner); ok {
			m.launch(e.comp.Name(), r)
		}
	}
	return nil
}

func (m *Manager) launch(name string, r Runner) {
	h := runnerHandle{name: name, done: make(chan struct{})}
	m.mu.Lock()
	m.runners = append(m.runners, h)
	m.mu.Unlock()
	go func() {
		defer close(h.done)
		err := r.Run(m.runCtx)
		if err == nil || (m.runCtx.Err() != nil && errors.Is(err, context.Canceled)) {
			return
		}
		m.log.Error("component exited with error",
			slog.String("component", name),
			slog.String("error", err.Error()))
		select {
		case m.fatal <- fmt.Errorf("lifecycle: %s: %w", name, err):
		default: // first fatal error wins
		}
	}()
}

// Wait blocks until ctx is done or a Runner fails. It returns the runner's
// error in the second case and nil in the first.
func (m *Manager) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-m.fatal:
		return err
	}
}

// Stop stops the started components in reverse order. Each Stop call gets its
// own timeout; errors are collected and joined. Stop is idempotent.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if m.state == stateStopped {
		m.mu.Unlock()
		return nil
	}
	m.state = stateStopped
	started := m.started
	m.started = nil
	runners := m.runners
	m.runners = nil
	cancel := m.runCancel
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	var errs []error
	for _, e := range slices.Backward(started) {
		c := e.comp
		began := time.Now()
		stopCtx, cancelStop := context.WithTimeout(ctx, m.stopTimeout)
		err := c.Stop(stopCtx)
		cancelStop()
		if err != nil {
			m.log.Error("component failed to stop",
				slog.String("component", c.Name()),
				slog.String("error", err.Error()),
				slog.Duration("duration", time.Since(began)))
			errs = append(errs, fmt.Errorf("lifecycle: stop %s: %w", c.Name(), err))
			continue
		}
		m.log.Info("component stopped",
			slog.String("component", c.Name()),
			slog.Duration("duration", time.Since(began)))
	}
	// A Runner must return once its component is stopped (an http.Server's
	// Serve returns after Shutdown, for example). Wait for each one, but never
	// forever: a runner that ignores cancellation is reported, not waited on.
	for _, h := range slices.Backward(runners) {
		select {
		case <-h.done:
		case <-time.After(m.stopTimeout):
			errs = append(errs, fmt.Errorf("lifecycle: %s: Run did not return within %v after Stop", h.name, m.stopTimeout))
		case <-ctx.Done():
			errs = append(errs, fmt.Errorf("lifecycle: %s: %w", h.name, ctx.Err()))
		}
	}
	return errors.Join(errs...)
}
