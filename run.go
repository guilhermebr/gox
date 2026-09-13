package gox

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/guilhermebr/gox/pkg/config"
)

// Run starts the app and blocks until SIGINT or SIGTERM, or until a
// component fails fatally. It returns nil after a clean shutdown.
func (a *App) Run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return a.RunContext(ctx)
}

// RunContext is Run with a caller-supplied context: cancel it to shut down.
func (a *App) RunContext(ctx context.Context) error {
	a.log.Info("starting",
		slog.String("version", a.cfg.Version),
		slog.String("environment", a.cfg.Environment),
		slog.String("config_prefix", a.prefix))
	if a.log.Enabled(ctx, slog.LevelDebug) {
		if desc, err := config.Describe(a.prefix, a.userCfg, a.sections); err == nil {
			a.log.Debug("effective configuration (secrets masked)\n" + desc)
		}
	}

	if err := a.manager.Start(ctx); err != nil {
		a.log.Error("start failed", slog.String("error", err.Error()))
		return err
	}
	a.health.SetReady(true)
	a.log.Info("ready")

	runErr := a.manager.Wait(ctx)
	if runErr != nil {
		a.log.Error("shutting down after fatal error", slog.String("error", runErr.Error()))
	} else {
		a.log.Info("shutting down")
	}

	a.health.SetReady(false)
	if d := a.cfg.Shutdown.PreStopDelay; d > 0 {
		a.log.Info("pre-stop delay", slog.Duration("duration", d))
		time.Sleep(d)
	}

	began := time.Now()
	if err := a.manager.Stop(context.Background()); err != nil {
		a.log.Error("shutdown finished with errors",
			slog.String("error", err.Error()),
			slog.Duration("duration", time.Since(began)))
		if runErr == nil {
			runErr = err
		}
	} else {
		a.log.Info("stopped", slog.Duration("duration", time.Since(began)))
	}
	a.flush()
	return runErr
}

// flush gives buffered log sinks a chance to drain. slog's stdlib handlers
// write synchronously, so today this only syncs stderr.
func (a *App) flush() {
	_ = os.Stderr.Sync()
}
