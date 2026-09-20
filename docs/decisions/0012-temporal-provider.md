# 0012 — Temporal as a provider, next to jobs

Date: 2026-09-19
Status: accepted

## Context

`gox/jobs` covers single tasks that run later and retry. Some processes are
workflows: several steps, waits measured in days, signals from outside,
compensation when a late step fails. Building that on a job queue means
writing the bookkeeping by hand. Temporal is the engine for it, at the cost
of a cluster to run (or Temporal Cloud).

## Decision

- `providers/temporal` follows the provider rules: `From` returns the SDK
  client, and the module adds only the config section, the lifecycle and the
  observability wiring.
- The client is lazy so `New` needs no server; the component's `Start`
  checks the connection and fails the boot when the frontend is
  unreachable, and `Ready` repeats the check for `/readyz`.
- `temporal.Worker(a, taskQueue, options)` returns one worker per task
  queue, created between `New` and `Run` like route registration.
  Registration stays the SDK's own (`RegisterWorkflow`, `RegisterActivity`).
  The component starts the workers after the datastores and stops them
  before closing the client; running activities get the app's shutdown
  timeout unless the options say otherwise.
- `TEMPORAL_WORK=false` starts no worker, mirroring `JOBS_WORK`: an API
  that only starts workflows next to a worker binary from the same code.
- TLS is automatic with an API key or a client certificate; mTLS material
  is PEM in config, like the JWT keys.
- The SDK's OpenTelemetry interceptor is installed with the global tracer
  the root package configures, and the SDK logs through the service logger.

## Consequences

- Jobs and workflows coexist: a job inserted in a database transaction can
  start a workflow, which gives the transactional guarantee on the way in
  and durable orchestration after.
- The integration test needs a Temporal server (`TEMPORAL_ADDRESS`) and is
  skipped without one; `temporal server start-dev` is enough.
