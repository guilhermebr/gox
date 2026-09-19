package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
	"github.com/robfig/cron/v3"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/lifecycle"
)

type (
	clientKey  struct{}
	featureKey struct{}
)

type feature struct {
	cfg     *Config
	workers *river.Workers
	client  *river.Client[pgx.Tx]
}

// Enable declares the job queue on the pool that pool returns; pass
// postgres.From. It registers the JOBS config section and a component that
// migrates River's tables, works jobs with the app (after the datastores are
// up, before the HTTP server), and drains them on shutdown: running jobs get
// the shutdown budget to finish, then their contexts are cancelled.
//
// Register workers and schedules after New and before Run:
//
//	jobs.Register(a, &SendReceipt{})
//	jobs.Schedule(a, "0 3 * * *", PurgeExports{})
func Enable(pool func(*gox.App) *pgxpool.Pool) gox.Option {
	return func(b *gox.Builder) error {
		cfg := &Config{}
		b.ConfigSection("JOBS", cfg, "jobs.Enable()")
		f := &feature{cfg: cfg, workers: river.NewWorkers()}
		b.Component(gox.StageUser, func(a *gox.App) (lifecycle.Component, error) {
			p := pool(a)
			if p == nil {
				return nil, fmt.Errorf("jobs: Enable got a nil pool; pass postgres.From and add postgres.Enable() to gox.New")
			}
			rc := &river.Config{
				Workers:      f.workers,
				Logger:       a.Log().With(slog.String("component", "jobs")),
				ErrorHandler: &errorLogger{log: a.Log()},
			}
			if cfg.Work {
				rc.Queues = map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: cfg.MaxWorkers}}
			}
			client, err := river.NewClient(riverpgxv5.New(p), rc)
			if err != nil {
				return nil, fmt.Errorf("jobs: %w", err)
			}
			f.client = client
			b.Set(clientKey{}, client)
			b.Set(featureKey{}, f)
			return &component{f: f, pool: p}, nil
		})
		return nil
	}
}

// From returns the River client: Insert and InsertTx enqueue jobs, the
// latter inside the caller's transaction so a job exists only if the change
// that needs it committed. It panics if Enable was not passed to gox.New.
func From(a *gox.App) *river.Client[pgx.Tx] {
	return gox.MustValue[*river.Client[pgx.Tx]](a, clientKey{}, "jobs.From", "jobs.Enable()")
}

// Register adds the worker for one kind of job. Call it after New and
// before Run; a kind registered twice panics.
func Register[T river.JobArgs](a *gox.App, w river.Worker[T]) {
	river.AddWorker(gox.MustValue[*feature](a, featureKey{}, "jobs.Register", "jobs.Enable()").workers, w)
}

// Schedule enqueues args on a schedule: a cron expression ("0 3 * * *",
// optionally prefixed with a zone, "CRON_TZ=America/New_York 0 0 * * *") or
// an interval ("@every 5m"). Only one process of a fleet enqueues each run.
func Schedule(a *gox.App, spec string, args river.JobArgs) error {
	f := gox.MustValue[*feature](a, featureKey{}, "jobs.Schedule", "jobs.Enable()")
	schedule, err := cron.ParseStandard(spec)
	if err != nil {
		return fmt.Errorf("jobs: schedule %q: %w", spec, err)
	}
	f.client.PeriodicJobs().Add(river.NewPeriodicJob(schedule, func() (river.JobArgs, *river.InsertOpts) { return args, nil }, nil))
	return nil
}

type component struct {
	f       *feature
	pool    *pgxpool.Pool
	started bool
}

func (c *component) Name() string { return "jobs" }

func (c *component) Start(ctx context.Context) error {
	if c.f.cfg.Migrate {
		m, err := rivermigrate.New(riverpgxv5.New(c.pool), nil)
		if err != nil {
			return fmt.Errorf("jobs: migrate: %w", err)
		}
		if _, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
			return fmt.Errorf("jobs: migrate: %w", err)
		}
	}
	if !c.f.cfg.Work {
		return nil
	}
	// Jobs outlive the start-up context; Stop ends them.
	if err := c.f.client.Start(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("jobs: start: %w", err)
	}
	c.started = true
	return nil
}

func (c *component) Stop(ctx context.Context) error {
	if !c.started {
		return nil
	}
	if err := c.f.client.Stop(ctx); err != nil {
		hard, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = c.f.client.StopAndCancel(hard)
		return fmt.Errorf("jobs: stop: %w (running jobs were cancelled)", err)
	}
	return nil
}

// errorLogger reports failed attempts and panics with the service logger;
// River retries them either way.
type errorLogger struct{ log *slog.Logger }

func (e *errorLogger) HandleError(ctx context.Context, job *rivertype.JobRow, err error) *river.ErrorHandlerResult {
	e.log.WarnContext(ctx, "job failed", slog.String("kind", job.Kind), slog.Int64("job_id", job.ID),
		slog.Int("attempt", job.Attempt), slog.Int("max_attempts", job.MaxAttempts), slog.String("error", err.Error()))
	return nil
}

func (e *errorLogger) HandlePanic(ctx context.Context, job *rivertype.JobRow, panicVal any, trace string) *river.ErrorHandlerResult {
	e.log.ErrorContext(ctx, "job panicked", slog.String("kind", job.Kind), slog.Int64("job_id", job.ID),
		slog.Int("attempt", job.Attempt), slog.Any("panic", panicVal), slog.String("trace", trace))
	return nil
}
