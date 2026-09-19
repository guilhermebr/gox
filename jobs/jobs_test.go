package jobs_test

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/jobs"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func setArgs(t *testing.T) {
	t.Helper()
	old := os.Args
	os.Args = []string{"svc"}
	t.Cleanup(func() { os.Args = old })
}

func noPool(*gox.App) *pgxpool.Pool { return nil }

func TestFromPanicsWithoutEnable(t *testing.T) {
	setArgs(t)
	a, _ := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()))
	defer func() {
		if r := recover(); r != "gox: jobs.From called but jobs.Enable() was not passed to gox.New" {
			t.Fatalf("panic = %v", r)
		}
	}()
	jobs.From(a)
}

func TestConfigIsValidated(t *testing.T) {
	setArgs(t)
	t.Setenv("SHOP_JOBS_MAX_WORKERS", "0")
	_, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), jobs.Enable(noPool))
	if err == nil || !strings.Contains(err.Error(), "JOBS_MAX_WORKERS") {
		t.Fatalf("err = %v", err)
	}
}

func TestANilPoolNamesTheFix(t *testing.T) {
	setArgs(t)
	_, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), jobs.Enable(noPool))
	if err == nil || !strings.Contains(err.Error(), "pass postgres.From") {
		t.Fatalf("err = %v", err)
	}
}

func TestABadScheduleFailsAtRegistration(t *testing.T) {
	setArgs(t)
	// A pool that never connects is enough: nothing here touches the database.
	p, err := pgxpool.New(t.Context(), "postgres://u:p@127.0.0.1:1/db")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	a, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), jobs.Enable(func(*gox.App) *pgxpool.Pool { return p }))
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.Schedule(a, "every tuesday", greet{}); err == nil || !strings.Contains(err.Error(), "every tuesday") {
		t.Fatalf("err = %v", err)
	}
}

type greet struct {
	Name string `json:"name"`
}

func (greet) Kind() string { return "greet" }
