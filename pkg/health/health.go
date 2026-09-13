// Package health is a registry of named liveness and readiness checks with
// JSON handlers for /healthz and /readyz.
//
// Liveness answers "should this process be restarted?" and runs its checks
// always. Readiness answers "should this process receive traffic?" and is
// gated: it fails until SetReady(true) is called after every component has
// started, and fails again as soon as shutdown begins, so load balancers
// drain before connections are closed.
package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Check reports nil when healthy.
type Check func(ctx context.Context) error

// Statuses used in reports.
const (
	StatusOK   = "ok"
	StatusFail = "fail"
)

// DefaultTimeout bounds each check when no other value is set.
const DefaultTimeout = 5 * time.Second

// CheckResult is one check's outcome.
type CheckResult struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Report is the JSON body of /healthz and /readyz.
type Report struct {
	Status string                 `json:"status"`
	Checks map[string]CheckResult `json:"checks,omitempty"`
}

// Option configures a Registry.
type Option func(*Registry)

// WithTimeout bounds each individual check.
func WithTimeout(d time.Duration) Option {
	return func(r *Registry) { r.timeout = d }
}

// Registry holds named checks.
type Registry struct {
	timeout time.Duration
	ready   atomic.Bool

	mu    sync.RWMutex
	live  []namedCheck
	readi []namedCheck
}

type namedCheck struct {
	name  string
	check Check
}

// NewRegistry creates an empty Registry. It reports not ready until
// SetReady(true).
func NewRegistry(opts ...Option) *Registry {
	r := &Registry{timeout: DefaultTimeout}
	for _, o := range opts {
		o(r)
	}
	return r
}

// AddLiveness registers a liveness check. Names are unique per kind; a
// duplicate is a programming error and panics.
func (r *Registry) AddLiveness(name string, c Check) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.live = add(r.live, "liveness", name, c)
}

// AddReadiness registers a readiness check.
func (r *Registry) AddReadiness(name string, c Check) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.readi = add(r.readi, "readiness", name, c)
}

func add(list []namedCheck, kind, name string, c Check) []namedCheck {
	for _, n := range list {
		if n.name == name {
			panic(fmt.Sprintf("health: %s check %q registered twice", kind, name))
		}
	}
	return append(list, namedCheck{name: name, check: c})
}

// SetReady opens or closes the readiness gate.
func (r *Registry) SetReady(ready bool) { r.ready.Store(ready) }

// IsReady reports the state of the readiness gate.
func (r *Registry) IsReady() bool { return r.ready.Load() }

// Healthy runs every liveness check.
func (r *Registry) Healthy(ctx context.Context) Report {
	r.mu.RLock()
	checks := r.live
	r.mu.RUnlock()
	return r.run(ctx, checks)
}

// Ready reports not ready while the gate is closed, otherwise runs every
// readiness check.
func (r *Registry) Ready(ctx context.Context) Report {
	if !r.ready.Load() {
		return Report{
			Status: StatusFail,
			Checks: map[string]CheckResult{"app": {Status: StatusFail, Error: "not ready"}},
		}
	}
	r.mu.RLock()
	checks := r.readi
	r.mu.RUnlock()
	return r.run(ctx, checks)
}

func (r *Registry) run(ctx context.Context, checks []namedCheck) Report {
	rep := Report{Status: StatusOK}
	if len(checks) == 0 {
		return rep
	}
	rep.Checks = make(map[string]CheckResult, len(checks))
	for _, c := range checks {
		cctx, cancel := context.WithTimeout(ctx, r.timeout)
		err := c.check(cctx)
		cancel()
		if err != nil {
			rep.Status = StatusFail
			rep.Checks[c.name] = CheckResult{Status: StatusFail, Error: err.Error()}
			continue
		}
		rep.Checks[c.name] = CheckResult{Status: StatusOK}
	}
	return rep
}

// LivenessHandler serves the liveness report: 200 when ok, 503 otherwise.
func (r *Registry) LivenessHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		write(w, r.Healthy(req.Context()))
	})
}

// ReadinessHandler serves the readiness report: 200 when ok, 503 otherwise.
func (r *Registry) ReadinessHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		write(w, r.Ready(req.Context()))
	})
}

func write(w http.ResponseWriter, rep Report) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	status := http.StatusOK
	if rep.Status != StatusOK {
		status = http.StatusServiceUnavailable
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(rep)
}
