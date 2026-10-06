---
name: gox-add-module
description: Add a new module to the gox repository - a top-level feature package (like postgres, jobs, jwt, openapi, web), a third-party provider under providers/ (like stripe, s3, workos) or a stdlib-only library (like monetary) - and wire it into everything that must know about it - go.mod and go.work, the Config/Enable/From shape, tests through gox.New, depguard, check-deps, llm.txt, a recipe, an ADR, the service skill table and the lists of modules. Use it whenever a change creates a new go.mod in this repository, wraps a new vendor SDK, or adds support for a database, cache, queue, auth method or SaaS API to gox itself, even if the word module is not used. Not for using an existing feature from a service built on gox (that service's gox-add-feature skill), not for a new package inside an existing module (pkg/<name> lives in the root module), and not for releasing or tagging a module (gox-release).
---

# Add a module to gox

Most of the wiring is lists that no check verifies, and they are how agents
in generated services learn a module exists: llm.txt, the recipe index, the
gox-add-feature table, the root package comment. So do every step for your
kind; `make ci` proves the rest.

## 1. Choose the kind

- **Feature**: a building block a service is made of (a datastore, auth,
  HTML). Directory `<name>/`, module `github.com/guilhermebr/gox/<name>`.
- **Provider**: a client for one third-party service (a cloud or SaaS API).
  Directory `providers/<name>/`, module
  `github.com/guilhermebr/gox/providers/<name>`. Read `providers/AGENTS.md`.
- **Library**: stdlib only, no gox import (like `monetary`, `osrelease`).
  Directory `<name>/`. Follow "Library" at the end instead of steps 3-5.
- **Not a module**: a vendor-neutral interface that providers implement
  (`storage.Bucket`, `mail.Sender`; ADR 0013, 0014) goes in `pkg/<name>` in
  the root module.

Below, `<dir>` is `<name>` or `providers/<name>`, `<NAME>` is the config
section (`STRIPE`) and `<sdk>` is the module path of what you wrap.

## 2. Create the module

From the repository root:

```bash
mkdir <dir> && cd <dir>
go mod init github.com/guilhermebr/gox/<dir>
go mod edit -go=1.26 -require=github.com/guilhermebr/gox@$(git tag -l 'v*' --sort=-v:refname | head -1)
GOWORK=off go get <sdk>@latest    # or the version you need
cd - && go work use ./<dir>
```

- `-go=1.26` is the root's `go` line. `go mod init` writes your toolchain's
  patch version (`go 1.26.1`), which every consumer would then need, and
  `go work use` would raise go.work to it.
- Require the newest root tag and add no `replace`. Go ignores a replace in
  a dependency's go.mod, so a module that needs one is broken for everyone
  who imports it by version; go.work already builds it against the working
  tree. Some sibling go.mod files still require `v0.0.0` with a local
  `replace github.com/guilhermebr/gox` from before the root was tagged:
  never copy one.
- Read the SDK's API with `go doc <sdk>`. Once the code compiles, run
  `GOWORK=off go mod tidy` in `<dir>`: it resolves the module the way a
  consumer does and writes go.sum.

## 3. Write the code

`docs/features.md` walks through every piece (Config, Validate, Enable and
the `Builder` hooks, `From`, Start and Ready); the root AGENTS.md rules apply.
Models in the tree: `providers/stripe` (a value, `b.Setup`),
`providers/posthog` (a lifecycle, `b.Component`), `providers/s3` (Start and
Ready check the bucket), `postgres` (options, readiness, migrations).

A value owned by another module arrives as a function argument
(`jobs.Enable(postgres.From)`, ADR 0011). Never import the module.

## 4. Test through a real gox.New

Use an external test package (`package <name>_test`) and copy `quiet` and
`setArgs` from `postgres/postgres_test.go`. At least:

- `gox.New` without a required variable fails, and the error names it
  (`SHOP_<NAME>_<FIELD>`).
- `From` on an app built without `Enable()` panics with
  `gox: <name>.From called but <name>.Enable() was not passed to gox.New`.
- `--help` lists the section: `setArgs(t, "--help")`, `errors.As(err, &help)`
  with `help *config.HelpError` (`pkg/config`), and `help.Usage` contains
  the variables and `<name>.Enable()`.
- What the module adds, against an `httptest` server or a fake.
- Anything that needs a real server goes behind `//go:build integration`,
  reads its address from the environment and calls `t.Skip` when it is
  unset (`providers/s3/integration_test.go`).

## 5. Wire it into the repository

1. **depguard** (`.golangci.yml`). Provider: nothing to do; the `providers`
   and `provider-siblings` rules match `**/providers/**`. Feature:
   - add `- "!**/<name>/**"` to the `files` of the `root` rule;
   - copy the `feature-postgres` rule as `feature-<name>`, set its `files`
     to `"**/<name>/**"` and add `postgres` to its deny list, so it denies
     every other feature and `github.com/guilhermebr/gox/providers`;
   - add `- pkg: github.com/guilhermebr/gox/<name>`, with the `desc` of its
     neighbours, to the deny lists of `pkg`, every other `feature-*` rule
     and `providers`;
   - copy the `feature-postgres` probe line in `scripts/check-lint-rules.sh`
     for `feature-<name>`, so `make check-lint-rules` proves the rule fires.
2. **check-deps** (`Makefile`): append `<sdk>` to the four `absent` lists,
   proving that the examples, which do not import the module, do not link
   the SDK.
3. **llm.txt**: in `spec()` in `cmd/gox/main.go`, copy a sibling's line
   (`stripe` for a provider) and rename it; add the module to the module
   lists in `docs/llm/00-intro.md` and in the root package comment
   (`doc.go`); then `make llm`.
4. **Recipe**: `docs/recipes/<task>.md` in the format `docs/AGENTS.md`
   gives, and its line in `docs/recipes/README.md` ending in `[<dir>]`.
5. **Service skill**: a row in the "Per feature" table of
   `cmd/gox/internal/scaffold/_template/base/.claude/skills/gox-add-feature/SKILL.md.tmpl`
   (need, module, recipe, gotchas). Name a new kind of capability in its
   description and in the capability list under "Read first for your task"
   in `cmd/gox/internal/scaffold/_template/base/AGENTS.md.tmpl`. The
   description has little room left under its 1024 characters (`make
   check-agent-docs` counts the raw template): shorten an existing phrase if
   yours does not fit. Generated services copy these files: no versions or
   signatures in them.
6. **ADR**: `docs/decisions/<NNNN>-<slug>.md`, NNNN one above the last of
   `ls docs/decisions/0*.md`, saying why this dependency and this design;
   format in `docs/AGENTS.md`; its row in `docs/decisions/README.md`.
7. **Provider**: a row in `providers/README.md`.
8. **Sweep**: `git grep -l posthog` (a provider;
   `openapi` for a feature, `monetary` for a library) lists every file that
   names a sibling. Add yours wherever the sibling appears in a list of
   modules.

Add no example under `examples/`: those are the canonical mains
`check-deps` builds, and the recipe is the module's example.

## 6. Verify

```bash
make test lint MODULES=./<dir>    # while iterating
make ci                           # what CI runs; must be green
cd <dir> && GOWORK=off go build ./... && GOWORK=off go test ./...
```

`make ci` runs under go.work, so it passes even when the module cannot
build for a consumer. The last line builds against the root tag go.mod
requires. `undefined: gox.<Name>` there, or `does not contain package` from
`GOWORK=off go mod tidy`, means the module uses root API that is not
released: it can merge, but its first release needs a root release first
(`.claude/skills/gox-release/SKILL.md`). Commit go.work.sum if it changed.

## Library

- Step 2 without `-require` and without `<sdk>`.
- Tests in the package itself (`package <name>`), not `<name>_test`: the
  strict `utilities` rule allows only the standard library, not even the
  library's own import path.
- `.golangci.yml`: add `- "!**/<name>/**"` to the `files` of the `root`
  rule and `- "**/<name>/**"` to those of the `utilities` rule.
- No Config, Enable, From, spec line, check-deps entry or service skill
  row. Then step 5.8 (`doc.go` names the libraries) and step 6.

## Gotchas

- `import '<sdk>' is not allowed from list 'root'` on a new feature: the
  `root` exclusion from step 5.1 is missing.
- `... is not allowed from list 'provider-siblings'` (or `providers`, `pkg`,
  `feature-<x>`): the module imports another gox module. Take the value as
  a function argument, or move the shared part to `pkg/`. `provider-siblings`
  also flags a provider importing its own subpackage: keep it one package.
- `missing go.sum entry` under `GOWORK=off`: run `GOWORK=off go mod tidy` in
  the module.
- `llm.txt is stale: run make llm`: a spec line or a doc comment changed.
