//go:build integration

package postgres_test

import (
	"context"
	"embed"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"
)

//go:embed testdata/migrations/*.sql
var migrations embed.FS

func databaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	return url
}

func waitReady(t *testing.T, a *gox.App) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if a.Health().IsReady() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("app did not become ready")
}

func TestIntegrationStartPingsRunsMigrationsAndReportsReady(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_POSTGRES_URL", databaseURL(t))

	a, err := newApp(t, postgres.Enable(postgres.WithMigrations(migrations)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	pool := postgres.From(a)
	if _, err := pool.Exec(context.Background(), "DROP TABLE IF EXISTS gox_items, schema_migrations"); err != nil {
		t.Fatalf("reset: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	waitReady(t, a)

	rep := a.Health().Ready(context.Background())
	if rep.Checks["postgres"].Status != "ok" {
		t.Fatalf("readiness = %+v", rep)
	}
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM gox_items").Scan(&n); err != nil {
		t.Fatalf("migration did not create gox_items: %v", err)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("RunContext = %v", err)
	}
	if err := pool.Ping(context.Background()); err == nil {
		t.Fatal("pool must be closed after Stop")
	}
}

func TestIntegrationTxCommitsAndRollsBack(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_POSTGRES_URL", databaseURL(t))
	a, err := newApp(t, postgres.Enable(postgres.WithMigrations(migrations)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	waitReady(t, a)
	defer func() {
		cancel()
		<-done
	}()
	pool := postgres.From(a)
	if _, err := pool.Exec(ctx, "DELETE FROM gox_items"); err != nil {
		t.Fatal(err)
	}

	err = postgres.Tx(ctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO gox_items (name) VALUES ($1)", "kept")
		return err
	})
	if err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	boom := errors.New("boom")
	err = postgres.Tx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO gox_items (name) VALUES ($1)", "dropped"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("rollback tx = %v", err)
	}

	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM gox_items").Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d err = %v; the rolled back row must not exist", n, err)
	}
}

func TestIntegrationMigrateIsUsableOutsideRun(t *testing.T) {
	url := databaseURL(t)
	if err := postgres.Migrate(context.Background(), url, migrations); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Idempotent: a second run is a no-op.
	if err := postgres.Migrate(context.Background(), url, migrations); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

func TestIntegrationBootFailsFastOnUnreachableDatabase(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_POSTGRES_URL", "postgres://u:p@127.0.0.1:1/nope?connect_timeout=1")
	a, err := newApp(t, postgres.Enable())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = a.RunContext(ctx)
	if err == nil || a.Health().IsReady() {
		t.Fatalf("RunContext = %v ready=%v; a lazy pool must not report healthy", err, a.Health().IsReady())
	}
}
