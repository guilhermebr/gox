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
iterate over every module.

## Development workflow

Run `make ci` before opening a pull request; `make help` lists every target.
CI also runs `make test-integration`, whose tests skip without their servers
(see `AGENTS.md`).

You'll need Go 1.26+, [`golangci-lint`](https://golangci-lint.run/) v2 at the
version CI pins (`.github/workflows/ci.yml`; `AGENTS.md` has the install line),
`python3` (for `make check-recipes`), and
[`govulncheck`](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) for
`make vulncheck`.

## Guidelines

- **Keep modules self-contained.** Use the Go standard library where possible;
  add external dependencies only when necessary.
- **Adding a feature package or provider?** Follow
  `.claude/skills/gox-add-module/SKILL.md`; `docs/features.md` explains the code.

## Pull requests

- Keep PRs focused on a single module/change where possible.
- Describe the motivation and any behavior changes in the PR description.
- Ensure CI is green.

## Releases

Modules are versioned independently: `vX.Y.Z` for the root,
`<module>/vX.Y.Z` for the rest (`git tag -l` lists them). The order and the
checks are in `.claude/skills/gox-release/SKILL.md`.
