package temporal

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/contrib/opentelemetry"
	"go.temporal.io/sdk/interceptor"
	sdklog "go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/pkg/lifecycle"
)

type (
	clientKey  struct{}
	featureKey struct{}
)

type feature struct {
	cfg    *Config
	client client.Client
	stop   worker.Options // defaults applied to every worker

	mu      sync.Mutex
	workers map[string]worker.Worker
	order   []string
}

// Enable declares the Temporal client: it registers the TEMPORAL config
// section and a component that checks the connection at boot (an
// unreachable server fails the start, not the first workflow), exposes
// readiness, starts the workers registered with Worker, and on shutdown
// lets running activities finish within the shutdown budget before closing
// the client. Workflows and activities are traced with the app's
// OpenTelemetry setup and log through the service logger.
func Enable() gox.Option {
	return func(b *gox.Builder) error {
		cfg := &Config{}
		b.ConfigSection("TEMPORAL", cfg, "temporal.Enable()")
		f := &feature{cfg: cfg, workers: map[string]worker.Worker{}}
		b.Component(gox.StageUser, func(a *gox.App) (lifecycle.Component, error) {
			opts := client.Options{
				HostPort:  cfg.Address,
				Namespace: cfg.Namespace,
				Logger:    sdklog.NewStructuredLogger(a.Log().With(slog.String("component", "temporal"))),
			}
			if tlsCfg := cfg.tlsConfig(); tlsCfg != nil {
				opts.ConnectionOptions.TLS = tlsCfg
			}
			if cfg.APIKey != "" {
				opts.Credentials = client.NewAPIKeyStaticCredentials(cfg.APIKey)
			}
			if tracing, err := opentelemetry.NewTracingInterceptor(opentelemetry.TracerOptions{}); err == nil {
				opts.Interceptors = []interceptor.ClientInterceptor{tracing}
			}
			// Lazy: New needs no server, like the postgres pool. Start checks it.
			c, err := client.NewLazyClient(opts)
			if err != nil {
				return nil, fmt.Errorf("temporal: %w", err)
			}
			f.client = c
			f.stop = worker.Options{WorkerStopTimeout: a.Config().Shutdown.Timeout}
			b.Set(clientKey{}, c)
			b.Set(featureKey{}, f)
			return &component{f: f}, nil
		})
		return nil
	}
}

// From returns the Temporal client, for starting, signalling and querying
// workflows. It panics if Enable was not passed to gox.New.
func From(a *gox.App) client.Client {
	return gox.MustValue[client.Client](a, clientKey{}, "temporal.From", "temporal.Enable()")
}

// Worker returns the worker for a task queue, creating it on first use.
// Call it after New and before Run and register workflows and activities on
// the result; the app starts and stops it. Unless opts says otherwise,
// running activities get the app's shutdown timeout to finish.
func Worker(a *gox.App, taskQueue string, opts worker.Options) worker.Worker {
	f := gox.MustValue[*feature](a, featureKey{}, "temporal.Worker", "temporal.Enable()")
	f.mu.Lock()
	defer f.mu.Unlock()
	if w, ok := f.workers[taskQueue]; ok {
		return w
	}
	if opts.WorkerStopTimeout == 0 {
		opts.WorkerStopTimeout = f.stop.WorkerStopTimeout
	}
	w := worker.New(f.client, taskQueue, opts)
	f.workers[taskQueue] = w
	f.order = append(f.order, taskQueue)
	return w
}

type component struct {
	f       *feature
	started []worker.Worker
}

func (c *component) Name() string { return "temporal" }

func (c *component) Start(ctx context.Context) error {
	if err := c.Ready(ctx); err != nil {
		return err
	}
	if !c.f.cfg.Work {
		return nil
	}
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	for _, queue := range c.f.order {
		w := c.f.workers[queue]
		if err := w.Start(); err != nil {
			return fmt.Errorf("temporal: start worker %q: %w", queue, err)
		}
		c.started = append(c.started, w)
	}
	return nil
}

// Ready reports whether the Temporal frontend answers.
func (c *component) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.f.cfg.ConnectTimeout)
	defer cancel()
	if _, err := c.f.client.CheckHealth(ctx, &client.CheckHealthRequest{}); err != nil {
		return fmt.Errorf("temporal: %s is not reachable: %w", c.f.cfg.Address, err)
	}
	return nil
}

func (c *component) Stop(context.Context) error {
	for i := len(c.started) - 1; i >= 0; i-- {
		c.started[i].Stop() // waits for running activities, up to WorkerStopTimeout
	}
	c.f.client.Close()
	return nil
}
