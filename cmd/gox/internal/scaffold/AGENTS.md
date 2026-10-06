# Editing the scaffold

Deltas for `gox new`; the root AGENTS.md applies too.

- `_template/base` is always rendered, then `postgres` and `web` when their
  flag is set; a later layer's file replaces one at the same path. Lines that
  differ per variant go in a shared `.tmpl` under `{{if .Postgres}}` or
  `{{if .Web}}`.
- A `.tmpl` file is rendered with text/template over `scaffold.data`
  (`.Name`, `.Module`, `.Prefix`, `.Postgres`, `.Web`, `.GoxDir` and the
  versions) and loses the suffix; every other file is copied as is. `__name__` in a path
  becomes the service name. Write a literal `{{.Dir}}` as `{{ "{{.Dir}}" }}`.
- Everything under `_template` ships in generated services, where agents read it:
  `AGENTS.md.tmpl` is their level 1, `.claude/skills/*/SKILL.md.tmpl` their
  skills. Keep them version-neutral (procedures, rules, symbol and recipe
  names; no defaults or signatures, which `go doc` and `--help` give) and in
  budget: rendered AGENTS.md <= 6144 bytes in every variant, SKILL.md <= 200
  lines. Never name a file `AGENTS.md`, `CLAUDE.md` or `SKILL.md` under
  `_template`: it would load into sessions in this repo
  (`make check-agent-docs` fails). `Render` writes the service's `CLAUDE.md`
  as `@AGENTS.md`.
- The rules in `AGENTS.md.tmpl` mirror `docs/llm/00-intro.md` and the Never
  list in `docs/llm/80-conventions.md`: change both.
- `make generate` also regenerates the `*_templ.go` here, with the templ
  version in web/go.mod; never run a bare `templ`. `TemplVersion` in
  scaffold.go must equal web/go.mod (a test checks).
- Try a change: `go run ./cmd/gox new demo -dir <tmp> -gox-dir . -postgres -web`
  with `<tmp>` outside this repo (go.work would capture it), then
  `make test lint` there.
- Done: `go test ./cmd/gox/...` without `-short` and with golangci-lint on
  PATH (or it skips lint). It renders plain, web and postgres+web services,
  then builds, vets, tests, lints and starts them (postgres+web starts only
  with DATABASE_URL).
