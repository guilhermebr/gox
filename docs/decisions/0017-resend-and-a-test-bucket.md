# 0017 — Resend provider with bounded retries, and an in-memory test bucket

Date: 2026-11-14
Status: accepted

## Context

Two services sending through Resend had each written their own sender: one
on the official SDK with retries and an idempotency key, one on raw HTTP
with neither. Both also carried an in-memory `storage.Bucket` for tests,
with different strictness: one checked a direct upload against what the
presigned URL allowed, the other accepted anything.

0014 says there is no queue inside a sender and that retries come from a
`gox/jobs` worker. That holds for mail that can wait. A sign-in link is
sent inside the request that asked for it, and a single dropped connection
should not make the person ask again.

## Decision

- `providers/resend` is a `mail.Sender` on `resend-go`: `Enable()`,
  `Sender(a)`, `From(a)` for the SDK client, section `RESEND`.
- `RESEND_API_KEY` is optional, as `POSTHOG_PROJECT_KEY` is. Without it
  `Configured(a)` is false and `Send` returns `ErrNotConfigured`: a service
  can run in development with no mail and decide what to do instead (log
  the link). Mail is never dropped silently.
- `Send` makes at most three attempts, only for network errors, 5xx and
  429, with exponential backoff capped at 5 seconds and a `Retry-After`
  honoured up to 15 seconds. Other 4xx answers are final. This is not a
  queue: the worst case stays inside a request timeout, and mail that can
  wait still belongs in a job.
- The idempotency key travels as `Message.Headers["Idempotency-Key"]`
  (`resend.HeaderIdempotencyKey`). The sender moves it to the request
  header, so `mail.Message` stays provider-neutral and every attempt of one
  `Send`, and a caller's own retry, deliver once.
- `pkg/storage/storagetest` is the strict in-memory bucket. `Upload` plays
  the client's PUT and refuses what S3 refuses; constraints left at zero
  are not enforced, as `storage.PresignPutOptions` documents. It lives in
  the root module because it depends only on `pkg/storage`.

## Consequences

- When the service enables `gox.HTTPClient()` the provider sends through
  it (tracing, the service's transport); otherwise through a client with a
  15 second timeout.
- A retry without an idempotency key can deliver twice when the first
  attempt reached Resend and only its answer was lost. Callers that care
  set the key.
- `storagetest` ships in the root module, so releasing it means a root tag.
