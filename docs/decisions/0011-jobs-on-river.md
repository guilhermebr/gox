# 0011 — Background jobs on River

Date: 2026-09-19
Status: accepted

## Context

`gox.Periodic` covers in-process timers. Services also need durable work:
jobs that survive a restart, retry with backoff, are enqueued in the same
transaction as the change that needs them, and run on a schedule once per
fleet rather than once per process. The services gox is built for already
run Postgres and nothing else.

## Decision

- `gox/jobs` wraps River, a Postgres-backed queue for Go. No Redis, no
  second datastore to operate, and transactional enqueueing comes from
  sharing the database.
- `jobs.From(a)` returns the River client itself. The module adds what a
  gox service needs around it: the `JOBS` config section, River's
  migrations at boot, a component at `StageUser` that starts after the
  datastores and drains before they close (graceful stop within the
  shutdown budget, then cancellation), failures and panics in the service
  log, and `Register`/`Schedule` helpers called between `New` and `Run`
  like route registration.
- Feature modules do not import each other, so `Enable` takes the pool as a
  function: `jobs.Enable(postgres.From)`. The wiring is explicit and the
  lint rule holds.
- Schedules are cron expressions (with an optional `CRON_TZ=` zone) or
  `@every` intervals, parsed at registration so a typo fails the boot.
  River's leader election makes a schedule fire once per fleet.
- `JOBS_WORK=false` makes a process insert-only, which is how an API and a
  separate worker share one codebase.

## Consequences

- River owns its tables (`river_job` and friends) in the application
  database and migrates them itself, separately from the service's
  golang-migrate files.
- `gox.Periodic` stays for work that needs no durability or fleet-wide
  exclusivity.
