package postgres

import (
	"context"
	"fmt"
	"net/url"

	"github.com/ardanlabs/conf/v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

// legacyConfig is the pre-framework environment layout kept for New.
type legacyConfig struct {
	DatabaseName     string `conf:"env:DATABASE_NAME,required"`
	DatabaseUser     string `conf:"env:DATABASE_USER,required"`
	DatabasePassword string `conf:"env:DATABASE_PASSWORD,required,mask"`
	DatabaseHost     string `conf:"env:DATABASE_HOST,default:localhost"`
	DatabasePort     string `conf:"env:DATABASE_PORT,default:5432"`
	DatabaseSSLMode  string `conf:"env:DATABASE_SSLMODE,default:disable"`
	PoolMinSize      int32  `conf:"env:DATABASE_POOL_MIN_SIZE,default:2"`
	PoolMaxSize      int32  `conf:"env:DATABASE_POOL_MAX_SIZE,default:10"`
}

// New builds a pool from the pre-framework variables
// (<PREFIX>_DATABASE_HOST, _PORT, _USER, _PASSWORD, _NAME, _SSLMODE,
// _POOL_MIN_SIZE, _POOL_MAX_SIZE). It does not dial.
//
// Deprecated: pass postgres.Enable() to gox.New and read the pool with
// postgres.From. New is removed one minor version after the root package
// reaches v1.
func New(ctx context.Context, prefix string) (*pgxpool.Pool, error) {
	var lc legacyConfig
	if _, err := conf.Parse(prefix, &lc); err != nil {
		return nil, fmt.Errorf("postgres: parsing config: %w", err)
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(lc.DatabaseUser, lc.DatabasePassword),
		Host:     lc.DatabaseHost + ":" + lc.DatabasePort,
		Path:     "/" + lc.DatabaseName,
		RawQuery: "sslmode=" + lc.DatabaseSSLMode,
	}
	pc, err := pgxpool.ParseConfig(u.String())
	if err != nil {
		return nil, fmt.Errorf("postgres: parse config: %w", err)
	}
	pc.MinConns = lc.PoolMinSize
	pc.MaxConns = lc.PoolMaxSize
	pc.MaxConnLifetimeJitter = jitter(pc.MaxConnLifetime)
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("postgres: create pool: %w", err)
	}
	return pool, nil
}
