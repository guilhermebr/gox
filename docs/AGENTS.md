# Editing docs

Deltas for `docs/`; the root AGENTS.md applies too.

## Who reads what

- `docs/llm/*.md` → `llm.txt`: `make llm` joins every `*.md` there (names
  below `50` before the API, the rest after) with the API and variables of
  the packages listed in `cmd/gox/main.go`. Put nothing else there.
- Doc comments → `go doc`. `llm.txt` keeps what `spec()` selects: signature
  and first sentence (of `Deprecated:` if any). `conf` `help:` → `--help`, `llm.txt`.
- Rules in `docs/llm/00-intro.md` and `80-conventions.md` that a service can
  break are mirrored in
  `cmd/gox/internal/scaffold/_template/base/AGENTS.md.tmpl`: change both.
- `docs/recipes/` → agents in generated services, through
  `docs/recipes/README.md` and the service skills.
- `docs/decisions/` → contributors, through `docs/decisions/README.md`.
- `guide.md` → humans; `features.md` → humans and the gox-add-module skill.

## Recipes

- One task per file, named for the outcome. Never rename one: skills and the
  index name it.
- A block that must build is a fence at column 0 ending in its path:
  ```` ```go path=main.go ````. A fence without `path=` is not checked. An
  indented fence, or text after the path, is skipped silently.
- `make check-recipes` writes each recipe's blocks into one directory, runs
  templ generate if needed, then `go vet`: tests compile, never run. Each
  `func main` needs its own directory.
- Import the recipe's own packages as `example.com/shop/...`, the only module
  path the checker rewrites. The service is `shop` or `billing`.
- List it in `docs/recipes/README.md`, then run `make check-recipes`.

## ADRs

- `docs/decisions/<NNNN>-<slug>.md`, the next free number, shaped like the
  newest ADR: `# NNNN — Title`, `Date:`, `Status: accepted`, then Context,
  Decision and Consequences sections.
- Add its row to `docs/decisions/README.md`. An ADR that changes an earlier
  decision also gets a note in the earlier one's row, and the earlier ADR an
  `Amended:` line under `Status:` (copy one).
