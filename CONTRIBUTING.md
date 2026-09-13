# Contributing to gox

Thanks for your interest in contributing! `gox` is a collection of small,
independent Go modules. Each top-level directory (`http`, `jwt`, `logger`,
`monetary`, `osrelease`, `postgres`, `supabase`) is its own module with its own
`go.mod` and can be built, tested, and released independently.

## Getting started

```bash
git clone https://github.com/guilhermebr/gox
cd gox
```

There is no root module — run commands inside the module you're working on, or
use the provided `Makefile` targets which iterate over every module.

## Development workflow

Before opening a pull request, make sure the full check suite passes:

```bash
make ci          # fmt-check + vet + lint + test-race across all modules
```

Individual targets are also available:

```bash
make fmt         # format all modules
make test-race   # tests with the race detector
make vet         # go vet
make lint        # golangci-lint
make gosec       # gosec security scan
make vulncheck   # govulncheck
make tidy        # go mod tidy
```

You'll need [`golangci-lint`](https://golangci-lint.run/),
[`gosec`](https://github.com/securego/gosec), and
[`govulncheck`](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) installed
for the corresponding targets.

## Guidelines

- **Keep modules self-contained.** Use the Go standard library where possible;
  add external dependencies only when necessary.
- **Document exported identifiers.** Every exported type, function, and method
  needs a godoc comment that starts with its name.
- **Test your changes.** Add table-driven tests for new behavior; tests must
  pass with `-race`.
- **Match the existing config pattern.** Configuration uses
  `github.com/ardanlabs/conf/v3` with a `<PREFIX>_<MODULE>_*` environment layout.
- **Format before committing.** `make fmt` (or `gofmt -w .`).

## Pull requests

- Keep PRs focused on a single module/change where possible.
- Describe the motivation and any behavior changes in the PR description.
- Ensure CI is green.

## Releases

Modules are versioned independently using module-path tags, e.g.
`logger/v0.1.0`, `postgres/v0.2.0`.
