# AGENTS.md — this service is built on gox

Read `llm.txt` first. It is the complete reference for the framework this
service uses: the API as Go signatures, every environment variable, the
error envelope, the log keys and the things never to do. Find it with:

```
cat "$(go list -m -f '{{.Dir}}' github.com/guilhermebr/gox)/llm.txt"
```

## Layout

```
cmd/<name>/main.go            gox.MustNew(...) + route registration + a.Run()
internal/<feature>/           handlers (net/http) and, for HTML, views/*.templ
migrations/                   NNNN_name.up.sql / .down.sql (postgres.WithMigrations)
static/                       embedded assets (HTML apps)
.env.example                  every <PREFIX>_* variable
```

## How to

- **Add a route**: `a.HandleFunc("GET /things/{id}", handler)` in `main.go` or a feature's `Register(a *gox.App)`. Read path values with `r.PathValue("id")`, bodies with `gox.Decode`, respond with `gox.JSON`, fail with `gox.Error(w, r, gox.NotFound("thing %s", id))`.
- **Add config**: a field on the service's `Config` struct (which embeds `gox.BaseConfig`) with a `conf` tag; document it in `.env.example`.
- **Add a migration**: a new `migrations/NNNN_name.up.sql` (and `.down.sql`); it runs at boot.
- **Add a page** (HTML apps): a templ component taking typed params, rendered with `web.Render`; forms follow decode → 422 re-render → flash + redirect.
- **Add a background job**: `gox.Periodic("name", interval, fn)` for timers; `gox.Component(c)` for anything with Start/Stop; never a bare goroutine.
- **Add a health check**: implement `Ready(ctx) error` on your component, or `a.Health().AddReadiness("name", fn)`.
- **Call another service**: `a.HTTPClient()` (declare `gox.HTTPClient()`); in an HTML app call this service's own API with `web.APIFrom(r)`.

Recipes with complete code for each of these are in the gox module under
`docs/recipes/`.

## Verify

```
make test lint
```

Both must pass before a change is done. `go run ./cmd/<name> --help` prints
every environment variable the service reads.
