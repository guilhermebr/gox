#!/usr/bin/env bash
# check-lint-rules.sh: prove the depguard dependency rules actually fire.
#
# A depguard rule whose file glob silently stops matching reports nothing and
# looks green. This script plants one deliberate violation for every rule in
# .golangci.yml (a new rule gets a probe line below), runs golangci-lint on it
# ($GOLANGCI_LINT, default golangci-lint) and requires the finding.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
lint=${GOLANGCI_LINT:-golangci-lint}
fixture=
fail=0

# The fixtures import other modules of the repository: only go.work resolves them.
export GOWORK="$root/go.work"

if ! $lint version >/dev/null 2>&1; then
	echo "FAIL: cannot run '$lint': install golangci-lint (AGENTS.md, Done) or set GOLANGCI_LINT" >&2
	exit 2
fi

cleanup() { if [ -n "$fixture" ]; then rm -rf "$fixture"; fi; }
trap cleanup EXIT

# probe <fixture dir> <import> <list> <what>: plant a package at <fixture dir>
# (relative to the repository root) that imports <import>, lint it from its
# module, and require depguard to report the import from <list>.
probe() {
	local rel=$1 imp=$2 list=$3 what=$4
	fixture="$root/$rel"
	if [ -e "$fixture" ]; then
		echo "fixture dir $fixture already exists; refusing to overwrite" >&2
		fixture=
		exit 2
	fi
	local mod
	mod=$(dirname "$fixture")
	while [ ! -f "$mod/go.mod" ] && [ "$mod" != / ]; do mod=$(dirname "$mod"); done
	mkdir -p "$fixture"
	cat >"$fixture/probe.go" <<EOF
// Package zz_depguard_probe is a temporary fixture planted by scripts/check-lint-rules.sh.
package zz_depguard_probe

import _ "$imp" // deliberate violation: $what
EOF
	local out
	# $lint is split into words, like $(GOLANGCI_LINT) in the Makefile.
	out=$(cd "$mod" && $lint run --config "$root/.golangci.yml" "./${fixture#"$mod"/}/..." 2>&1 || true)
	rm -rf "$fixture"
	fixture=
	if grep -qF "import '$imp' is not allowed from list '$list'" <<<"$out"; then
		echo "ok: depguard list '$list' fires on $what"
		return
	fi
	echo "FAIL: depguard did not flag $what ($rel imports $imp); fix list '$list' in .golangci.yml. Output:" >&2
	echo "$out" >&2
	fail=1
}

probe pkg/zz_depguard_probe github.com/guilhermebr/gox pkg "pkg/* -> root"
probe pkg/zz_depguard_probe github.com/guilhermebr/gox/providers/stripe pkg "pkg/* -> provider"
probe jobs/zz_depguard_probe github.com/guilhermebr/gox/postgres feature-jobs "feature -> feature"
probe jobs/zz_depguard_probe github.com/guilhermebr/gox/providers/stripe feature-jobs "feature -> provider"
probe postgres/zz_depguard_probe github.com/guilhermebr/gox/jobs feature-postgres "postgres -> feature"
probe openapi/zz_depguard_probe github.com/guilhermebr/gox/jwt feature-openapi "openapi -> feature"
probe jwt/zz_depguard_probe github.com/guilhermebr/gox/web feature-jwt "jwt -> feature"
probe web/zz_depguard_probe github.com/guilhermebr/gox/postgres feature-web "web -> feature"
probe providers/stripe/zz_depguard_probe github.com/guilhermebr/gox/postgres providers "provider -> feature"
probe providers/stripe/zz_depguard_probe github.com/guilhermebr/gox/providers/posthog provider-siblings "provider -> provider"
probe cmd/zz_depguard_probe github.com/ardanlabs/conf/v3 cmd "cmd -> third-party"
probe monetary/zz_depguard_probe github.com/guilhermebr/gox utilities "utility -> root"
probe zz_depguard_probe github.com/guilhermebr/gox/jobs root "an unclassified top-level directory"

exit $fail
