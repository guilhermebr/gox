# Testing

Handler tests build the app without ports and serve its mux:

```go
func newApp(t *testing.T) *gox.App {
	t.Helper()
	a, err := gox.New("billing",
		gox.WithoutAdminServer(),                                      // no :9090
		gox.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))), // silent
		gox.HTTP(),
	)
	if err != nil {
		t.Fatal(err)
	}
	invoices.Register(a, deps...)
	return a
}

func TestGetInvoice(t *testing.T) {
	rec := httptest.NewRecorder()
	newApp(t).Mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/invoices/nope", nil))
	// rec.Code == 404; body is the envelope {"code":"not_found",...}
}
```

Facts to rely on:
- `gox.New` reads the real environment; set variables with `t.Setenv("BILLING_X", ...)`. Only `HTTP()` needs nothing.
- `a.Mux()` is the bare mux: handler code, `gox.Error` and `gox.JSON` work; the middleware chain (request ids, timeout, envelope 404 for unmatched routes) is not applied. To test through the chain, run `a.RunContext(ctx)` in a goroutine on a free port (`t.Setenv("BILLING_HTTP_ADDR", "127.0.0.1:0")` is not enough; pick a free port first), wait for `a.Health().IsReady()`, and use `http.Get`.
- Postgres-backed handlers: pass dependencies through `Register(a, db)` (the feature's `Register(a *gox.App, deps...)` takes what its handlers need) so tests call `Register(a, nil)` for paths that never reach the database, and keep database tests behind `//go:build integration` with `DATABASE_URL`. `postgres.Enable()` in a test app requires `<PREFIX>_POSTGRES_URL` and pings at `Run`, not at `New`; `postgres.From(a)` is valid right after `New`.
- After importing a module directly for the first time (`github.com/jackc/pgx/v5` for `pgx.ErrNoRows`), run `go mod tidy` and commit `go.mod` and `go.sum`.
- gox/web pages: serve the mux the same way; `web.Render` needs the web middleware, so test HTML handlers through the chain (free port) or test the templ components directly with `component.Render(ctx, &buf)`.
