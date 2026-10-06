// Command postgres is a JSON API backed by Postgres with one migration. Two
// gox imports plus pgx.
//
// Readiness is green only after the pool pings and the migration has run,
// queries are traced once OTEL export is on, pool stats are exported on
// :9090/metrics, and shutdown closes the pool last.
//
//	BILLING_POSTGRES_URL=postgres://u:p@localhost:5432/db go run ./examples/postgres
//	curl -i -X POST localhost:8080/invoices -d '{"id":"inv_1","customer":"ana","amount":1250}'
//	curl -i localhost:8080/invoices/inv_1
//	curl localhost:9090/readyz
package main

import (
	"errors"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"

	"github.com/guilhermebr/gox/examples/postgres/migrations"
)

type invoice struct {
	ID       string `json:"id"`
	Customer string `json:"customer"`
	Amount   int    `json:"amount"`
}

func main() {
	a := gox.MustNew("billing",
		gox.HTTP(),
		postgres.Enable(postgres.WithMigrations(migrations.FS)),
	)
	db := postgres.From(a)

	a.HandleFunc("GET /invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		var inv invoice
		err := db.QueryRow(r.Context(), "SELECT id, customer, amount FROM invoices WHERE id = $1", r.PathValue("id")).
			Scan(&inv.ID, &inv.Customer, &inv.Amount)
		if errors.Is(err, pgx.ErrNoRows) {
			gox.Error(w, r, gox.NotFound("invoice %s", r.PathValue("id")))
			return
		}
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		_ = gox.JSON(w, http.StatusOK, inv)
	})

	a.HandleFunc("POST /invoices", func(w http.ResponseWriter, r *http.Request) {
		var inv invoice
		if err := gox.Decode(r, &inv); err != nil {
			gox.Error(w, r, err)
			return
		}
		if inv.Amount <= 0 {
			gox.Error(w, r, gox.InvalidArgument("amount must be positive").WithDetail("field", "amount"))
			return
		}
		err := postgres.Tx(r.Context(), db, func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(),
				"INSERT INTO invoices (id, customer, amount) VALUES ($1, $2, $3)", inv.ID, inv.Customer, inv.Amount)
			return err
		})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			gox.Error(w, r, gox.AlreadyExists("invoice %s", inv.ID))
			return
		}
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		_ = gox.JSON(w, http.StatusCreated, inv)
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
