# Run Temporal workflows (durable, multi-step processes)

Use `gox/jobs` for single tasks that run later and retry. Use Temporal when
a process has several steps, waits for days or for a signal, or must
compensate earlier steps; the engine records every step and resumes after a
crash or a deploy.

```go path=main.go
package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/providers/temporal"
)

// Onboard is the workflow: deterministic code that calls activities.
func Onboard(ctx workflow.Context, customerID string) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
	if err := workflow.ExecuteActivity(ctx, SendWelcome, customerID).Get(ctx, nil); err != nil {
		return err
	}
	if err := workflow.Sleep(ctx, 72*time.Hour); err != nil { // survives restarts
		return err
	}
	return workflow.ExecuteActivity(ctx, SendCheckIn, customerID).Get(ctx, nil)
}

// Activities do the I/O; they retry on error.
func SendWelcome(context.Context, string) error { return nil }

// SendCheckIn follows up three days later.
func SendCheckIn(context.Context, string) error { return nil }

func main() {
	// BILLING_TEMPORAL_ADDRESS (default localhost:7233), BILLING_TEMPORAL_NAMESPACE.
	// Temporal Cloud: BILLING_TEMPORAL_API_KEY, or TLS_CERT and TLS_KEY for mTLS.
	// An API next to a separate worker binary sets BILLING_TEMPORAL_WORK=false.
	a := gox.MustNew("billing", gox.HTTP(), temporal.Enable())

	// One worker per task queue; the app starts it and, on shutdown, lets
	// running activities finish within the shutdown timeout.
	w := temporal.Worker(a, "onboarding", worker.Options{})
	w.RegisterWorkflow(Onboard)
	w.RegisterActivity(SendWelcome)
	w.RegisterActivity(SendCheckIn)

	a.HandleFunc("POST /customers/{id}/onboarding", func(rw http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		// A deterministic workflow id makes the call idempotent.
		run, err := temporal.From(a).ExecuteWorkflow(r.Context(),
			client.StartWorkflowOptions{ID: "onboard-" + id, TaskQueue: "onboarding"}, Onboard, id)
		if err != nil {
			gox.Error(rw, r, gox.WrapError(err, gox.CodeUnavailable, "could not start the onboarding"))
			return
		}
		_ = gox.JSON(rw, http.StatusAccepted, map[string]string{"workflow_id": run.GetID(), "run_id": run.GetRunID()})
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

`temporal.From(a)` is the SDK client: signals, queries, schedules and
everything else work as documented upstream. `/readyz` follows the
connection to the Temporal frontend.
