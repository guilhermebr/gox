# 0015 — Client IP, rate limits, pagination, env aliases, transaction settings, i18n

Date: 2026-09-20
Status: accepted

## Context

A set of small needs shows up in every service once it faces real traffic.
Each is a few dozen lines that are easy to get subtly wrong, and none
justifies a dependency.

## Decision

- **Client IP.** `HTTP_TRUSTED_PROXIES` lists proxy CIDRs. The middleware is
  always in the chain; with nothing trusted the client is the peer.
  `X-Forwarded-For` is read right to left, skipping trusted hops, so entries
  a client prepends are never used, and a malformed chain stops the walk.
  `gox.ClientIP(r)` is the only accessor; the access log carries it.
- **Rate limiting** is a per-route wrapper, not a chain default: a token
  bucket per client IP, in memory, per process. It protects logins and
  signups without another datastore; a fleet-wide quota is a different
  problem and belongs at the edge.
- **Pagination** is keyset only. `ParsePage` reads `after` and `limit` with a
  default and a ceiling; cursors are base64url JSON of the sort key, opaque
  to clients and validated on the way in. The response envelope stays the
  service's own.
- **Env aliases.** `gox.WithEnvAlias(name, alias)` lets a variable fall back
  to the name a platform injects (`DATABASE_URL`). It is an option, not
  built-in knowledge of platforms; the service's own variable always wins.
  `PORT` remains the one alias the framework knows.
- **Transaction settings.** `postgres.TxWith` applies `set_config(..., true)`
  inside the transaction, which is the only safe way to hand values to
  triggers and row-level security through a pool: the value lives and dies
  with the transaction's connection.
- **i18n** is `pkg/i18n`: JSON catalogs per locale with dotted keys, a
  fallback locale, `{name}` placeholders (and Ruby's `%{name}`), locale from
  a cookie then `Accept-Language`, `Content-Language` on the response, and
  `Messages` to hand a frontend a subtree. No plural rules and no
  formatting: those need CLDR data, which is a dependency the root does not
  take. Services import it directly, like `pkg/storage` and `pkg/mail`.

## Consequences

- Every helper is stdlib-only and lives in the root module.
- Cross-origin protection, the body limit and the timeout remain the chain's
  defaults; rate limiting is the one protection a service opts into per
  route.
