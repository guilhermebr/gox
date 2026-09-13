# 0004. Coded errors with a public-safe message and one envelope

Date: 2026-09-13
Status: accepted

## Context

Every audited service had a hand-written switch mapping domain sentinels to
HTTP statuses, three different JSON envelopes, and in one case fifteen
`err.Error() == "literal"` comparisons. One service deliberately never
echoed `err.Error()` to clients because user input ends up in it.

## Decision

- `pkg/errors.Error` carries a `Code` (numbering and names mirror gRPC
  status codes), a public-safe `Message`, optional public `Details` and a
  private cause. `Error()` includes the cause for logs; `Message()` is what
  clients see.
- One constructor per code (`errors.NotFound("invoice %s", id)`), `Wrap`
  to attach a cause, `CodeOf` and `HTTPStatus` that look through wrapping,
  and a fixed code→status table. Context errors map to
  `deadline_exceeded` (504) and `canceled` (499).
- The JSON envelope is flat and stable:
  `{"code": "not_found", "message": "...", "request_id": "...", "details": {...}}`.
  Errors that are not `*Error` render as `internal` / `internal error`
  and never leak their text.
- Services keep their own sentinels. `gox.WithErrorMapper` registers
  functions that translate them at the boundary; `gox.Error` (Phase 2)
  applies the mappers before rendering. Nothing forces a rewrite of a
  domain package to adopt the envelope.
- Every rejection produced by the framework (recovery, auth, timeout, body
  limit, 404/405) uses the same envelope. Clients never see plain text from
  one layer and JSON from another.

## Consequences

- The root re-exports the constructors and helpers so services import only
  `gox`.
- Two errors with the same code are not `errors.Is`-equal; compare codes
  with `CodeOf`. Sentinels a service wants to match by identity remain the
  service's own.
- Details are public by definition. Anything sensitive belongs in the cause
  and in logs.
