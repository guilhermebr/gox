//go:build integration

package postgres_test

import (
	"context"
	"embed"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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

// A database that another framework migrated already has a schema_migrations
// table with its own layout; the service keeps its versions elsewhere.
func TestIntegrationMigrationsTableCanBeRenamed(t *testing.T) {
	url := databaseURL(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	reset := "DROP TABLE IF EXISTS gox_items, schema_migrations, service_migrations"
	if _, err := pool.Exec(ctx, reset+"; CREATE TABLE schema_migrations (version varchar PRIMARY KEY); INSERT INTO schema_migrations VALUES ('20260101120000')"); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, reset) }()

	if err := postgres.Migrate(ctx, url, migrations, postgres.WithMigrationsTable("service_migrations")); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var foreign string
	if err := pool.QueryRow(ctx, "SELECT version FROM schema_migrations").Scan(&foreign); err != nil || foreign != "20260101120000" {
		t.Fatalf("the other framework's table must be untouched: %q %v", foreign, err)
	}
	var version int
	if err := pool.QueryRow(ctx, "SELECT version FROM service_migrations").Scan(&version); err != nil || version == 0 {
		t.Fatalf("service_migrations: %d %v", version, err)
	}

	// The same through Enable, from config.
	setArgs(t)
	t.Setenv("BILLING_POSTGRES_URL", url)
	t.Setenv("BILLING_POSTGRES_MIGRATIONS_TABLE", "service_migrations")
	a, err := newApp(t, postgres.Enable(postgres.WithMigrations(migrations)))
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- a.RunContext(runCtx) }()
	waitReady(t, a)
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// Settings are what triggers and row-level security read with
// current_setting(): they must be set on the transaction's own connection
// and vanish with it.
func TestIntegrationTxWithSetsLocalSettings(t *testing.T) {
	url := databaseURL(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	var actor, tenant string
	err = postgres.TxWith(ctx, pool, map[string]string{"app.actor": "user_01", "app.tenant": "org'9"}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT current_setting('app.actor'), current_setting('app.tenant')").Scan(&actor, &tenant)
	})
	if err != nil || actor != "user_01" || tenant != "org'9" {
		t.Fatalf("inside = %q %q %v", actor, tenant, err)
	}
	var after string
	if err := pool.QueryRow(ctx, "SELECT coalesce(current_setting('app.actor', true), '')").Scan(&after); err != nil || after != "" {
		t.Fatalf("the setting must not outlive the transaction: %q %v", after, err)
	}
	err = postgres.TxWith(ctx, pool, map[string]string{"not a valid name": "x"}, func(pgx.Tx) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "not a valid name") {
		t.Fatalf("a bad setting name: %v", err)
	}
}
