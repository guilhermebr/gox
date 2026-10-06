# Recipes

One task per file, as complete code that `make check-recipes` compiles in the
gox repository. Read only the recipe for your task.

- Recipes keep the code in one `main.go` for brevity. In a service made by
  `gox new`, add the options to the `gox.MustNew` call in `cmd/<name>/main.go`
  and put handlers in `internal/<feature>/` with `Register(a, deps...)`, like
  `internal/hello`, so tests can pass fakes.
- Rename as you copy: `example.com/shop` is your module path; `billing` and
  `shop` are the service name, so `BILLING_*` and `SHOP_*` take your prefix.
- `[jobs]` after a file means `go get github.com/guilhermebr/gox/jobs` first,
  and `go mod tidy` after adding its imports. No brackets: the root module has
  everything, including the `pkg/...` packages recipes import.

## HTTP and errors

- JSON endpoint: path values, decode a body, answer 404 or 400 → add-http-route.md
- Test a handler without opening a port → write-handler-test.md
- RFC 9457 problem+json errors instead of the default envelope → problem-json-errors.md
- Reject requests an OpenAPI document does not allow → validate-requests-with-openapi.md [openapi]
- Contract first: OpenAPI document, generated types, handlers → start-from-a-contract.md [openapi]
- Serve a built single-page app from the API's origin → serve-spa.md
- Translate error messages and other server strings per request locale → translate-server-strings.md

## Data (Postgres)

- Create or change tables: SQL migrations run at boot → add-migration.md [postgres]
- Query, map no rows to 404, run a transaction → add-postgres-query.md [postgres]
- Paginate a list with a cursor (keyset, not offset) → paginate-a-list.md [postgres]

## Pages (server-rendered HTML)

- A templ page with the layout and static assets → add-html-page.md [web]
- A form: validate, re-render with field errors (422), flash and redirect → add-form-with-validation.md [web]
- Update part of a page with htmx → add-htmx-partial.md [web]
- Pages that call this service's JSON API as the signed-in user → call-backend-api.md [web]

## Background work

- Run a function every N minutes in every replica; the interval from config → add-periodic-job.md
- Durable jobs on Postgres (River): enqueued in the request's transaction, retried, cron schedules that fire once per fleet → add-background-job.md [jobs, postgres]
- A long-running worker or listener with Start and Stop → add-custom-component.md
- Multi-step workflows that wait for days or for a signal (Temporal) → add-temporal-workflow.md [providers/temporal]
- API and workers as separate binaries, or one binary that picks its role, from one wiring → two-binaries-one-wiring.md [postgres]

## Auth

- JWT bearer tokens on every route or some; issuing tokens; API keys → add-auth.md [jwt]
- Accept an identity provider's tokens (Auth0, Okta, Keycloak, Cognito, Entra) through JWKS → verify-identity-provider-tokens.md [jwt]
- Sign-in with WorkOS AuthKit: login, sessions, organizations, identity webhooks → add-workos-login.md [providers/workos]

## Integrations

- Call a third-party HTTP API with timeouts, tracing and opt-in retries → call-external-api.md
- Stripe: call the API and verify its webhooks → receive-stripe-webhooks.md [providers/stripe]
- Send email (Mailgun; SMTP locally; an allow-list on staging), delivery events, inbound mail → send-email.md [providers/mailgun]
- File uploads straight to S3, MinIO, R2 or Google Cloud Storage with presigned URLs → add-file-uploads.md [providers/s3]
- Product analytics events and feature flags (PostHog) → track-events-with-posthog.md [providers/posthog]

## Ops

- Readiness and liveness checks; a platform's own probe path → add-health-check.md
- Rate-limit a route (login, signup) and log the real client IP behind a proxy → protect-a-login-route.md

No recipe yet: CORS (`go doc github.com/guilhermebr/gox.WithCORS`) and
Supabase (`go get` it, then `go doc github.com/guilhermebr/gox/providers/supabase`).
