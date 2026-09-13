# 0002. Explicit lifecycle, no dependency-injection framework

Date: 2026-09-13
Status: accepted

## Context

uber-go/fx, dig and wire solve "build this graph of constructors and start it
in the right order". They do it with reflection or code generation over
constructor signatures, and the resulting program is hard to read top to
bottom: what starts when is decided by type resolution, not by the file in
front of you. Every one of the four audited services wired its dependencies
by hand in `main()`, in one case across 650 lines, and none of them used a DI
container.

## Decision

- gox does not own `main()` and does not resolve a graph. The service
  declares what it needs with options; gox instantiates, configures, orders,
  starts and stops.
- Ordering is by **stage**, not by dependency analysis:
  datastores → clients → user components → public server → admin server.
  Config, logging and telemetry are ready before any component factory runs.
  A feature package picks its stage when it registers a factory.
- `pkg/lifecycle.Manager` is the only orchestrator: staged start with
  rollback on failure, reverse stop with a fresh timeout per component,
  supervision of long-running `Runner` components whose failure is fatal,
  and `Periodic` jobs that survive errors and panics.
- Feature values are handed to services through typed accessors
  (`postgres.From(a)`) backed by a small keyed store on the App, not by
  injecting them into constructors.

## Consequences

- No reflection over user types, no code generation step, no graph errors
  at startup that a reader cannot trace.
- Services that need a component built from another feature's value do it
  in a factory: `b.Component(gox.StageUser, func(a *gox.App) (Component,
  error) { db := postgres.From(a); ... })`. The stage order guarantees the
  datastore exists.
- Circular needs cannot be expressed. That is a feature.
