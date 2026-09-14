# One binary, several roles (ROLE picks the components)

```go path=main.go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"
)

type Config struct {
	gox.BaseConfig
	Role string `conf:"default:all,help:all | api | worker"`
}

func main() {
	// Options are values: build the list from the role. gox.LoadConfig reads
	// the same struct New will load, so there is one source of truth.
	var cfg Config
	if err := gox.LoadConfig("mailvault", &cfg); err != nil {
		os.Exit(1)
	}
	opts := []gox.Option{gox.WithConfig(&cfg)}
	if cfg.Role == "all" || cfg.Role == "api" {
		opts = append(opts, gox.HTTP(), postgres.Enable())
	}
	if cfg.Role == "all" || cfg.Role == "worker" {
		opts = append(opts, gox.Periodic("deliver", time.Minute, func(ctx context.Context) error {
			slog.InfoContext(ctx, "delivering")
			return nil
		}))
	}
	a := gox.MustNew("mailvault", opts...)

	if a.HasHTTP() {
		a.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("api")) })
	}
	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```
