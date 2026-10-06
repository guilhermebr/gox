# API and workers from one codebase

```go path=internal/app/app.go
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"
)

// Config is shared by every binary. Only the single-binary main at the end
// of this recipe acts on Role.
type Config struct {
	gox.BaseConfig
	SyncEvery time.Duration `conf:"default:5m"`
	Role      string        `conf:"default:all,help:all | api | worker"`
}

// Validate refuses a role no main acts on, so a typo cannot start a binary
// that serves nothing.
func (c *Config) Validate() error {
	switch c.Role {
	case "all", "api", "worker":
		return nil
	}
	return fmt.Errorf("BILLING_ROLE must be all, api or worker, got %q", c.Role)
}

// Base is what every binary of the service is made of.
func Base(cfg *Config) []gox.Option {
	return []gox.Option{gox.WithConfig(cfg), postgres.Enable()}
}

// Routes registers the API's handlers.
func Routes(a *gox.App) {
	db := postgres.From(a)
	a.HandleFunc("GET /invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		var id string
		err := db.QueryRow(r.Context(), "SELECT id FROM invoices WHERE id = $1", r.PathValue("id")).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			gox.Error(w, r, gox.NotFound("invoice %s", r.PathValue("id")))
			return
		}
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		_ = gox.JSON(w, http.StatusOK, map[string]string{"id": id})
	})
}

// Sync is the workers' periodic job. Every worker replica runs it: run one
// replica, or use gox/jobs (add-background-job.md) for work that must run
// once, with BILLING_JOBS_WORK=false on the API binary.
func Sync(every time.Duration) gox.Option {
	return gox.Periodic("sync", every, func(ctx context.Context) error {
		slog.InfoContext(ctx, "syncing")
		return nil
	})
}
```

```go path=cmd/api/main.go
package main

import (
	"os"

	"github.com/guilhermebr/gox"

	"example.com/shop/internal/app"
)

func main() {
	var cfg app.Config
	a := gox.MustNew("billing", append(app.Base(&cfg), gox.HTTP())...)
	app.Routes(a)
	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

```go path=cmd/worker/main.go
package main

import (
	"os"
	"time"

	"github.com/guilhermebr/gox"

	"example.com/shop/internal/app"
)

func main() {
	var cfg app.Config
	// The interval comes from config before New: add-periodic-job.md,
	// "Interval from config".
	every := 5 * time.Minute
	if err := gox.LoadConfig("billing", &cfg); err == nil {
		every = cfg.SyncEvery
	}
	// Same prefix (BILLING_*), same Postgres section, no HTTP server; the
	// admin server still serves /readyz and /metrics for this binary.
	a := gox.MustNew("billing", append(app.Base(&cfg), app.Sync(every))...)
	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

One binary that picks its role from the environment uses the same wiring:
`gox.LoadConfig` reads the role before the app exists, as in `cmd/worker`.

```go path=cmd/shop/main.go
package main

import (
	"os"
	"time"

	"github.com/guilhermebr/gox"

	"example.com/shop/internal/app"
)

func main() {
	var cfg app.Config
	every := 5 * time.Minute
	if err := gox.LoadConfig("billing", &cfg); err == nil {
		every = cfg.SyncEvery
	}
	opts := app.Base(&cfg)
	if cfg.Role == "all" || cfg.Role == "api" {
		opts = append(opts, gox.HTTP())
	}
	if cfg.Role == "all" || cfg.Role == "worker" {
		opts = append(opts, app.Sync(every))
	}
	a := gox.MustNew("billing", opts...)
	if a.HasHTTP() {
		app.Routes(a)
	}
	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```
