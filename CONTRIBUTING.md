# Contributing to gox

Thanks for your interest in contributing! `gox` is a multi-module repository:
the root module (`github.com/guilhermebr/gox`) plus one module per feature
package (`postgres`, `supabase`, `jwt`, `web`), the stdlib-only utilities
(`monetary`, `osrelease`) and `examples`. See `docs/decisions/0000-module-layout.md` for why.

## Getting started

```bash
git clone https://github.com/guilhermebr/gox
cd gox
```

A `go.work` file ties the modules together for local development, so `go
build` and `go test` work from any module directory. The `Makefile` targets
iterate over every module; `make ci` is what CI runs.

## Development workflow

Before opening a pull request, make sure the full check suite passes:

```bash
make ci          # everything CI runs; `make help` lists the parts
```

Individual targets are also available (`make help` lists them):

```bash
make fmt              # gofumpt + goimports via golangci-lint
make test             # tests with the race detector
make test-integration # tests tagged `integration` (needs DATABASE_URL)
make vet              # go vet
make lint             # golangci-lint with the shared .golangci.yml
make check-deps       # prove a root-only example links no feature dependency
make check-lint-rules # prove the depguard dependency rules fire
make vulncheck        # govulncheck
make tidy             # go mod tidy in every module
```

You'll need Go 1.26+, [`golangci-lint`](https://golangci-lint.run/) v2, and
[`govulncheck`](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) for the
corresponding targets.

## Guidelines

- **Keep modules self-contained.** Use the Go standard library where possible;
  add external dependencies only when necessary.
- **Document exported identifiers.** Every exported type, function, and method
  needs a godoc comment that starts with its name.
- **Test your changes.** Add table-driven tests for new behavior; tests must
  pass with `-race`.
- **Respect the dependency direction.** `pkg/*` never imports the root or a
  feature package; the root imports only `pkg/*`, stdlib, `ardanlabs/conf` and
  OpenTelemetry; `monetary` and `osrelease` are stdlib only. `depguard` enforces
  this; see `docs/decisions/0001-root-vs-feature-split.md`.
- **Match the config pattern.** Configuration uses `github.com/ardanlabs/conf/v3`;
  feature packages register a config section under the service prefix.
- **Format before committing.** `make fmt`.

## Pull requests

- Keep PRs focused on a single module/change where possible.
- Describe the motivation and any behavior changes in the PR description.
- Ensure CI is green.

## Releases

Modules are versioned independently using module-path tags, e.g.
`postgres/v0.2.0`, `web/v0.1.0`.
