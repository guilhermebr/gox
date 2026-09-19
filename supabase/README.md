# supabase

Supabase client as a gox feature: `Enable()` reads `<PREFIX>_SUPABASE_URL` and
`<PREFIX>_SUPABASE_KEY`, builds the client, and reports readiness from the
auth service's health endpoint. `From(a)` returns the `*supabase.Client`; use
the SDK directly for auth and data calls.

```go
a := gox.MustNew("shop", gox.HTTP(), supabase.Enable())
client := supabase.From(a)
```

`New(prefix)` and `NewFromConfig(cfg)` build a client outside a gox app
(deprecated in favor of `Enable`).
