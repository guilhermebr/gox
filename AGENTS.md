# AGENTS.md: changing gox itself

Building a service on gox? Use the `AGENTS.md` that `gox new` generated.

## Read first for your task

- New module (feature package or provider; anything that adds a `go.mod`): `.claude/skills/gox-add-module/SKILL.md`
- Tagging or releasing a module, or pointing one at a new root version: `.claude/skills/gox-release/SKILL.md`
- Anything under `cmd/gox/internal/scaffold/`: `cmd/gox/internal/scaffold/AGENTS.md`
- Docs that agents read (`docs/llm`, `docs/recipes`, `docs/decisions`, doc comments, `llm.txt`): `docs/AGENTS.md`
- Anything under `providers/`: `providers/AGENTS.md`
- Changing behaviour an ADR decided: `docs/decisions/README.md`, then only that ADR

Otherwise this file is enough.

## Layout and imports

gox is a root package, the one import of a plain HTTP service, plus modules
a service opts into by importing them. `go.work` lists every module.

```
*.go                  root package
pkg/<name>/           building blocks, inside the root module
<feature>/            feature packages (postgres/, web/, ...), one module each
providers/<name>/     third-party service clients, one module each
monetary/ osrelease/  stdlib-only utilities
examples/             runnable examples, one module
cmd/gox/              CLI: gox new (scaffold) and gox docs (llm.txt)
docs/                 llm/ (llm.txt sources), recipes/, decisions/ (ADRs)
```

- root → stdlib, `pkg/*`, `ardanlabs/conf`, OpenTelemetry.
- `pkg/*` → stdlib, other `pkg/*`, third-party; never the root, a feature or a provider.
- feature → root, `pkg/*`, its own dependency; never another feature or a provider.
- provider → root, `pkg/*`, its own SDK; never a feature or another provider.
- `monetary`, `osrelease` → stdlib only.
- depguard in `.golangci.yml` enforces these; `scripts/check-lint-rules.sh` (`make check-lint-rules`) proves each rule fires.

## Done

- `make ci` is green: CI runs it and `make test-integration` (`make fmt` fixes
  formatting). It needs golangci-lint v2.11.4, the CI pin, on PATH:
  `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4`.
- While iterating: `make test lint MODULES=./<module>` (the root module is `.`);
  `go test -short ./cmd/gox/...` skips the scaffold end-to-end test.
- Integration tests (`make test-integration`) skip silently without DATABASE_URL
  (postgres, jobs), S3_TEST_* (providers/s3) or TEMPORAL_ADDRESS
  (providers/temporal): look for SKIP in `go test -tags integration -v`.
- One commit per logical step, one line: `type(scope): subject`, the scope
  being the module or package, if any. No Co-authored-by or other attribution trailers.

## Rules

- Test first. Every exported function has a test that failed before the code existed.
- Every exported identifier has a doc comment; `llm.txt` keeps only its first sentence (or its `Deprecated:` one), so make that the useful one.
- One way to do each thing: no alias, no second constructor, no option that duplicates config.
- Every panic and startup error names the fix: `gox: postgres.From called but postgres.Enable() was not passed to gox.New`, `BILLING_POSTGRES_URL is required`.
- Config is struct fields with `conf` tags, never `os.Getenv`. Secrets carry `mask`. `help:` text has no commas (conf splits tag options on commas).
- Errors wrap with the package prefix: `fmt.Errorf("postgres: connect: %w", err)`.
- Context is the first parameter, never stored in a struct.
- Never edit generated files: `llm.txt` comes from `docs/llm/*.md` and doc comments via `make llm`; `*_templ.go` from `.templ` files via `make generate`.
- Never break a public API, even when asked to rename or remove: keep the old name as a `// Deprecated:` wrapper for one minor version.
- A released module that starts using new root API needs a root release first. `go.work` hides the gap; `GOWORK=off go build ./...` in the module shows it.

## Never

- uber-go/fx, dig, wire, a third-party router or an ORM.
- A heavy dependency imported from the root or from `pkg/*`.
- Build tags, `init()` registration or blank imports as an opt-in mechanism.
