// Command minimal is the smallest gox service: no HTTP, one user component
// and one periodic job. It still gets config loading, structured logs, and
// the admin server with /healthz, /readyz, /version and pprof on :9090.
//
//	MINIMAL_LOG_LEVEL=debug go run ./examples/minimal
//	curl localhost:9090/readyz
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/guilhermebr/gox"
)

// greeter is a user component: anything with Name, Start and Stop.
type greeter struct {
	log *slog.Logger
}

func (g *greeter) Name() string { return "greeter" }

func (g *greeter) Start(context.Context) error {
	g.log.Info("hello from greeter")
	return nil
}

func (g *greeter) Stop(context.Context) error {
	g.log.Info("goodbye from greeter")
	return nil
}

// Ready makes the greeter part of /readyz.
func (g *greeter) Ready(context.Context) error { return nil }

func main() {
	a := gox.MustNew("minimal",
		gox.WithVersion("0.1.0"),
		gox.Periodic("heartbeat", 10*time.Second, func(context.Context) error {
			slog.Info("heartbeat") // gox installs its logger as the slog default
			return nil
		}),
	)
	a.Add(&greeter{log: a.Log()})
	if err := a.Run(); err != nil {
		os.Exit(1) // Run already logged the failure
	}
}
