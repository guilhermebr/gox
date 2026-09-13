package gox

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/guilhermebr/gox/pkg/config"
	"github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/health"
	"github.com/guilhermebr/gox/pkg/lifecycle"
	goxotel "github.com/guilhermebr/gox/pkg/otel"
)

// App is a built service: config loaded, logger ready, components
// constructed. Run starts it.
type App struct {
	name     string
	prefix   string
	cfg      *config.Base
	userCfg  any
	sections []config.Section
	log      *slog.Logger
	health   *health.Registry
	manager  *lifecycle.Manager
	mappers  []errors.Mapper
	otel     *goxotel.Providers

	mu     sync.RWMutex
	values map[any]any
}

// Name returns the service name given to New.
func (a *App) Name() string { return a.name }

// Log returns the service logger.
func (a *App) Log() *slog.Logger { return a.log }

// Config returns the framework configuration. A service's own fields live in
// the struct it passed to WithConfig.
func (a *App) Config() BaseConfig { return *a.cfg }

// Health returns the registry behind /healthz and /readyz. Register extra
// checks on it before Run.
func (a *App) Health() *health.Registry { return a.health }

// Add registers user components at StageUser after New and before Run.
func (a *App) Add(cs ...lifecycle.Component) {
	for _, c := range cs {
		a.add(int(StageUser), c)
	}
}

func (a *App) add(stage int, c lifecycle.Component) {
	a.manager.Add(stage, c)
	if hc, ok := c.(lifecycle.HealthChecker); ok {
		a.health.AddLiveness(c.Name(), hc.Healthy)
	}
	if rc, ok := c.(lifecycle.ReadyChecker); ok {
		a.health.AddReadiness(c.Name(), rc.Ready)
	}
}

// Value returns a value stored by a feature package's Enable option.
func (a *App) Value(key any) (any, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	v, ok := a.values[key]
	return v, ok
}

// Value is the typed form of App.Value.
func Value[T any](a *App, key any) (T, bool) {
	v, ok := a.Value(key)
	if !ok {
		var zero T
		return zero, false
	}
	t, ok := v.(T)
	return t, ok
}

// MustValue is what feature accessors use: it returns the stored value or
// panics with the standard message naming the accessor that was called and
// the option that was missing, for example
// "gox: postgres.From called but postgres.Enable() was not passed to gox.New".
func MustValue[T any](a *App, key any, accessor, option string) T {
	t, ok := Value[T](a, key)
	if !ok {
		panic(fmt.Sprintf("gox: %s called but %s was not passed to gox.New", accessor, option))
	}
	return t
}
