---
name: gox-release
description: Release modules of the gox repository by tagging the root (vX.Y.Z) or a nested module (<module>/vX.Y.Z). Covers dropping a module's local replace, pointing a module at a newer root version so it builds outside go.work, choosing version numbers, the root patch release that ships changed recipes, and confirming with proxy.golang.org. Use it whenever a task says release, tag, publish, cut or bump a version, make a module installable with go get @version, or require a newer gox root in one of this repo's modules, even if the word release is not used. It stops before every git push. Not for adding a new module (use gox-add-module) or for updating third-party dependencies.
---

# Release gox modules

Each module is versioned on its own. The root is tagged `vX.Y.Z`; a nested
module is tagged with its directory, `<dir>/vX.Y.Z` (`jwt/v0.1.0`,
`providers/workos/v0.1.2`). Why: docs/decisions/0000-module-layout.md.

Two facts drive every step:

- go.work builds every module against the working-tree root, so `make ci`
  cannot tell whether a tag works. The release checks run with `GOWORK=off`,
  which builds a module the way a consumer does: against the root version its
  go.mod requires.
- A pushed tag is permanent. proxy.golang.org and sum.golang.org keep the
  first content they see for a version, so never move, delete or reuse a
  pushed tag. Check before tagging; fix a broken release forward with the next
  patch.

## 1. Read the state

Release state lives in git tags and go.mod files. Never record it in
AGENTS.md or any other doc; a list there goes stale, the tags do not.

```bash
git fetch --tags origin
git tag -l --sort=-v:refname                        # every release; root tags are plain vX.Y.Z
git tag -l 'v*' --sort=-v:refname | head -1         # newest root tag
git tag -l '<dir>/v*' --sort=-v:refname | head -1   # newest tag of one module; empty: never released
git grep -l '^replace github.com/guilhermebr/gox' -- '*go.mod'   # never released, of the modules that require gox
```

- Only tags on origin count: a tag that `git ls-remote --tags origin` does
  not list was never pushed.
- A go.mod that replaces github.com/guilhermebr/gox with a local path has
  never been released. examples/ is the exception: it is never released and
  keeps its replaces.
- A go.mod that does not require github.com/guilhermebr/gox at all needs no
  edit: run the checks in step 3 and tag.
- What a release would ship: `git log --oneline <dir>/vX.Y.Z..HEAD -- <dir>`
  for a module (never released: `git log --oneline -- <dir>`). For the root,
  everything outside the nested modules:
  `git log --oneline vX.Y.Z..HEAD -- . $(git ls-files '*/go.mod' | sed 's|/go.mod$||; s|^|:(exclude)|')`

## 2. Choose the versions

- A module's first release is `v0.1.0`.
- `feat` commits since the newest tag (new API): bump the minor. Only fixes,
  docs or refactors: bump the patch.
- Never tag `v1.0.0` or a new major unless the user asks: v1 is a
  compatibility promise and v2+ changes the module path.
- If the task names no version, pick one by these rules and confirm it when
  you ask to push (step 5). Until then a tag is local: `git tag -d` undoes it.

## 3. Release a nested module

Start on main, up to date and green: `git switch main && git pull --ff-only`,
then `make ci`. Tag nothing while it fails. In the module directory:

```bash
go mod edit -dropreplace=github.com/guilhermebr/gox -require=github.com/guilhermebr/gox@vX.Y.Z
GOWORK=off go mod tidy
GOWORK=off go build ./... && GOWORK=off go test ./...
git diff -- .   # expect only gox lines (require, replace, go.sum) and versions the newer root raised
```

- `vX.Y.Z` is the newest root tag. `-dropreplace` is a no-op when there is no
  replace, so the same commands bump a released module.
- Skipping the tidy fails the build with `missing go.sum entry ... go mod
  download github.com/guilhermebr/gox`. Tidy only this module; `make tidy`
  tidies all of them.
- Raise a released module's root requirement only when it needs newer root
  API; consumers already get the newest root they require (minimal version
  selection). A release with no go.mod change needs only the two checks and a
  tag.

If the build fails on root API (`undefined: gox.<Name>`, or
`undefined: lifecycle.<Name>` from a root `pkg/` package), or tidy with `module
github.com/guilhermebr/gox@latest found (vX.Y.Z), but does not contain
package github.com/guilhermebr/gox/pkg/...`, the module uses root API that no
root tag has yet. go.work hid it, so `make ci` passed anyway. Undo the
edit (`git checkout -- go.mod go.sum`), release the root (step 4, a minor
bump), push its tag (step 5) and confirm it (step 6), then rerun the commands
above with the new root version. The order is forced: tidy downloads the root
through the proxy, so it fails with `unknown revision vX.Y.Z` until the root
tag is pushed, and the proxy caches a request made too early for up to 30
minutes.

Commit and tag in the house style (one commit per module, annotated tag whose
message is the tag name):

```bash
git add go.mod go.sum
git commit -m 'build(<dir>): depend on the released root module vX.Y.Z'
git tag -a <dir>/vA.B.C -m <dir>/vA.B.C
```

## 4. Release the root

Same start as step 3 (main up to date, `make ci` green), then in the
repository root:

```bash
GOWORK=off go build ./... && GOWORK=off go test ./...   # the root module alone
git tag -a vX.Y.Z -m vX.Y.Z
```

Recipes ship with the root. The root module is everything outside the nested
modules: the root package, pkg/*, cmd/gox (what
`go install github.com/guilhermebr/gox/cmd/gox@latest` installs, scaffold
included), docs/recipes and llm.txt. A service reads the recipes of the root
version it requires, so when a recipe or llm.txt changed since the newest root
tag (a module fix that changed its recipe, say), cut a root patch release too:

```bash
git diff --stat "$(git tag -l 'v*' --sort=-v:refname | head -1)"..HEAD -- docs/recipes llm.txt
```

## 5. Stop and ask before pushing

Never run `git push` without the user's explicit yes in this conversation.
Show what would go out (`git log --oneline origin/main..main` and the tags
you created, with the versions you picked) and wait. On yes, push the branch
and the tags in one atomic push, so a rejected branch never leaves a tag on a
commit main does not have:

```bash
git push --atomic origin main <dir>/vA.B.C vX.Y.Z
```

- A rejected push sent nothing: `git pull --rebase`, rerun the checks, move
  each tag to the new commit (`git tag -d`, tag again) and ask again.
- Root first (step 3) means two rounds: push and confirm the root tag, then
  release the module and ask again.
- If main only accepts pull requests, merge the release commit first and tag
  the merged commit; a tag on a commit that is later squashed points outside
  main.

## 6. Confirm

After the push, ask the proxy for each version you pushed, then resolve it
as a consumer would, outside this repository:

```bash
GOPROXY=https://proxy.golang.org go list -m github.com/guilhermebr/gox/<dir>@vA.B.C   # root: github.com/guilhermebr/gox@vX.Y.Z
(cd "$(mktemp -d)" && go mod init example.com/probe && go get github.com/guilhermebr/gox/<dir>@vA.B.C)
```

The first request makes the proxy fetch the tag; allow about a minute
(`go list -m -versions` lags longer). If a pushed version is broken, release
the next patch with a `retract` directive for the broken one in the module's
go.mod.
