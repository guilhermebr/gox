# Build a service on gox

This guide takes a JSON API from an empty directory to a running, observable
service with a database. Every step is a complete file you can copy. The
same steps produce an HTML app; the differences are called out at the end.

## 1. Layout

```
billing/
├── cmd/billing/main.go
├── internal/invoices/handler.go
├── migrations/000001_create_invoices.up.sql
├── migrations/000001_create_invoices.down.sql
├── .env.example
├── Makefile
└── go.mod
```

```
go mod init example.com/billing
go get github.com/guilhermebr/gox github.com/guilhermebr/gox/postgres
```

## 2. main.go

```go
package main

import (
	"embed"
	"os"
	"time"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"

	"example.com/billing/internal/invoices"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Config holds the service's own settings next to the framework's.
type Config struct {
	gox.BaseConfig
	InvoiceTTL time.Duration `conf:"default:24h,help:how long an open invoice may stay unpaid"`
}

func main() {
	var cfg Config
	a := gox.MustNew("billing",
		gox.WithConfig(&cfg),
		gox.WithVersion(version),
		gox.HTTP(),
		postgres.Enable(postgres.WithMigrations(migrations)),
	)
	invoices.Register(a, cfg.InvoiceTTL)
	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}

var version = "dev" // set with -ldflags "-X main.version=$(git rev-parse --short HEAD)"
```

What `MustNew` did: applied the options, loaded `BILLING_*` in one pass
(the framework's variables, your `Config`, and `BILLING_POSTGRES_*`),
validated everything and exited with a message naming the variable if
something was missing, built the logger, set up telemetry, built the
Postgres pool without dialing, built the HTTP server and mux. Nothing has
started yet, so handlers can be registered.

What `Run` will do: start the pool (ping, then migrate on a dedicated
connection), then the HTTP server, then the admin server; mark `/readyz`
green; block until SIGTERM or a fatal component error; go not-ready; stop
everything in reverse with a fresh timeout per component; flush telemetry.

## 3. Handlers

```go
package invoices

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"
)

type Invoice struct {
	ID       string `json:"id"`
	Customer string `json:"customer"`
	Amount   int    `json:"amount"`
}

type handler struct {
	db  *pgxpool.Pool
	ttl time.Duration
}

// Register wires the routes. Handlers are plain net/http.
func Register(a *gox.App, ttl time.Duration) {
	h := &handler{db: postgres.From(a), ttl: ttl}
	a.HandleFunc("GET /invoices/{id}", h.get)
	a.HandleFunc("POST /invoices", h.create)
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	var inv Invoice
	err := h.db.QueryRow(r.Context(),
		"SELECT id, customer, amount FROM invoices WHERE id = $1", r.PathValue("id")).
		Scan(&inv.ID, &inv.Customer, &inv.Amount)
	if errors.Is(err, pgx.ErrNoRows) {
		gox.Error(w, r, gox.NotFound("invoice %s", r.PathValue("id")))
		return
	}
	if err != nil {
		gox.Error(w, r, err) // 500 with a generic message; the cause is logged with the request id
		return
	}
	_ = gox.JSON(w, http.StatusOK, inv)
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	var in Invoice
	if err := gox.Decode(r, &in); err != nil { // strict JSON; 400 or 413
		gox.Error(w, r, err)
		return
	}
	if in.Amount <= 0 {
		gox.Error(w, r, gox.InvalidArgument("amount must be positive").WithDetail("field", "amount"))
		return
	}
	err := postgres.Tx(r.Context(), h.db, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(),
			"INSERT INTO invoices (id, customer, amount) VALUES ($1, $2, $3)", in.ID, in.Customer, in.Amount)
		return err
	})
	if err != nil {
		gox.Error(w, r, err)
		return
	}
	_ = gox.JSON(w, http.StatusCreated, in)
}
```

Three rules:

- Read path values with `r.PathValue`, bodies with `gox.Decode`, respond
  with `gox.JSON`.
- Fail with a coded error and `gox.Error`. Never write a status code for an
  error by hand. Unknown errors render as `internal` with the text hidden.
- Keep your own sentinels if you have them and map them once:
  `gox.WithErrorMapper(func(err error) error { if errors.Is(err, ErrNoRows) { return gox.WrapError(err, gox.CodeNotFound, "not found") }; return err })`.

## 4. Configuration

Every variable is under one prefix, the uppercased service name. Run the
binary with `--help` to see all of them with types, defaults and which
option declared them:

```
BILLING_HTTP_ADDR              (string)   listen address; PORT is honored when unset  [default :8080]
BILLING_LOG_LEVEL              (string)   debug | info | warn | error                  [default info]
BILLING_INVOICE_TTL            (duration) how long an open invoice may stay unpaid     [default 24h]
# Section POSTGRES (declared by postgres.Enable())
BILLING_POSTGRES_URL           (string)   required
BILLING_POSTGRES_MIGRATE       (bool)     run embedded migrations at boot              [default true]
```

Secrets carry `mask` and never appear in logs. Set `BILLING_LOG_LEVEL=debug`
to log the effective configuration at startup with secrets masked.
`gox.WithConfigPrefix("")` switches to unprefixed variables for services that
already deploy that way.

## 5. Run it

```
BILLING_POSTGRES_URL=postgres://u:p@localhost:5432/billing go run ./cmd/billing
curl -i localhost:8080/invoices/inv_1        # 404 {"code":"not_found",...}
curl -s localhost:9090/readyz                # {"status":"ok","checks":{"postgres":{"status":"ok"}}}
curl -s localhost:9090/metrics | grep http_server_request_duration
curl -s localhost:9090/version
```

Every response carries `X-Request-ID`; send your own to correlate. Every
log line from a request carries `request_id`, and `trace_id` when a trace
is active.

## 6. Observability

Traces and metrics are on locally without export. Set
`BILLING_OTEL_ENDPOINT=collector:4317` (or `BILLING_OTEL_ENABLED=true` in
production, where it is the default) to export over OTLP. Prometheus
scrapes `:9090/metrics` regardless. Outbound calls made with
`a.HTTPClient()` propagate trace context and the request id.

## 7. Background work

- `gox.Periodic("expire-trials", 15*time.Minute, fn)` for timers.
- `gox.Component(c)` or `a.Add(c)` for anything with `Start`/`Stop`;
  implement `Run(ctx) error` for a blocking loop that gox supervises, and
  `Ready(ctx) error` to be part of `/readyz`.

Never start a bare goroutine that outlives a request: it would not be
stopped on shutdown.

## 8. Tests

Handlers are tested through the mux (`docs/recipes/write-handler-test.md`).
Integration tests run against `DATABASE_URL` behind the `integration` build
tag; `postgres.Migrate` bootstraps the schema in `TestMain`.

## 9. The HTML variant

Add `gox/web` and templ:

```go
a := gox.MustNew("shop",
	gox.HTTP(),
	web.Enable(web.WithStatic(assets), web.WithSessions(), web.WithLayout(views.Layout)),
)
```

Pages are templ components with typed parameters rendered with
`web.Render`; the layout receives `*web.Page` (title, user, flashes, CSRF
token, `Asset(name)`). Forms follow one pattern: `web.Form[T]` to decode
and validate, `web.RenderStatus(..., 422, ...)` to re-render with field
errors, `web.AddFlash` + `web.Redirect` on success. Sessions are sealed
cookies with CSRF on; `web.APIFrom(r)` calls the service's own API as the
signed-in user. `examples/web` is the complete version of this section.
