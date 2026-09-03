package postgres

import (
	"context"
	"testing"
	"time"
)

// setPoolEnv points a config prefix at a database that need not exist: pgxpool
// creates connections lazily, so New and NewOptimized return a usable pool
// object without dialing.
func setPoolEnv(t *testing.T, prefix, lifetime string) {
	t.Helper()
	for k, v := range map[string]string{
		"DATABASE_HOST":              "localhost",
		"DATABASE_PORT":              "5432",
		"DATABASE_USER":              "test",
		"DATABASE_PASSWORD":          "test",
		"DATABASE_NAME":              "testdb",
		"DATABASE_SSLMODE":           "disable",
		"DATABASE_MAX_CONN_LIFETIME": lifetime,
	} {
		t.Setenv(prefix+"_"+k, v)
	}
}

// TestNewJittersConnectionLifetime is a regression guard, not a feature test.
//
// Without jitter every connection opened at process start expires in the same
// second. A small pool does not recover from that synchronized teardown: in
// June 2026 it took three applications offline for about 21 hours, and the
// database was healthy the whole time. Any future change that drops
// MaxConnLifetimeJitter reintroduces exactly that failure, so it is asserted
// here rather than left to review.
func TestNewJittersConnectionLifetime(t *testing.T) {
	setPoolEnv(t, "JIT", "1h")

	pool, err := New(context.Background(), "JIT")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pool.Close()

	cfg := pool.Config()
	if cfg.MaxConnLifetimeJitter <= 0 {
		t.Fatalf("MaxConnLifetimeJitter = %v; a lifetime without jitter is the bug this guards",
			cfg.MaxConnLifetimeJitter)
	}
	if want := 30 * time.Minute; cfg.MaxConnLifetimeJitter != want {
		t.Errorf("MaxConnLifetimeJitter = %v, want %v (half the 1h lifetime)",
			cfg.MaxConnLifetimeJitter, want)
	}
}

// NewOptimized also jitters (optimized.go), but it cannot be exercised twice
// in one test binary until NewDatabaseMetrics stops registering collectors on
// the default Prometheus registry — a second call panics with "duplicate
// metrics collector registration attempted". Add the equivalent assertion
// there once that is fixed.
