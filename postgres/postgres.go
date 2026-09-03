package postgres

import (
	"context"
	"fmt"

	"github.com/ardanlabs/conf/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// New creates a new Postgres connection pool using the provided context and configuration prefix.
// Deprecated: Use NewOptimized for better performance and monitoring capabilities.
func New(ctx context.Context, prefix string) (*pgxpool.Pool, error) {
	var cfg Config

	_, err := conf.Parse(prefix, &cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing postgres config from prefix [%s]: %w", prefix, err)
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.ConnectionString())
	if err != nil {
		return nil, fmt.Errorf("parsing postgres pool config: %w", err)
	}
	// Stagger connection expiry so a pool of conns all born together (e.g. at
	// boot) doesn't recycle them simultaneously at MaxConnLifetime — a
	// synchronized mass-reconnect can wedge a small pool behind a shared
	// proxy. The DSN form can't express jitter, so set it on the parsed config.
	poolCfg.MaxConnLifetimeJitter = cfg.DatabaseMaxConnLifetime / 2

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to setup postgres: %w", err)
	}

	return pool, nil
}
