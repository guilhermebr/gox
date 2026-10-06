# providers/: third-party service clients

Deltas for `providers/<name>`; the root AGENTS.md applies too. Adding a
provider: `.claude/skills/gox-add-module/SKILL.md`.

- Wrap the vendor's official SDK; `From` returns the SDK client. Add only a
  config section, lifecycle or readiness, and the service's edges (webhook
  verification, middleware that speaks the gox error envelope). Never
  re-wrap the vendor's API (ADR 0008).
- Where `pkg/` has a vendor-neutral interface, implement it: `s3.From`
  returns a `storage.Bucket`; `mailgun.Sender(a)` returns a `mail.Sender`
  while `mailgun.From` stays the SDK client (ADR 0013, 0014).
- Build the client on `a.HTTPClient()` when `a.HasHTTPClient()` and the SDK
  takes an `*http.Client` (stripe, s3, mailgun, workos).
- A value from another module arrives as a function argument, never as an
  import of another provider or a feature. `make lint` enforces both, the
  provider imports in non-test files only: a provider's tests import it.
- With a health signal, give the component `Ready(ctx) error` (it joins
  `/readyz`) and call it from `Start` so a bad address or key fails the
  boot, as s3 and temporal do. A client with nothing to start, stop or
  check is a value set in `b.Setup` (stripe).
- Integration tests (`//go:build integration`) need a real server. CI has
  none, so they skip there; run them in the module with
  `go test -tags integration -v ./...`:
  - `providers/s3`: `S3_TEST_ENDPOINT`, `S3_TEST_BUCKET`,
    `S3_TEST_ACCESS_KEY_ID`, `S3_TEST_SECRET_ACCESS_KEY`: an S3-compatible
    server where the bucket exists (MinIO works).
  - `providers/temporal`: `TEMPORAL_ADDRESS` (`temporal server start-dev`).
- Some provider go.mod files still require gox `v0.0.0` with `replace
  github.com/guilhermebr/gox => ../../`, from before the root was tagged;
  `git grep -l '^replace github.com/guilhermebr/gox' -- '*go.mod'` lists
  them. Never copy one; releasing the module drops it
  (`.claude/skills/gox-release/SKILL.md`).
