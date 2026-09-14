# Two binaries, one wiring package (API and workers)

```go path=internal/app/app.go
package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"
)

// Config is shared by every binary.
type Config struct {
	gox.BaseConfig
	SyncEvery time.Duration `conf:"default:5m"`
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
		if err := db.QueryRow(r.Context(), "SELECT id FROM invoices WHERE id = $1", r.PathValue("id")).Scan(&id); err != nil {
			gox.Error(w, r, gox.NotFound("invoice %s", r.PathValue("id")))
			return
		}
		_ = gox.JSON(w, http.StatusOK, map[string]string{"id": id})
	})
}

// Sync is the workers' periodic job.
func Sync(cfg *Config) gox.Option {
	return gox.Periodic("sync", cfg.SyncEvery, func(ctx context.Context) error {
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

	"github.com/guilhermebr/gox"

	"example.com/shop/internal/app"
)

func main() {
	var cfg app.Config
	// Same prefix (BILLING_*), same Postgres section, no HTTP server; the
	// admin server still serves /readyz and /metrics for this binary.
	a := gox.MustNew("billing", append(app.Base(&cfg), app.Sync(&cfg))...)
	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```
