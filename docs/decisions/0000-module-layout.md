# 0000. Module layout: a root module plus one module per heavy package

Date: 2026-09-13
Status: accepted

## Context

gox is becoming a framework whose root package (`github.com/guilhermebr/gox`)
is the one import for a plain HTTP service, while datastores and other heavy
integrations (`gox/postgres`, `gox/supabase`, `gox/jwt`, `gox/web`) are opted
into by importing them (`GOX_FRAMEWORK_PLAN.md` §1 and §2).

What the compiler links is decided by the import graph, so import-as-opt-in
holds with any module layout. What differs is the consumer's **module graph**:

- With a **single module**, `go get github.com/guilhermebr/gox` records pgx,
  supabase-go, golang-jwt and templ in every consumer's `go.mod`/`go.sum`,
  even for a service that never imports them. Dependabot, `govulncheck ./...`
  and security review all see dependencies the binary does not contain.
- With **nested modules**, a consumer that imports only the root sees only the
  root's dependencies (stdlib, `ardanlabs/conf`, OpenTelemetry). Feature
  packages are versioned and tagged separately (`postgres/v0.3.0`).

Today the repository already has one module per package and no root module.
The four services that will migrate to gox pin per-subpackage pseudo-versions
(`docs/audit-consumers.md` §12). The OpenTelemetry `contrib` repository and
`google.golang.org/genproto` use the nested layout at scale.

## Decision

Nested modules:

- `github.com/guilhermebr/gox` — root module. Contains the root package and
  `pkg/*`. Depends only on stdlib, `ardanlabs/conf`, and the OpenTelemetry SDK.
- `github.com/guilhermebr/gox/postgres`, `.../supabase`, `.../jwt`, `.../web`
  — one module each, requiring the root module.
- `github.com/guilhermebr/gox/monetary`, `.../osrelease` — one module each,
  stdlib only.
- `github.com/guilhermebr/gox/http`, `.../logger` — deprecated shim modules
  until removed.
- `github.com/guilhermebr/gox/examples` — one module for all examples, using
  `replace` directives so it always builds against the working tree.
- `go.work` at the root lists every module for local development. It is
  committed; consumers are unaffected because `go.work` only applies inside
  this checkout.

Tags are per module: `v0.4.0` for the root, `postgres/v0.4.0` for a feature.
Feature modules require a released root version, never a pseudo-version, so
`go get github.com/guilhermebr/gox/postgres@latest` resolves cleanly.

## Consequences

- `make` targets and CI iterate over every module (`find . -name go.mod`).
  One shared `.golangci.yml` at the root is passed explicitly with `--config`.
- Adding a feature package means adding a `go.mod`, a `go.work` entry, a CI
  matrix entry, and a depguard rule.
- Releases touch several tags. A `make release` helper (Phase 5) can automate
  the fan-out, but a root change that feature modules need requires a root tag
  first and a feature tag second.
- `pkg/*` lives inside the root module, so feature modules reach it through
  their dependency on the root, and `depguard` (not module boundaries) enforces
  that `pkg/*` never imports the root or a feature package.
