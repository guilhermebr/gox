# Contributing to gox

Thanks for your interest in contributing! `gox` is a multi-module repository:
the root module (`github.com/guilhermebr/gox`) plus one module per feature
package, one per third-party provider under `providers/` (`providers/README.md`
lists them), the stdlib-only utilities (`monetary`, `osrelease`) and
`examples`; `go.work` lists every module. See
`docs/decisions/0000-module-layout.md` for why. `AGENTS.md` holds the rules
every change follows, whoever writes it.

## Getting started

```bash
git clone https://github.com/guilhermebr/gox
cd gox
```

A `go.work` file ties the modules together for local development, so `go
build` and `go test` work from any module directory. The `Makefile` targets
iterate over every module; CI runs `make ci` and `make test-integration`.

## Development workflow

Before opening a pull request, make sure the full check suite passes:

```bash
make ci          # the full check suite; `make help` lists the parts
```

Individual targets are also available (`make help` lists them):

```bash
make fmt              # gofumpt + goimports via golangci-lint
make test             # tests with the race detector
make test-integration # tests tagged `integration` (needs DATABASE_URL)
make vet              # go vet
make lint             # golangci-lint with the shared .golangci.yml
make check-deps       # prove a root-only example links no feature dependency
make check-lint-rules # prove each depguard dependency rule fires
make vulncheck        # govulncheck
make tidy             # go mod tidy in every module
```

You'll need Go 1.26+, [`golangci-lint`](https://golangci-lint.run/) v2 at the
version CI pins (`.github/workflows/ci.yml`; `AGENTS.md` has the install line),
and [`govulncheck`](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) for
the corresponding targets.

## Guidelines

- **Keep modules self-contained.** Use the Go standard library where possible;
  add external dependencies only when necessary.
- **Document exported identifiers.** Every exported type, function, and method
  needs a godoc comment that starts with its name.
- **Test your changes.** Add table-driven tests for new behavior; tests must
  pass with `-race`.
- **Respect the dependency direction** listed in `AGENTS.md`. `depguard`
  enforces it; see `docs/decisions/0001-root-vs-feature-split.md` for why.
- **Adding a feature package or provider?** Follow
  `.claude/skills/gox-add-module/SKILL.md`; `docs/features.md` explains the code.
- **Match the config pattern.** Configuration uses `github.com/ardanlabs/conf/v3`;
  feature packages register a config section under the service prefix.
- **Format before committing.** `make fmt`.

## Pull requests

- Keep PRs focused on a single module/change where possible.
- Describe the motivation and any behavior changes in the PR description.
- Ensure CI is green.

## Releases

Modules are versioned independently using module-path tags, e.g.
`postgres/v0.2.0`, `web/v0.1.0`. The order and the checks are in
`.claude/skills/gox-release/SKILL.md`.
