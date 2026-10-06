# Add a periodic job

```go path=main.go
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/guilhermebr/gox"
)

func main() {
	a := gox.MustNew("billing",
		gox.HTTP(),
		// Runs every 15 minutes with a random initial stagger, recovers from
		// panics, logs errors and continues, and stops with the app.
		gox.Periodic("expire-trials", 15*time.Minute, func(ctx context.Context) error {
			slog.InfoContext(ctx, "expiring trials")
			return nil // a non-nil error is logged; the job runs again next tick
		}),
	)
	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

## Interval from config

`gox.Periodic` takes its interval when the option list is built, before `New`
loads config. Read it first with `gox.LoadConfig`, which loads the struct
`New` will load:

```go path=cmd/billing/main.go
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/guilhermebr/gox"
)

// Config adds the job's interval to the framework's settings.
type Config struct {
	gox.BaseConfig
	ExpireTrialsEvery time.Duration `conf:"default:15m,help:how often expire-trials runs"`
}

func main() {
	var cfg Config
	// BILLING_EXPIRE_TRIALS_EVERY=1h. Do not exit when LoadConfig fails: it
	// also fails for --help and --version, and MustNew loads the same struct,
	// prints why and exits. The placeholder never runs; it must be above zero.
	every := time.Minute
	if err := gox.LoadConfig("billing", &cfg); err == nil {
		every = cfg.ExpireTrialsEvery
	}
	a := gox.MustNew("billing",
		gox.WithConfig(&cfg),
		gox.HTTP(),
		gox.Periodic("expire-trials", every, expireTrials),
	)
	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}

// expireTrials is the job: a plain function, so a test calls it directly.
func expireTrials(ctx context.Context) error {
	slog.InfoContext(ctx, "expiring trials")
	return nil
}
```

`LoadConfig` always reads the default prefix (`BILLING_*`); it ignores
`gox.WithConfigPrefix` and `gox.WithEnvAlias`.
