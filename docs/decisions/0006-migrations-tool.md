# 0006. Migrations: golang-migrate over embedded SQL, on a dedicated connection

Date: 2026-09-13
Status: accepted

## Context

All three audited services that use Postgres use `golang-migrate/migrate`
with plain SQL files. Two run migrations at boot; one runs them as a
separate deploy step. One of them crash-looped under a rolling deploy with
`53300 too many connections` because golang-migrate keeps an advisory-lock
connection open for the life of the `*sql.DB` it is handed, and it had been
handed the shared pool. The alternative, `pressly/goose`, is equally capable
but would be new to every consumer.

## Decision

- `golang-migrate/migrate/v4` with the `iofs` source and the `pgx/v5`
  database driver. Migration files are `NNNN_name.up.sql` / `.down.sql`,
  embedded with `//go:embed migrations/*.sql` and passed to
  `postgres.WithMigrations(fs)`. The runner finds the directory inside the
  embed automatically.
- Migrations run in the postgres component's `Start`, after the pool has
  pinged, so `/readyz` is green only once the schema is current.
- They run over **one dedicated connection** opened for the migration and
  closed after (`sql.OpenDB` with `SetMaxOpenConns(1)`), never over the
  shared pool.
- `POSTGRES_MIGRATE=false` skips boot-time migration for services that
  migrate as a deploy step; `postgres.Migrate(ctx, url, fs)` is the same
  runner callable from a script, a CLI or a test's `TestMain`.
- Down migrations are shipped but never run automatically.

## Consequences

- No ORM, no Go-code migrations. SQL files are the contract, reviewable in
  a diff and lintable with squawk.
- A service that wants migrations checked into version control but applied
  by CI sets `POSTGRES_MIGRATE=false` and calls `postgres.Migrate` from its
  migration job; the same files serve both paths.
- Integration tests prove the runner against a real database
  (`DATABASE_URL`), including idempotence and the fail-fast boot on an
  unreachable database.
