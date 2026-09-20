# Add a Postgres query (and a transaction)

```go path=main.go
package main

import (
	"errors"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"
)

type invoice struct {
	ID     string `json:"id"`
	Amount int    `json:"amount"`
}

func main() {
	a := gox.MustNew("billing", gox.HTTP(), postgres.Enable())
	db := postgres.From(a) // *pgxpool.Pool; BILLING_POSTGRES_URL is required

	a.HandleFunc("GET /invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		var inv invoice
		err := db.QueryRow(r.Context(), "SELECT id, amount FROM invoices WHERE id = $1", r.PathValue("id")).
			Scan(&inv.ID, &inv.Amount)
		if errors.Is(err, pgx.ErrNoRows) {
			gox.Error(w, r, gox.NotFound("invoice %s", r.PathValue("id")))
			return
		}
		if err != nil {
			gox.Error(w, r, err) // 500 "internal error"; the cause goes to the log
			return
		}
		_ = gox.JSON(w, http.StatusOK, inv)
	})

	a.HandleFunc("POST /invoices", func(w http.ResponseWriter, r *http.Request) {
		var in invoice
		if err := gox.Decode(r, &in); err != nil {
			gox.Error(w, r, err)
			return
		}
		err := postgres.Tx(r.Context(), db, func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(), "INSERT INTO invoices (id, amount) VALUES ($1, $2)", in.ID, in.Amount)
			return err // non-nil rolls back
		})
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		_ = gox.JSON(w, http.StatusCreated, in)
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

Audit triggers and row-level security read the acting user or tenant with
`current_setting('app.actor')`. Set them for one transaction, on its own
connection, with `postgres.TxWith(ctx, db, map[string]string{"app.actor": userID}, func(tx pgx.Tx) error { ... })`;
they are gone when it ends, so nothing leaks through the pool.
