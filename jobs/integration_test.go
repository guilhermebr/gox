//go:build integration

package jobs_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/jobs"
)

type greeter struct {
	river.WorkerDefaults[greet]
	mu    sync.Mutex
	names []string
}

func (g *greeter) Work(_ context.Context, j *river.Job[greet]) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.names = append(g.names, j.Args.Name)
	return nil
}

func (g *greeter) seen() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.names...)
}

type flaky struct{}

func (flaky) Kind() string { return "flaky" }

type flakyWorker struct {
	river.WorkerDefaults[flaky]
	attempts atomic.Int32
}

func (w *flakyWorker) Work(context.Context, *river.Job[flaky]) error {
	if w.attempts.Add(1) == 1 {
		return errors.New("first attempt fails")
	}
	return nil
}

// Retry immediately so the test does not wait for River's backoff.
func (w *flakyWorker) NextRetry(*river.Job[flaky]) time.Time { return time.Now() }

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	p, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	_, _ = p.Exec(context.Background(), "TRUNCATE river_job") // absent on the first run
	return p
}

func run(t *testing.T, p *pgxpool.Pool, register func(a *gox.App)) *gox.App {
	t.Helper()
	setArgs(t)
	a, err := gox.New("shop", gox.WithoutAdminServer(), gox.WithLogger(quiet()), jobs.Enable(func(*gox.App) *pgxpool.Pool { return p }))
	if err != nil {
		t.Fatal(err)
	}
	register(a)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.RunContext(ctx) }()
	for !a.Health().IsReady() {
		select {
		case err := <-done: // a failed start must fail the test, not hang it
			t.Fatalf("RunContext: %v", err)
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("RunContext = %v", err)
		}
	})
	return a
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestInsertedJobsAreWorkedAndFailuresRetry(t *testing.T) {
	p := pool(t)
	g, f := &greeter{}, &flakyWorker{}
	a := run(t, p, func(a *gox.App) {
		jobs.Register(a, g)
		jobs.Register(a, f)
	})
	ctx := context.Background()
	if _, err := jobs.From(a).Insert(ctx, greet{Name: "ana"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.From(a).Insert(ctx, flaky{}, nil); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the greeting", func() bool { return len(g.seen()) == 1 && g.seen()[0] == "ana" })
	eventually(t, "the retry", func() bool { return f.attempts.Load() == 2 })
}

func TestInsertTxFollowsTheTransaction(t *testing.T) {
	p := pool(t)
	g := &greeter{}
	a := run(t, p, func(a *gox.App) { jobs.Register(a, g) })
	ctx := context.Background()

	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.From(a).InsertTx(ctx, tx, greet{Name: "rolled back"}, nil); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(ctx)

	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		_, err := jobs.From(a).InsertTx(ctx, tx, greet{Name: "committed"}, nil)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the committed job", func() bool { return len(g.seen()) == 1 })
	time.Sleep(300 * time.Millisecond)
	if got := g.seen(); len(got) != 1 || got[0] != "committed" {
		t.Fatalf("worked = %v", got)
	}
}

func TestScheduledJobsRun(t *testing.T) {
	p := pool(t)
	g := &greeter{}
	run(t, p, func(a *gox.App) {
		jobs.Register(a, g)
		if err := jobs.Schedule(a, "@every 1s", greet{Name: "tick"}); err != nil {
			t.Fatal(err)
		}
	})
	eventually(t, "a scheduled run", func() bool { return len(g.seen()) >= 1 })
}

func TestAnInsertOnlyProcessDoesNotWork(t *testing.T) {
	p := pool(t)
	t.Setenv("SHOP_JOBS_WORK", "false")
	g := &greeter{}
	a := run(t, p, func(a *gox.App) { jobs.Register(a, g) })
	if _, err := jobs.From(a).Insert(context.Background(), greet{Name: "later"}, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if len(g.seen()) != 0 {
		t.Fatal("JOBS_WORK=false must only insert")
	}
}

// Enabling jobs before the first worker exists must not stop the service
// from booting. River still refuses to enqueue a kind no worker is
// registered for, so every process registers its workers and an
// insert-only one sets JOBS_WORK=false.
func TestNoRegisteredWorkersStillBoots(t *testing.T) {
	p := pool(t)
	a := run(t, p, func(*gox.App) {})
	if !a.Health().IsReady() {
		t.Fatal("the app must be ready")
	}
	_, err := jobs.From(a).Insert(context.Background(), greet{Name: "queued"}, nil)
	if err == nil || !strings.Contains(err.Error(), "greet") {
		t.Fatalf("Insert of an unregistered kind = %v", err)
	}
}
