# Conventions and contracts

Error envelope (every error response, from handlers and from the framework alike):

```json
{"code": "not_found", "message": "invoice inv_1", "request_id": "8e03…", "details": {"field": "amount"}}
```

`gox.Envelope` fields: `Code string`, `Message string`, `RequestID string`, `Details map[string]any`. The constructors return `*errors.Error` (from `gox/pkg/errors`, no import needed) with these methods: `WithDetail(key string, value any) *Error` (adds to `details`), `WithHTTPStatus(status int) *Error`, `Code() Code`, `Message() string`, `Details() map[string]any`, `Error() string`, `Unwrap() error`. Check codes with `gox.CodeOf(err) == gox.CodeNotFound`, never by comparing error text.

Codes and HTTP statuses: `invalid_argument` 400, `unauthenticated` 401, `permission_denied` 403, `not_found` 404, `already_exists` 409, `aborted` 409, `failed_precondition` 400, `resource_exhausted` 429, `unimplemented` 501 (405 for a wrong method), `internal` 500, `unavailable` 503, `deadline_exceeded` 504, `canceled` 499. Errors that are not gox errors render as `internal` with the message "internal error"; their text never reaches a client. Map your own sentinels once with `gox.WithErrorMapper`.

Log keys (contract with dashboards): `request_id`, `trace_id`, `span_id`, `error`, `component`, `duration`. Access log fields: `method`, `path`, `route` (the mux pattern), `client_ip`, `status`, `bytes`. Level by status: 5xx error, 4xx warn, else info. `/healthz` and `/readyz` are not logged.

Metrics (Prometheus on the admin `/metrics`): `http_server_request_duration_seconds` and `http_server_active_requests` by `http_request_method`, `http_route`, `http_response_status_code`; `db_client_connection_count{state}` and `db_client_connection_max` with `postgres.Enable()`; Go runtime and process metrics.

Middleware chain (fixed): error renderers → route capture → recovery → request id → client ip → tracing → metrics → logging → timeout (`HTTP_REQUEST_TIMEOUT`) → max bytes (`HTTP_MAX_BODY_BYTES`) → security headers → CORS (only `gox.WithCORS`) → cross-origin protection → feature middleware → auth (`gox.WithAuth`) → `gox.WithMiddleware` → mux.

Forms (gox/web): `GET` renders the form; `POST` decodes with `web.Form[T]`, re-renders the same component with status 422 on `FieldErrors`, and on success calls `web.AddFlash` then `web.Redirect` (303, or `HX-Redirect` for htmx). `FieldErrors` is keyed by the `form` tag name; messages are `required`, `must be at least N characters`, `must be at most N characters`, `must be at least N`, `must be at most N`, `must be a whole number`, `must be a number`, `must be a duration such as 30s or 5m`. CSRF is automatic with sessions and applies to form submissions (`application/x-www-form-urlencoded`, `multipart/form-data`, or no Content-Type): include `@web.CSRFField(page)` (hidden field `_csrf`) in forms and `@web.CSRFMeta(page)` in the layout head (`X-CSRF-Token` header for htmx and fetch); a missing or wrong token is a 403. JSON requests (`Content-Type: application/json`) are never checked, so a JSON API and HTML pages coexist in one service and curl works; `web.WithCSRFExempt("/webhooks/")` skips prefixes that third parties call. Rejections by any middleware log the real route.

Cross-origin protection (always on): a state-changing request (`POST`, `PUT`, `PATCH`, `DELETE`) that a browser sends from another origin is a 403 `permission_denied`, decided from `Sec-Fetch-Site` and `Origin` with no token. Requests without those headers (curl, webhooks, server-to-server) and safe methods pass; origins in `gox.WithCORS` are trusted. A single-page app on the same origin needs nothing else.

Behind a proxy: set `<PREFIX>_HTTP_TRUSTED_PROXIES` to the proxies' CIDRs and read the caller with `gox.ClientIP(r)`; `X-Forwarded-For` is believed only from those addresses and never from the client's own entries. `gox.RateLimit(10, time.Minute)` wraps single routes (login, signup) and answers 429 with `Retry-After`, per client IP and per process. Lists paginate by keyset: `gox.ParsePage(r, 50, 100)` reads `?after=&limit=`, `page.Cursor(&key)` decodes the cursor and `gox.EncodeCursor(lastKey)` makes the next one. A platform that injects another variable name: `gox.WithEnvAlias("POSTGRES_URL", "DATABASE_URL")`; the service's own variable wins. Values that triggers or row-level security read with `current_setting()` go through `postgres.TxWith(ctx, pool, map[string]string{"app.actor": id}, fn)`.

Other error shapes: `gox.WithErrorRenderer(gox.ProblemJSON)` replaces the envelope with RFC 9457 `application/problem+json` for every error, framework rejections included (`title`, `status`, `detail`, `code`, `request_id`, and each `WithDetail` entry as a top-level member). Pass your own `gox.ErrorRenderer` to pick a shape by path; return false to fall back to the envelope.

Single-page apps: `gox.HTTP(gox.WithSPA(gox.SPAConfig{FS: dist.FS, ServerPrefixes: []string{"/api/"}}))` serves the built app from the API's origin: files at their paths (`/assets/` cached forever), `index.html` for every other `GET` so client routes deep-link, the error envelope for unmatched requests under the server prefixes. `SPAConfig.Head` injects per-request tags before `</head>`. Registered routes always win.

HTML errors (gox/web): `web.Enable` registers an error renderer, so `web.Error`, panics, timeouts, 404/405 and CSRF failures render the error page for requests that want HTML (htmx requests, or `Accept` preferring `text/html`) and the JSON envelope for everything else, with the same status either way. The `ErrorPage` function returns a body fragment; gox wraps it in the layout and `page` is never nil. Set `web.PageFrom(r).Title` before `web.Render`, in Go: templ components render lazily and the layout prints the title before the body.

Never:
- Never add a router (chi, gin, echo), an ORM, or a DI framework. `net/http` mux, pgx, explicit options.
- Never call `http.Error` or write error JSON by hand; use `gox.Error` / `web.Error`.
- Never read env vars directly; add fields to your config struct (`gox.WithConfig`).
- Never start goroutines that outlive a request outside a component or `gox.Periodic`; they would not be stopped on shutdown.
- Never mutate the shared HTTP client; derive one per request (`web.APIFrom`, `httpclient.WithBearer`).
- Never import `github.com/guilhermebr/gox/pkg/...` from a service unless you are writing a feature package; the root re-exports what services need. The exceptions are the provider-neutral APIs: `pkg/storage` (`Bucket`, option types, upload tokens) and `pkg/mail` (`Message`, `Sender`, `SMTP`, `Restrict`, `Recorder`), which every storage or mail provider shares, and `pkg/i18n` (server-side translations).
- Never put datastore settings in `BaseConfig`; they are feature sections (`<PREFIX>_POSTGRES_URL`).
