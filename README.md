# gox

[![CI](https://github.com/guilhermebr/gox/actions/workflows/ci.yml/badge.svg)](https://github.com/guilhermebr/gox/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/guilhermebr/gox.svg)](https://pkg.go.dev/github.com/guilhermebr/gox)

Go(x) is an opinionated Go service framework. Adopt it and stop deciding which
logger, router, config loader, database driver, or observability stack to
use: every service on gox looks the same, starts the same, shuts down the
same, and shows up the same on dashboards.

It is built for services written *with* AI coding assistants. A small,
symmetrical API, one project layout, self-explaining errors, and a single
machine-readable reference (`llm.txt`) mean a model generates a correct
service on the first try and spends its tokens on your domain, not on
plumbing.

**Go 1.26 or newer.**

## Two constraints that shape everything

1. **The common path is one import.** A plain HTTP service imports only
   `github.com/guilhermebr/gox`. Lifecycle, config, logging, tracing, metrics,
   health, the admin server and the HTTP server live in the root.
2. **Heavy integrations are opt-in by import.** Postgres, background jobs,
   auth, server-rendered HTML and third-party clients live in their own
   modules: importing one and passing its `Enable()` option to `gox.New` is
   the opt-in. A service that does not import `gox/postgres` never compiles
   pgx. CI proves it on every build.

## A JSON API, one import

```go
package main

import (
	"net/http"
	"os"

	"github.com/guilhermebr/gox"
)

func main() {
	a := gox.MustNew("hello", gox.HTTP())
	a.HandleFunc("GET /hello/{name}", func(w http.ResponseWriter, r *http.Request) {
		_ = gox.JSON(w, http.StatusOK, map[string]string{"hello": r.PathValue("name")})
	})
	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

Run it and you have structured logs (JSON in production) with request ids,
Prometheus metrics on `:9090/metrics`, OpenTelemetry traces once export is on
(production, or an OTLP endpoint), `/healthz` and `/readyz` on both ports,
pprof, a per-request timeout and body limit, one error envelope for
everything, and a graceful drain on SIGTERM. You wrote none of that.

## A service with Postgres, two gox imports

```go
package main

import (
	"embed"
	"errors"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5"

	"github.com/guilhermebr/gox"
	"github.com/guilhermebr/gox/postgres"
)

//go:embed migrations/*.sql
var migrations embed.FS

func main() {
	a := gox.MustNew("billing",
		gox.HTTP(),
		postgres.Enable(postgres.WithMigrations(migrations)),
	)
	db := postgres.From(a)

	a.HandleFunc("GET /invoices/{id}", func(w http.ResponseWriter, r *http.Request) {
		var id string
		err := db.QueryRow(r.Context(), "SELECT id FROM invoices WHERE id = $1", r.PathValue("id")).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			gox.Error(w, r, gox.NotFound("invoice %s", r.PathValue("id")))
			return
		}
		if err != nil {
			gox.Error(w, r, err)
			return
		}
		_ = gox.JSON(w, http.StatusOK, map[string]string{"id": id})
	})

	if err := a.Run(); err != nil {
		os.Exit(1)
	}
}
```

With `BILLING_POSTGRES_URL` set: the pool pings at boot (a lazy pool never
reports healthy), the migration runs on a dedicated connection before
`/readyz` turns green, queries are traced once OTEL export is on, pool stats are exported, and the
pool closes last on shutdown. `examples/postgres` is the complete version,
with a POST that maps a duplicate id to 409.

A server-rendered HTML app on templ adds `gox/web` the same way; see
`examples/web`.

## The opinions

gox chooses once so you do not. Most choices have an ADR in `docs/decisions/`;
its `README.md` indexes them.

| gox chooses | because |
|---|---|
| `net/http` and the Go 1.22 `ServeMux` | models and people already know it; method and wildcard routing cover what routers were used for (ADR 0005) |
| `log/slog`, JSON in production, text in development | stdlib, structured, with request and trace ids stamped from the context |
| `ardanlabs/conf`, one env prefix per service, `--help` lists everything | one config pass, one source of truth, discoverable from the binary (ADR 0003) |
| OpenTelemetry traces and metrics, Prometheus on the admin port | every service on the same dashboards with the same metric names |
| coded errors with a public-safe message and one JSON envelope | clients see one shape from handlers and the framework alike; causes go to logs, never to clients (ADR 0004) |
| an explicit staged lifecycle, no DI container | start order is fixed by stage, readable, and shutdown is the exact reverse (ADR 0002) |
| pgx plus golang-migrate on a dedicated connection | what every consumer already used, minus the incident that starved a pool under a rolling deploy (ADR 0006) |
| templ, Alpine.js, HTMX 2, embedded hashed assets | what the existing HTML apps converged on; no Node at build or run time (ADR 0007) |
| nested modules, one per feature | a consumer's module graph contains only what it imports (ADR 0000) |

## Escape hatches

- `gox.Component(c)` and `a.Add(c)` accept anything with `Name`, `Start` and
  `Stop`; implement `Run` for a blocking loop, `Ready` for readiness.
- `gox.WithMiddleware` appends to the chain; `gox.WithAuth` fills the auth
  slot; `gox.WithErrorMapper` translates your own sentinels.
- `a.Mux()` is the plain `*http.ServeMux`.
- `pkg/*` is importable: services use `pkg/storage`, `pkg/mail`, `pkg/i18n`,
  `pkg/middleware` and `pkg/httpclient` directly and rarely need the rest.

## Writing a feature package

A feature is opted in by passing its `Enable` option to `gox.New`; most also
have a `Config` section and a `From(a)` accessor. Building blocks (`postgres`,
`web`, ...) live at the top level and third-party clients under `providers/`
(`providers/README.md` lists them), one module each. `docs/features.md` walks
through one; adding one follows `.claude/skills/gox-add-module/SKILL.md`.

## Start a service

```
go install github.com/guilhermebr/gox/cmd/gox@latest
gox new billing --module github.com/acme/billing --postgres
cd billing && make run
```

`gox new` renders `cmd/<name>/main.go`, an example feature with a test,
`.env.example`, a Makefile, a distroless Dockerfile, a lint config, a CI
workflow, `AGENTS.md`, `CLAUDE.md` and agent skills under `.claude/skills/`.
`--postgres` adds a `migrations` package and `postgres.Enable`; `--web` adds
a templ layout, a home page and embedded static assets. On every commit a
test in this repository renders the plain, web and postgres+web services
against the working tree and checks that they build and pass their tests, and
that plain and web start and answer `/healthz` (postgres+web too with
`DATABASE_URL`).

## Documentation

- `llm.txt`: a single-file export for models, generated from source.
- `docs/guide.md`: build a service end to end.
- `docs/features.md`: write your own `Enable()`/`From()` package.
- `docs/recipes/`: one complete, build-checked example per task.
- `AGENTS.md`: for agents working in this repository. Generated services
  get their own `AGENTS.md` and `CLAUDE.md` from the scaffold template.
- `examples/`: `minimal`, `http`, `postgres`, `web`.

## Versioning

Semantic versioning per module: `v0.x.y` for the root, `postgres/v0.x.y`
for a feature. A renamed or removed public API keeps its old name as a
`// Deprecated:` wrapper for one minor version (`AGENTS.md`).

## Utilities

`monetary` (exact money arithmetic) and `osrelease` (Linux distribution
detection) are plain libraries with no framework dependency.

## Contributing

See `CONTRIBUTING.md` and `AGENTS.md`. `make ci` is the bar.

## License

MIT. See `LICENSE`.
