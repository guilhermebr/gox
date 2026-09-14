# Add a health check

```go path=main.go
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/guilhermebr/gox"
)

func main() {
	a := gox.MustNew("billing", gox.HTTP(), gox.HTTPClient())

	// Readiness: should this instance receive traffic? Fails /readyz (503).
	// Components that implement Ready(ctx) error are registered automatically.
	a.Health().AddReadiness("payments-api", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://payments.example.com/healthz", nil)
		resp, err := a.HTTPClient().Do(req)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return errors.New("payments api unhealthy")
		}
		return nil
	})

	// Liveness: should this process be restarted? Fails /healthz (503).
	a.Health().AddLiveness("not-deadlocked", func(context.Context) error { return nil })

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```
