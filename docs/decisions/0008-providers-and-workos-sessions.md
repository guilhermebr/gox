# 0008 — Providers, and WorkOS sessions

Date: 2026-09-19
Status: accepted

## Context

Feature packages are the building blocks a service is made of (`postgres`,
`jwt`, `web`). Clients for third-party services are a different, open-ended
family: identity providers, mail, storage, payments, analytics. They need a
home that scales to many modules without crowding the top level, and the
first one with real behaviour beyond "build a client" is WorkOS.

## Decision

- Third-party clients live under `providers/<name>`, one module each, with
  the feature-package shape (`Config`, `Enable`, `From`) and rules: a
  provider imports the root, `pkg/*` and its SDK, never a feature or another
  provider. One depguard block covers the directory.
- A provider wraps the vendor's official SDK and returns it from `From`. It
  adds only what a gox service needs around it: a config section, lifecycle
  or readiness when the vendor has a health signal, HTTP handlers and
  middleware that speak the gox error envelope.
- `providers/workos` delegates every security decision to the SDK: token
  signature, issuer, audience and expiry checks, the JWKS cache, the refresh
  grant, webhook signatures. The provider owns the HTTP edges: the state
  cookie for the login redirect (single-use, local `return_to` only), the
  session cookie attributes, clearing cookies that can never authenticate
  again, and keeping the cookie on transient provider failures.
- Refresh tokens are single-use. Concurrent requests carrying the same one
  share a single grant in-process (`singleflight`); across processes the
  provider's replay grace window applies.
- The cookie layout is pluggable (`WithSessionCodec`). WorkOS SDKs for
  different languages seal the same data in different byte layouts, and a
  service that shares a cookie with an application on another SDK must read
  and write that application's layout. The default is the Go SDK's.
- Sessions are opt-in (`WithSessions`), so a service that only needs the API
  client or webhook verification configures two variables, not five.

## Consequences

- Bearer access tokens and cookies produce the same `Session`, so handlers
  do not care how the caller authenticated. `Session.User` is nil for
  bearer tokens: the profile is sealed in the cookie, not in the token.
- Authorization stays in the service: the provider exposes role and
  permissions, it does not enforce them.
- A service verifying tokens from any other identity provider uses
  `gox/jwt` with `JWT_JWKS_URL`; the two do not depend on each other.
