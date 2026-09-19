# Add a background job (Postgres queue, retries, schedules)

```go path=main.go
package main

import (
	"context"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/jobs"
	"github.com/guilhermebr/gox/postgres"
)

// SendReceipt is the job: its fields are the arguments, stored as JSON.
type SendReceipt struct {
	InvoiceID string `json:"invoice_id"`
}

// Kind names the job in the queue; never change it once jobs exist.
func (SendReceipt) Kind() string { return "send_receipt" }

// receipts works SendReceipt jobs. A returned error retries with backoff
// (25 attempts by default); the context is cancelled on shutdown.
type receipts struct {
	river.WorkerDefaults[SendReceipt]
	a *gox.App
}

func (w *receipts) Work(ctx context.Context, job *river.Job[SendReceipt]) error {
	w.a.Log().InfoContext(ctx, "sending receipt", "invoice", job.Args.InvoiceID, "attempt", job.Attempt)
	return nil
}

// PurgeExports runs on a schedule.
type PurgeExports struct{}

func (PurgeExports) Kind() string { return "purge_exports" }

type purger struct {
	river.WorkerDefaults[PurgeExports]
}

func (purger) Work(context.Context, *river.Job[PurgeExports]) error { return nil }

func main() {
	// SHOP_POSTGRES_URL. River's tables are created at boot (SHOP_JOBS_MIGRATE).
	// An API next to a separate worker process sets SHOP_JOBS_WORK=false.
	a := gox.MustNew("shop", gox.HTTP(), postgres.Enable(), jobs.Enable(postgres.From))

	jobs.Register(a, &receipts{a: a})
	jobs.Register(a, purger{})
	// Cron, optionally with a zone ("CRON_TZ=America/New_York 0 0 * * *"), or "@every 5m".
	if err := jobs.Schedule(a, "0 3 * * *", PurgeExports{}); err != nil {
		os.Exit(1)
	}

	a.HandleFunc("POST /invoices/{id}/receipt", func(w http.ResponseWriter, r *http.Request) {
		// InsertTx: the job exists only if the change that needs it commits.
		err := pgx.BeginFunc(r.Context(), postgres.From(a), func(tx pgx.Tx) error {
			if _, err := tx.Exec(r.Context(), "UPDATE invoices SET receipt_requested = true WHERE id = $1", r.PathValue("id")); err != nil {
				return err
			}
			_, err := jobs.From(a).InsertTx(r.Context(), tx, SendReceipt{InvoiceID: r.PathValue("id")}, nil)
			return err
		})
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

`jobs.From(a)` is the River client; everything River offers (unique jobs,
priorities, per-job timeouts, the River UI) works as documented upstream.
