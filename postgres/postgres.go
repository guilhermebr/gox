package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/lifecycle"
)

type key struct{}

type options struct {
	migrations fs.FS
	hasMigrate bool
}

// Option configures Enable.
type Option func(*options)

// WithMigrations runs the SQL migrations in fsys (golang-migrate layout:
// NNNN_name.up.sql / .down.sql, anywhere in the tree) at boot, before the
// app reports ready. POSTGRES_MIGRATE=false skips them for deployments that
// migrate as a separate step; Migrate runs them standalone.
func WithMigrations(fsys fs.FS) Option {
	return func(o *options) {
		o.migrations = fsys
		o.hasMigrate = true
	}
}

// Enable declares a Postgres pool: it registers the POSTGRES config
// section and a component at StageDatastore that connects and pings on
// Start (a lazy pool must never report healthy), runs migrations, exposes
// readiness, closes on Stop, and traces queries and pool stats.
func Enable(opts ...Option) gox.Option {
	return func(b *gox.Builder) error {
		o := options{}
		for _, opt := range opts {
			opt(&o)
		}
		if o.hasMigrate && o.migrations == nil {
			return errors.New("postgres: WithMigrations(nil): pass an embed.FS with the migration files")
		}
		cfg := &Config{}
		b.ConfigSection("POSTGRES", cfg, "postgres.Enable()")
		b.Component(gox.StageDatastore, func(a *gox.App) (lifecycle.Component, error) {
			pool, err := newPool(cfg, a)
			if err != nil {
				return nil, err
			}
			c := &component{cfg: cfg, pool: pool, log: a.Log(), migrations: o.migrations}
			b.Set(key{}, pool)
			return c, nil
		})
		return nil
	}
}

// From returns the pool. It panics if Enable was not passed to gox.New.
func From(a *gox.App) *pgxpool.Pool {
	return gox.MustValue[*pgxpool.Pool](a, key{}, "postgres.From", "postgres.Enable()")
}

func newPool(cfg *Config, a *gox.App) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse POSTGRES_URL: %w", err)
	}
	pc.MaxConns = cfg.MaxConns
	pc.MinConns = cfg.MinConns
	pc.MaxConnLifetime = cfg.MaxConnLifetime
	pc.MaxConnLifetimeJitter = jitter(cfg.MaxConnLifetime)
	pc.MaxConnIdleTime = cfg.MaxConnIdleTime
	pc.HealthCheckPeriod = cfg.HealthCheckPeriod
	pc.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	pc.ConnConfig.Tracer = newTracer(a)
	pool, err := pgxpool.NewWithConfig(context.Background(), pc)
	if err != nil {
		return nil, fmt.Errorf("postgres: create pool: %w", err)
	}
	return pool, nil
}

// jitter spreads connection recycling over a window so a fleet of pools
// started together does not drop every connection at the same instant.
func jitter(lifetime time.Duration) time.Duration {
	j := lifetime / 10
	if j > 5*time.Minute {
		j = 5 * time.Minute
	}
	if j <= 0 {
		j = time.Second
	}
	return j
}

type component struct {
	cfg        *Config
	pool       *pgxpool.Pool
	log        *slog.Logger
	migrations fs.FS
	metrics    func()
}

func (c *component) Name() string { return "postgres" }

// Start pings, then migrates on a dedicated short-lived connection: the
// migration lock lives on that connection, not in the shared pool.
func (c *component) Start(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, c.cfg.ConnectTimeout)
	defer cancel()
	if err := c.pool.Ping(pingCtx); err != nil {
		return fmt.Errorf("postgres: ping %s: %w", mask(c.cfg.URL), err)
	}
	c.log.Info("postgres connected", slog.String("database", c.pool.Config().ConnConfig.Database))
	if c.migrations != nil {
		if !c.cfg.Migrate {
			c.log.Info("postgres migrations skipped (POSTGRES_MIGRATE=false)")
		} else if err := Migrate(ctx, c.cfg.URL, c.migrations); err != nil {
			return err
		}
	}
	c.metrics = registerPoolMetrics(c.pool)
	return nil
}

func (c *component) Stop(context.Context) error {
	if c.metrics != nil {
		c.metrics()
	}
	c.pool.Close()
	return nil
}

// Ready pings the database for /readyz.
func (c *component) Ready(ctx context.Context) error {
	return c.pool.Ping(ctx)
}

// Tx runs fn in a transaction: commit when fn returns nil, rollback
// otherwise (and on panic). The returned error is fn's error wrapped.
func Tx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit: %w", err)
	}
	return nil
}
