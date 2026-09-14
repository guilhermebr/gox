# Add a background worker (a long-running loop)

```go path=main.go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/guilhermebr/gox"
)

// mailer drains a queue until the app stops. It is a Component (Name,
// Start, Stop) and a Runner (Run): gox starts Run in its own goroutine,
// treats a non-nil return as fatal, and cancels ctx on shutdown.
type mailer struct {
	log   *slog.Logger
	queue chan string
}

func (m *mailer) Name() string { return "mailer" }

func (m *mailer) Start(context.Context) error { return nil } // open connections here; must return quickly

func (m *mailer) Stop(context.Context) error { return nil } // close connections; Run has already returned

func (m *mailer) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err() // cancellation after Stop is not an error
		case to := <-m.queue:
			m.log.InfoContext(ctx, "sending", slog.String("to", to))
		}
	}
}

// Ready makes the worker part of /readyz.
func (m *mailer) Ready(context.Context) error { return nil }

func main() {
	a := gox.MustNew("notifier", gox.HTTP())
	m := &mailer{log: a.Log(), queue: make(chan string, 100)}
	a.Add(m) // StageUser: starts after datastores, before the HTTP server; stops in reverse

	a.Mux().HandleFunc("POST /notify/{to}", func(w http.ResponseWriter, r *http.Request) {
		select {
		case m.queue <- r.PathValue("to"):
			w.WriteHeader(http.StatusAccepted)
		case <-time.After(time.Second):
			gox.Error(w, r, gox.ResourceExhausted("queue is full"))
		}
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```
