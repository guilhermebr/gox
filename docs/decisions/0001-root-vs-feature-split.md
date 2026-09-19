# 0001. One root import, features opted in by import

Date: 2026-09-13
Status: accepted

## Context

gox is the base for services written with AI assistants. The fewer decisions
a service author (or a model) makes, the better: which logger, router, config
loader, DB driver, observability stack. At the same time a plain HTTP service
must not carry pgx, a Supabase SDK or templ in its binary.

Go decides what is compiled by the import graph, not by runtime calls. A
package that is never imported is never compiled or linked.

## Decision

- The root package `github.com/guilhermebr/gox` is the only import a plain
  service needs. It contains lifecycle, config, logging, observability,
  health/admin and HTTP. It depends only on the standard
  library, `ardanlabs/conf` and the OpenTelemetry SDK.
- Every dependency-heavy feature is a subpackage (`gox/postgres`,
  `gox/jwt`, `gox/web`, `gox/providers/supabase`). Importing it and passing its
  `Enable()` option to `gox.New` is the opt-in. Its `From(app)` accessor
  returns the built value.
- Feature packages plug in through the `Builder` extension API
  (`ConfigSection`, `Component`, `Set`) so the root never knows about them.
- `scripts/check-deps.sh` proves the rule in CI: the root-only example must
  not link pgx, supabase-go, golang-jwt or templ.

## Consequences

- A service's `main()` reads as a list of what it is made of.
- Accessors for features that were not enabled panic at call time with a
  message naming the option to add. That is a programming error and should
  surface immediately, not be silently nil.
- The root package is the one "wide" package on purpose; `pkg/*` stays
  small and single-purpose so the root's surface is what services see.
- Build tags, `init()` registration and blank imports are not used as opt-in
  mechanisms: they hide the dependency from the reader.
