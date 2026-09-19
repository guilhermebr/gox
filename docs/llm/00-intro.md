# gox — build a Go service with one import

gox is an opinionated service framework. It chooses the logger, config loader, HTTP server, middleware, observability, health endpoints and lifecycle so you do not. A plain HTTP service imports only `github.com/guilhermebr/gox`. Heavy integrations are opted in by importing their package and passing `Enable()` to `gox.New`: `gox/postgres`, `gox/jwt`, `gox/openapi`, `gox/web`, and third-party clients under `gox/providers/*` (`gox/providers/supabase`, `gox/providers/workos`). This file is complete: everything public is listed below as Go signatures.

Rules that always hold:
- `main()` is `gox.MustNew(name, options...)`, handler registration, then `a.Run()`. gox owns start order, config, health, shutdown.
- Handlers are plain `net/http`: `func(w http.ResponseWriter, r *http.Request)`, registered with Go 1.22 patterns (`"GET /invoices/{id}"`, `r.PathValue("id")`). `"GET /{$}"` is the exact root; `"GET /"` is a catch-all.
- Config comes from environment variables under one prefix, the uppercased service name: service `billing` reads `BILLING_HTTP_ADDR`, `BILLING_POSTGRES_URL`. `gox.WithConfigPrefix("")` makes them unprefixed. `--help` prints every variable.
- Errors: return `gox.NotFound("invoice %s", id)` style errors and render them with `gox.Error(w, r, err)` (JSON) or `web.Error(w, r, err)` (HTML or JSON). Never write status codes by hand for errors.
- Accessors for undeclared features panic with a message naming the option to add; read the message and add the option.
- Every service gets, without writing code for it: structured logs with request ids, traces and metrics, `/healthz` and `/readyz` on the public port, an admin server on `:9090` with `/metrics`, `/healthz`, `/readyz`, `/version`, `/debug/pprof/`, graceful shutdown on SIGTERM.

Install: `go get github.com/guilhermebr/gox` (root); `go get github.com/guilhermebr/gox/postgres` (and so on) per feature; HTML apps also `go get github.com/a-h/templ` and install the `templ` CLI at the same version. Go 1.26+.
