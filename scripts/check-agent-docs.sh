#!/usr/bin/env bash
# check-agent-docs.sh: keep the files coding agents load small, in the right
# place and pointing at files that exist.
#
# Agents load AGENTS.md on every task, a nested AGENTS.md when they touch its
# directory and a skill body when its description matches, then follow the
# paths that text names. This script fails when one of those files outgrows
# its budget or sits where it would load into the wrong session, when a
# CLAUDE.md exists (it can stop AGENTS.md from loading), when a path it names
# does not exist, when the recipe or ADR index and its directory disagree, or
# when a skill's name differs from its directory or its one-line description
# is missing, over 1024 characters or not a plain YAML scalar, or when the
# golangci-lint version AGENTS.md names differs from the CI pin.
#
# A path is a name with a / that ends in .md or .md.tmpl (a <placeholder> is
# not one), resolved from the repo root or from the naming file's directory.
# A template file (under the scaffold _template) names service paths too:
# those resolve against any template layer, with or without .tmpl, and a
# .claude/ path resolves only there. A leading / or $VAR stands for the gox
# module dir, which is this repo.
set -euo pipefail
shopt -s nullglob

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
tpl=cmd/gox/internal/scaffold/_template

fail=0
bad() {
	echo "FAIL: $*" >&2
	fail=1
}

# files EXPR...: files matching a find expression, outside .git and the
# checkouts Claude Code makes in .claude/worktrees, without ./.
files() { find . -not -path './.git/*' -not -path './.claude/worktrees/*' \( "$@" \) | sed 's#^\./##' | sort; }

# mdrefs FILE: every name ending in .md or .md.tmpl that FILE mentions, URLs
# and paths with a <placeholder> aside.
mdrefs() {
	sed -E 's#[a-z]+://[^[:space:]]*##g; s#\$[A-Za-z_][A-Za-z0-9_]*##g; s#<[A-Za-z0-9_-]*>[A-Za-z0-9_./-]*##g' "$1" |
		grep -oE '[A-Za-z0-9_./-]*[A-Za-z0-9_-][.]md([.]?[A-Za-z0-9_]+)?' |
		grep -E '[.]md([.]tmpl)?$' | sed 's#^/*##' | sort -u || true
}

# resolves FILE REF: REF names a file that exists (see the header).
resolves() {
	local file=$1 ref=$2 layer
	case $file in
	"$tpl"/*)
		for layer in "$tpl"/*; do
			if [ -e "$layer/$ref" ] || [ -e "$layer/$ref.tmpl" ]; then return 0; fi
		done
		case $ref in .claude/*) return 1 ;; esac
		;;
	esac
	[ -e "$ref" ] || [ -e "$(dirname "$file")/$ref" ]
}

# Budgets: the root AGENTS.md loads on every task, a nested one whenever a
# file in its directory is read.
for f in $(files -name AGENTS.md); do
	if [ "$f" = AGENTS.md ]; then max=4096; else max=2048; fi
	size=$(wc -c <"$f" | tr -d ' ')
	[ "$size" -le "$max" ] || bad "$f is $size bytes, over its $max-byte budget: move task-specific text into a skill or a scoped AGENTS.md and point to it"
done

# Placement: Claude Code picks these files up from the directories it reads.
for f in $(files -path "./$tpl/*" \( -name AGENTS.md -o -name SKILL.md \)); do
	bad "$f can load into sessions that work on gox itself: rename it to $(basename "$f").tmpl"
done
for f in $(files -name CLAUDE.md); do
	bad "$f can stop Claude Code from loading AGENTS.md files: delete it and move its text into an AGENTS.md"
done

# Pointers. A bare name such as CLAUDE.md is a name, not a pointer.
for f in $(files -name AGENTS.md -o -name AGENTS.md.tmpl -o -name SKILL.md -o -name SKILL.md.tmpl); do
	for ref in $(mdrefs "$f"); do
		case $ref in */*) ;; *) continue ;; esac
		resolves "$f" "$ref" || bad "$f names $ref, which does not exist: fix the path, or name a file the reader will create with a placeholder such as docs/recipes/<task>.md"
	done
done

# Indexes: agents find recipes and ADRs through the README of their directory.
index() {
	local dir=$1 readme=$1/README.md f listed
	if [ ! -f "$readme" ]; then
		bad "$readme is missing: create it with one line per file in $dir"
		return
	fi
	listed=$(mdrefs "$readme" | sed 's#.*/##')
	for f in "$dir"/$2; do
		[ "$f" != "$readme" ] || continue
		grep -qxF "${f##*/}" <<<"$listed" || bad "$f is not in $readme: add a line for it"
	done
	for f in $(mdrefs "$readme"); do
		resolves "$readme" "$f" || bad "$readme names $f, which does not exist: fix or remove that line"
	done
}
index docs/recipes '*.md'
index docs/decisions '0*.md'

# Skills: the description alone decides whether the body loads, and the
# name must match the directory.
for f in $(files -name SKILL.md -o -name SKILL.md.tmpl); do
	lines=$(wc -l <"$f" | tr -d ' ')
	[ "$lines" -le 200 ] || bad "$f is $lines lines, over the 200-line budget: point to a recipe or doc instead of copying it"
	dir=$(basename "$(dirname "$f")")
	front=$(awk 'NR == 1 { if ($0 != "---") exit; next } $0 == "---" { exit } { print }' "$f")
	name=$(sed -n 's/^name: //p' <<<"$front")
	desc=$(sed -n 's/^description: //p' <<<"$front")
	[ "$name" = "$dir" ] || bad "$f: frontmatter name is '$name': set 'name: $dir' between --- lines at the top"
	[ -n "$desc" ] || bad "$f: frontmatter has no one-line description: add one that says what the skill does and when to use it"
	[ "${#desc}" -le 1024 ] || bad "$f: description is ${#desc} characters: cut it to 1024"
	case $desc in *': '* | *' #'*) bad "$f: description contains ': ' or ' #', which breaks the plain YAML scalar: reword it" ;; esac
done

# Lint version: AGENTS.md tells agents which golangci-lint to install; CI's
# pin is the source of truth.
pin=$(awk '/GOLANGCI_LINT_VERSION:/ { print $2; exit }' .github/workflows/ci.yml)
[ -n "$pin" ] || bad ".github/workflows/ci.yml has no GOLANGCI_LINT_VERSION: restore it or update this check"
for v in $(grep -oE 'golangci-lint[@ ]v[0-9][0-9.]*' AGENTS.md | sed -E 's/.*[@ ]//; s/[.]+$//' | sort -u); do
	[ "$v" = "$pin" ] || bad "AGENTS.md names golangci-lint $v but .github/workflows/ci.yml pins $pin: make them agree"
done

[ "$fail" = 1 ] || echo "ok: agent docs are within budget and in place, and every path and index entry they name exists"
exit $fail
