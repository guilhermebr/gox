// Command postgres is the plan's second canonical service: a JSON API
// backed by Postgres with one migration. Two imports.
//
// Readiness is green only after the pool pings and the migration has run,
// pool stats and query spans reach the admin server, and shutdown closes
// the pool last.
//
//	POSTGRES_EXAMPLE_POSTGRES_URL=postgres://u:p@localhost:5432/db go run ./examples/postgres
//	curl -i -X POST localhost:8080/invoices -d '{"id":"inv_1","customer":"ana","amount":1250}'
//	curl -i localhost:8080/invoices/inv_1
//	curl localhost:9090/readyz
package main

import (
	"embed"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Config adds the service's own settings next to the framework's.
type Config struct {
	gox.BaseConfig
	InvoiceTTL time.Duration `conf:"default:24h"`
}

type invoice struct {
	ID       string `json:"id"`
	Customer string `json:"customer"`
	Amount   int    `json:"amount"`
}

func main() {
	var cfg Config
	a := gox.MustNew("postgres-example",
		gox.WithConfig(&cfg),
		gox.HTTP(),
		postgres.Enable(postgres.WithMigrations(migrations)),
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
		err := postgres.Tx(r.Context(), db, func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(),
				"INSERT INTO invoices (id, customer, amount) VALUES ($1, $2, $3)", inv.ID, inv.Customer, inv.Amount)
			return err
		})
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
