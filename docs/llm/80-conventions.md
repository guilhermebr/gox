# Conventions and contracts

Error envelope (every error response, from handlers and from the framework alike):

```json
{"code": "not_found", "message": "invoice inv_1", "request_id": "8e03…", "details": {"field": "amount"}}
```

`gox.Envelope` fields: `Code string`, `Message string`, `RequestID string`, `Details map[string]any`. The constructors return `*errors.Error` (from `gox/pkg/errors`, no import needed) with these methods: `WithDetail(key string, value any) *Error` (adds to `details`), `WithHTTPStatus(status int) *Error`, `Code() Code`, `Message() string`, `Details() map[string]any`, `Error() string`, `Unwrap() error`. Check codes with `gox.CodeOf(err) == gox.CodeNotFound`, never by comparing error text.

Codes and HTTP statuses: `invalid_argument` 400, `unauthenticated` 401, `permission_denied` 403, `not_found` 404, `already_exists` 409, `aborted` 409, `failed_precondition` 400, `resource_exhausted` 429, `unimplemented` 501 (405 for a wrong method), `internal` 500, `unavailable` 503, `deadline_exceeded` 504, `canceled` 499. Errors that are not gox errors render as `internal` with the message "internal error"; their text never reaches a client. Map your own sentinels once with `gox.WithErrorMapper`.

Log keys (contract with dashboards): `request_id`, `trace_id`, `span_id`, `error`, `component`, `duration`. Access log fields: `method`, `path`, `route` (the mux pattern), `status`, `bytes`. Level by status: 5xx error, 4xx warn, else info. `/healthz` and `/readyz` are not logged.

Metrics (Prometheus on the admin `/metrics`): `http_server_request_duration_seconds` and `http_server_active_requests` by `http_request_method`, `http_route`, `http_response_status_code`; `db_client_connection_count{state}` and `db_client_connection_max` with `postgres.Enable()`; Go runtime and process metrics.

Middleware chain (fixed): error renderers → route capture → recovery → request id → tracing → metrics → logging → timeout (`HTTP_REQUEST_TIMEOUT`) → max bytes (`HTTP_MAX_BODY_BYTES`) → security headers → CORS (only `gox.WithCORS`) → feature middleware → auth (`gox.WithAuth`) → `gox.WithMiddleware` → mux.

Forms (gox/web): `GET` renders the form; `POST` decodes with `web.Form[T]`, re-renders the same component with status 422 on `FieldErrors`, and on success calls `web.AddFlash` then `web.Redirect` (303, or `HX-Redirect` for htmx). `FieldErrors` is keyed by the `form` tag name; messages are `required`, `must be at least N characters`, `must be at most N characters`, `must be at least N`, `must be at most N`, `must be a whole number`, `must be a number`, `must be a duration such as 30s or 5m`. CSRF is automatic with sessions: include `@web.CSRFField(page)` (hidden field `_csrf`) in forms and `@web.CSRFMeta(page)` in the layout head (`X-CSRF-Token` header for htmx and fetch); a missing or wrong token is a 403.

HTML errors (gox/web): `web.Enable` registers an error renderer, so `web.Error`, panics, timeouts, 404/405 and CSRF failures render the error page for requests that want HTML (htmx requests, or `Accept` preferring `text/html`) and the JSON envelope for everything else, with the same status either way. The `ErrorPage` function returns a body fragment; gox wraps it in the layout and `page` is never nil. Set `web.PageFrom(r).Title` before `web.Render`, in Go: templ components render lazily and the layout prints the title before the body.

Never:
- Never add a router (chi, gin, echo), an ORM, or a DI framework. `net/http` mux, pgx, explicit options.
- Never call `http.Error` or write error JSON by hand; use `gox.Error` / `web.Error`.
- Never read env vars directly; add fields to your config struct (`gox.WithConfig`).
- Never start goroutines that outlive a request outside a component or `gox.Periodic`; they would not be stopped on shutdown.
- Never mutate the shared HTTP client; derive one per request (`web.APIFrom`, `httpclient.WithBearer`).
- Never import `github.com/guilhermebr/gox/pkg/...` from a service unless you are writing a feature package; the root re-exports what services need.
- Never put datastore settings in `BaseConfig`; they are feature sections (`<PREFIX>_POSTGRES_URL`).
