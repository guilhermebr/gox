# AGENTS.md — working inside the gox repository

This file is for AI agents (and humans) editing gox itself. For agents
building a *service on* gox, see the `AGENTS.md` that `gox new` puts in
every generated service (source: `cmd/gox/internal/scaffold/_template/base/AGENTS.md`)
and `llm.txt`.

## What gox is

An opinionated Go service framework: a root package that is the one import
for a plain HTTP service, plus feature packages opted in by import
(`postgres`, `jwt`, `supabase`, `web`). Read `docs/guide.md` for the
design, `docs/features.md` for how feature packages plug in, and
`docs/decisions/` for why each choice was made.

## Layout and dependency direction (lint-enforced)

```
gox.go builder.go app.go run.go http.go errors.go helpers.go   root package
pkg/lifecycle config log errors health admin middleware httpx httpserver httpclient otel
postgres/ jwt/ supabase/ web/     feature packages, one module each
examples/                          runnable examples, one module
cmd/gox/                           CLI: `gox new` (scaffolder; template under internal/scaffold/_template) and `gox docs` (llm.txt generator)
docs/llm/                          fragments assembled into llm.txt
docs/decisions/                    ADRs
docs/recipes/                      copy-pasteable tasks (build-checked)
```

- root → `pkg/*`, stdlib, `ardanlabs/conf`, OpenTelemetry only.
- feature packages → root + `pkg/*` + their own dependency; never each other.
- `pkg/*` → other `pkg/*`, stdlib, third-party; never the root or a feature.
- `monetary`, `osrelease` → stdlib only.
- depguard enforces this (`.golangci.yml`); `make check-lint-rules` proves the rules fire.

## How to run checks

```
make ci              # everything CI runs: fmt-check vet lint test generate-check llm-check check-recipes check-deps check-lint-rules
make test            # race tests, every module
make test-integration# needs DATABASE_URL pointing at a throwaway postgres database the tests own; CI provides one
make lint            # golangci-lint v2 with the shared .golangci.yml
make fmt             # gofumpt + goimports
make generate        # templ generate for examples/web (CLI pinned to web/go.mod)
make llm             # regenerate llm.txt after any public API or config change
```

The scaffold template lives in `cmd/gox/internal/scaffold/_template/{base,postgres,web}`
(a leading underscore keeps Go tooling out of it; `all:_template` embeds it).
`.tmpl` files are Go text templates over `scaffold.data`; `__name__` in a
path becomes the service name. templ files there ship with their generated
`_templ.go` (regenerate with `templ generate -path cmd/gox/internal/scaffold/_template/web`
after changing them). `TestGeneratedServiceBuildsTestsAndAnswersHealthz`
renders `--web` and `--postgres --web` against the working tree and runs
them; keep it green.

`make ci` must be green before a phase or a PR is considered done.

## Rules

- Test first. Every exported function has a test that failed before the code existed.
- Every exported identifier has a doc comment; the first sentence ends up in `llm.txt`, so make it the useful one.
- One way to do each thing. Do not add an alias, a second constructor, or an option that duplicates config.
- Every panic and startup error names the fix: `gox: postgres.From called but postgres.Enable() was not passed to gox.New`, `BILLING_POSTGRES_URL is required`.
- Config: fields on structs with `conf` tags; no direct `os.Getenv`. Help text must not contain commas (conf splits tag options on commas).
- Errors wrap with the package prefix: `fmt.Errorf("postgres: connect: %w", err)`.
- Context is the first parameter, never stored in a struct.
- Commit per logical step with a single-line message. No attribution footers.

## Adding a feature package

1. `mkdir <name> && go mod init github.com/guilhermebr/gox/<name>`; add `require github.com/guilhermebr/gox v0.0.0` and `replace github.com/guilhermebr/gox => ../` (until the root is tagged); add `./<name>` to `go.work`.
2. `Config` struct with `conf` tags and a `Validate() error`.
3. `Enable(opts ...Option) gox.Option` that calls `b.ConfigSection("<NAME>", cfg, "<name>.Enable()")` and registers a `b.Component(stage, factory)` (or `b.Setup` for a value with no lifecycle, `b.Finish` to decorate another feature's output, `b.Middleware` for request middleware, `b.ErrorRenderer` for error output).
4. `From(a *gox.App) T` implemented as `gox.MustValue[T](a, key{}, "<name>.From", "<name>.Enable()")`.
5. Tests through a real `gox.New`, an example under `examples/`, a depguard rule in `.golangci.yml`, a line in `cmd/gox/main.go`'s spec so `llm.txt` documents it, a probe line in the Makefile's `check-deps`, and an ADR.

## Never

- Never add uber-go/fx, dig, wire, a third-party router, or an ORM.
- Never import a heavy dependency from the root or from `pkg/*`.
- Never use build tags, `init()` registration, or blank imports as an opt-in mechanism.
- Never edit `llm.txt` by hand; edit `docs/llm/*.md` or the source and run `make llm`.
- Never silently break a public API; deprecate for one minor version first.
