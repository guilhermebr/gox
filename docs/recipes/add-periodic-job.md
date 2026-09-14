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
