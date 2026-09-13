package gox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/guilhermebr/gox/pkg/admin"
	"github.com/guilhermebr/gox/pkg/config"
	"github.com/guilhermebr/gox/pkg/health"
	"github.com/guilhermebr/gox/pkg/lifecycle"
	"github.com/guilhermebr/gox/pkg/log"
	goxotel "github.com/guilhermebr/gox/pkg/otel"
)

// BaseConfig is the configuration every service has. Embed it in your own
// config struct and pass that to WithConfig.
type BaseConfig = config.Base

// Option declares one thing the app is made of or one behaviour of it.
type Option func(*Builder) error

// New builds an App: it applies the options, loads and validates config in
// one pass, builds the logger, runs every component factory, and wires the
// admin server. Nothing is started until Run. On error the returned error
// names what to fix.
func New(name string, opts ...Option) (*App, error) {
	if name == "" {
		return nil, errors.New("gox: New: name must not be empty")
	}
	b := newBuilder(name)
	for _, o := range opts {
		if err := o(b); err != nil {
			return nil, fmt.Errorf("gox: %w", err)
		}
	}
	return b.build()
}

// MustNew is New for main(): --help and --version print and exit 0, any
// other error prints to stderr and exits 1.
func MustNew(name string, opts ...Option) *App {
	a, err := New(name, opts...)
	if err != nil {
		var help *config.HelpError
		var version *config.VersionError
		switch {
		case errors.As(err, &help):
			_, _ = fmt.Fprintln(os.Stdout, help.Usage)
			os.Exit(0)
		case errors.As(err, &version):
			_, _ = fmt.Fprintln(os.Stdout, version.Info)
			os.Exit(0)
		}
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return a
}

// ---- Feature options provided by the root ----

// Component adds any user component at StageUser. It is the escape hatch
// for things gox has no option for.
func Component(c lifecycle.Component) Option {
	return func(b *Builder) error {
		if c == nil {
			return errors.New("Component(nil)")
		}
		b.Component(StageUser, func(*App) (lifecycle.Component, error) { return c, nil })
		return nil
	}
}

// Periodic runs fn every interval as a component: staggered first run,
// panic recovery, errors logged, stopped with the app.
func Periodic(name string, every time.Duration, fn func(context.Context) error) Option {
	return func(b *Builder) error {
		if every <= 0 {
			return fmt.Errorf("Periodic(%q): interval must be > 0", name)
		}
		b.Component(StageUser, func(a *App) (lifecycle.Component, error) {
			return lifecycle.Periodic(name, every, fn, lifecycle.WithPeriodicLogger(a.Log())), nil
		})
		return nil
	}
}

// ---- Behaviour options ----

// WithVersion sets the build version reported in logs and on /version.
func WithVersion(v string) Option {
	return func(b *Builder) error {
		b.version = v
		return nil
	}
}

// WithConfigPrefix sets the environment prefix. The default is the
// uppercased service name; "" means unprefixed variables (HTTP_ADDR, ...).
func WithConfigPrefix(p string) Option {
	return func(b *Builder) error {
		b.prefix = &p
		return nil
	}
}

// WithConfig registers the service's own config struct, which must embed
// BaseConfig. It is loaded and validated in the same pass as everything else.
func WithConfig(dst any) Option {
	return func(b *Builder) error {
		if dst == nil {
			return errors.New("WithConfig(nil)")
		}
		if _, ok := config.BaseOf(dst); !ok {
			return fmt.Errorf("WithConfig: %T must embed gox.BaseConfig", dst)
		}
		b.userCfg = dst
		return nil
	}
}

// WithShutdownTimeout sets the default per-component stop budget. The
// SHUTDOWN_TIMEOUT variable still overrides it.
func WithShutdownTimeout(d time.Duration) Option {
	return func(b *Builder) error {
		b.shutdownTimeout = d
		return nil
	}
}

// WithoutAdminServer disables the ops server.
func WithoutAdminServer() Option {
	return func(b *Builder) error {
		b.adminDisabled = true
		return nil
	}
}

// WithLogger replaces the logger gox would build from config.
func WithLogger(l *slog.Logger) Option {
	return func(b *Builder) error {
		if l == nil {
			return errors.New("WithLogger(nil)")
		}
		b.logger = l
		return nil
	}
}

// build runs the parts of New that need the applied options.
func (b *Builder) build() (*App, error) {
	prefix := b.prefixOrDefault()

	base := &config.Base{ServiceName: b.name, Version: b.version}
	if b.shutdownTimeout > 0 {
		base.Shutdown.Timeout = b.shutdownTimeout
	}
	var dst any = base
	if b.userCfg != nil {
		dst = b.userCfg
		ub, _ := config.BaseOf(b.userCfg)
		*ub = *base
		base = ub
	}
	if err := config.LoadSections(prefix, dst, b.sections); err != nil {
		return nil, err
	}

	logger := b.logger
	if logger == nil {
		var err error
		logger, err = log.New(log.Config{
			Level:       base.Log.Level,
			Format:      base.Log.Format,
			Environment: base.Environment,
		}, log.WithContextAttrs(goxotel.LogAttrs))
		if err != nil {
			return nil, fmt.Errorf("gox: %w", err)
		}
		logger = logger.With(slog.String("service", base.ServiceName))
		slog.SetDefault(logger)
	}

	providers, err := goxotel.Setup(context.Background(), goxotel.Config{
		ServiceName: base.ServiceName,
		Version:     base.Version,
		Environment: base.Environment,
		Enabled:     base.Otel.Enabled,
		Endpoint:    base.Otel.Endpoint,
		Protocol:    base.Otel.Protocol,
		Insecure:    base.Otel.Insecure,
	})
	if err != nil {
		return nil, fmt.Errorf("gox: %w", err)
	}

	a := &App{
		name:     b.name,
		prefix:   prefix,
		cfg:      base,
		userCfg:  dst,
		sections: b.sections,
		log:      logger,
		health:   health.NewRegistry(),
		manager: lifecycle.NewManager(
			lifecycle.WithLogger(logger),
			lifecycle.WithStopTimeout(base.Shutdown.Timeout),
		),
		values:  b.values,
		mappers: b.mappers,
		otel:    providers,
	}
	if b.httpClient {
		b.buildHTTPClient(a)
	}

	for _, fn := range b.setups {
		if err := fn(a); err != nil {
			return nil, fmt.Errorf("gox: %w", err)
		}
	}
	sort.SliceStable(b.factories, func(i, j int) bool { return b.factories[i].stage < b.factories[j].stage })
	for _, f := range b.factories {
		c, err := f.fn(a)
		if err != nil {
			return nil, fmt.Errorf("gox: building component at stage %d: %w", f.stage, err)
		}
		if c == nil {
			return nil, fmt.Errorf("gox: component factory at stage %d returned nil", f.stage)
		}
		a.add(int(f.stage), c)
	}

	for _, fn := range b.finishes {
		if err := fn(a); err != nil {
			return nil, fmt.Errorf("gox: %w", err)
		}
	}

	if !b.adminDisabled {
		srv := admin.New(admin.Config{Addr: base.Admin.Addr}, a.health,
			admin.Info{Service: base.ServiceName, Version: base.Version},
			admin.WithLogger(logger))
		srv.Handle("GET /metrics", providers.Metrics)
		a.add(int(stageAdmin), srv)
	}
	return a, nil
}
