package postgres_test

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/config"
	"github.com/guilhermebr/gox/postgres"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func setArgs(t *testing.T, args ...string) {
	t.Helper()
	old := os.Args
	os.Args = append([]string{"svc"}, args...)
	t.Cleanup(func() { os.Args = old })
}

func newApp(t *testing.T, opts ...gox.Option) (*gox.App, error) {
	t.Helper()
	return gox.New("billing", append([]gox.Option{gox.WithoutAdminServer(), gox.WithLogger(quiet())}, opts...)...)
}

func TestEnableRequiresTheURLAndNamesTheVariable(t *testing.T) {
	setArgs(t)
	_, err := newApp(t, postgres.Enable())
	if err == nil {
		t.Fatal("expected an error without BILLING_POSTGRES_URL")
	}
	for _, want := range []string{"BILLING_POSTGRES_URL", "required", "postgres.Enable()"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err %q does not mention %q", err, want)
		}
	}
}

func TestEnableBuildsThePoolFromTheURLWithoutDialing(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_POSTGRES_URL", "postgres://u:secret@db.example:5433/billing?sslmode=disable")
	t.Setenv("BILLING_POSTGRES_MAX_CONNS", "7")

	a, err := newApp(t, postgres.Enable())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	pool := postgres.From(a)
	cfg := pool.Config()
	if cfg.ConnConfig.Host != "db.example" || cfg.ConnConfig.Port != 5433 || cfg.ConnConfig.Database != "billing" {
		t.Fatalf("conn config = %s:%d/%s", cfg.ConnConfig.Host, cfg.ConnConfig.Port, cfg.ConnConfig.Database)
	}
	if cfg.MaxConns != 7 {
		t.Fatalf("MaxConns = %d", cfg.MaxConns)
	}
	if cfg.MaxConnLifetime != time.Hour {
		t.Fatalf("MaxConnLifetime = %v, want the 1h default", cfg.MaxConnLifetime)
	}
	if cfg.MaxConnLifetimeJitter <= 0 || cfg.MaxConnLifetimeJitter > cfg.MaxConnLifetime/2 {
		t.Fatalf("MaxConnLifetimeJitter = %v; pools must not recycle in lockstep", cfg.MaxConnLifetimeJitter)
	}
	pool.Close()
}

func TestFromPanicsWithTheStandardMessage(t *testing.T) {
	setArgs(t)
	a, err := newApp(t)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		want := "gox: postgres.From called but postgres.Enable() was not passed to gox.New"
		if r := recover(); r != want {
			t.Fatalf("panic = %v, want %q", r, want)
		}
	}()
	postgres.From(a)
}

func TestHelpListsThePostgresSection(t *testing.T) {
	setArgs(t, "--help")
	_, err := newApp(t, postgres.Enable())
	var help *config.HelpError
	if !errors.As(err, &help) {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"BILLING_POSTGRES_URL", "BILLING_POSTGRES_MAX_CONNS", "BILLING_POSTGRES_MIGRATE", "postgres.Enable()"} {
		if !strings.Contains(help.Usage, want) {
			t.Errorf("usage lacks %s:\n%s", want, help.Usage)
		}
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"min above max", map[string]string{"BILLING_POSTGRES_MIN_CONNS": "20", "BILLING_POSTGRES_MAX_CONNS": "5"}, "MIN_CONNS"},
		{"zero max", map[string]string{"BILLING_POSTGRES_MAX_CONNS": "0"}, "MAX_CONNS"},
		{"bad url", map[string]string{"BILLING_POSTGRES_URL": "not a url"}, "POSTGRES_URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setArgs(t)
			t.Setenv("BILLING_POSTGRES_URL", "postgres://u:p@h/db")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			_, err := newApp(t, postgres.Enable())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want mention of %s", err, tt.want)
			}
		})
	}
}

func TestWithMigrationsRequiresAFilesystem(t *testing.T) {
	setArgs(t)
	t.Setenv("BILLING_POSTGRES_URL", "postgres://u:p@h/db")
	_, err := newApp(t, postgres.Enable(postgres.WithMigrations(nil)))
	if err == nil || !strings.Contains(err.Error(), "WithMigrations") {
		t.Fatalf("err = %v", err)
	}
}

func TestLegacyNewStillReadsTheOldVariables(t *testing.T) {
	t.Setenv("DB_DATABASE_HOST", "legacy.example")
	t.Setenv("DB_DATABASE_PORT", "5432")
	t.Setenv("DB_DATABASE_USER", "u")
	t.Setenv("DB_DATABASE_PASSWORD", "p")
	t.Setenv("DB_DATABASE_NAME", "old")

	pool, err := postgres.New(t.Context(), "DB") //nolint:staticcheck // deprecated shim under test
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pool.Close()
	if pool.Config().ConnConfig.Host != "legacy.example" || pool.Config().ConnConfig.Database != "old" {
		t.Fatalf("legacy config = %+v", pool.Config().ConnConfig)
	}
}
