package gox

import (
	"log/slog"
	"strings"
	"time"

	"github.com/guilhermebr/gox/pkg/config"
	"github.com/guilhermebr/gox/pkg/errors"
	"github.com/guilhermebr/gox/pkg/lifecycle"
)

// Stage orders component start-up. Lower stages start first and stop last.
// Config, logging and telemetry are not components: they are ready before
// any factory runs.
type Stage int

// Stages. Feature packages pick the one that matches what they provide.
const (
	StageDatastore Stage = iota + 10 // databases, caches, message brokers
	StageClient                      // clients of other services
	StageUser                        // the service's own components
	StageServer                      // public listeners
	stageAdmin                       // the ops server, always last
)

// Builder is the extension API feature packages use inside their Enable()
// option. Services never touch it directly.
type Builder struct {
	name            string
	prefix          *string
	version         string
	userCfg         any
	sections        []config.Section
	factories       []factory
	values          map[any]any
	logger          *slog.Logger
	shutdownTimeout time.Duration
	adminDisabled   bool
	mappers         []errors.Mapper
}

type factory struct {
	stage Stage
	fn    func(a *App) (lifecycle.Component, error)
}

func newBuilder(name string) *Builder {
	return &Builder{name: name, values: make(map[any]any)}
}

func (b *Builder) prefixOrDefault() string {
	if b.prefix != nil {
		return *b.prefix
	}
	return strings.ToUpper(strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(b.name))
}

// ConfigSection registers a feature's config struct. name becomes the env
// sub-prefix: section "POSTGRES" under service prefix "BILLING" reads
// BILLING_POSTGRES_*. declaredBy names the option (for --help and errors).
func (b *Builder) ConfigSection(name string, dst any, declaredBy string) {
	b.sections = append(b.sections, config.Section{Name: name, Dst: dst, DeclaredBy: declaredBy})
}

// Component registers a factory that runs at the end of New, after config
// and logging are ready, in stage order. The component it returns is
// started by Run.
func (b *Builder) Component(stage Stage, fn func(a *App) (lifecycle.Component, error)) {
	b.factories = append(b.factories, factory{stage: stage, fn: fn})
}

// Set stores a value feature packages expose through their From accessor.
// Keys are unexported types owned by the feature package.
func (b *Builder) Set(key, value any) {
	b.values[key] = value
}
