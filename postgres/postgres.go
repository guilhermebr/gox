package postgres

import (
	"context"
	"fmt"

	"github.com/ardanlabs/conf/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// New creates a new Postgres connection pool using the provided context and configuration prefix.
func New(ctx context.Context, prefix string) (*pgxpool.Pool, error) {
	var cfg Config

	_, err := conf.Parse(prefix, &cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing postgres config from prefix [%s]: %w", prefix, err)
	}

	pool, err := pgxpool.New(ctx, cfg.ConnectionString())
	if err != nil {
		return nil, fmt.Errorf("failed to setup postgres: %w", err)
	}

	return pool, nil
}
