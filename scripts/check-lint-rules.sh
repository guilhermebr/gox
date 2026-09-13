#!/usr/bin/env bash
# check-lint-rules.sh: prove the depguard dependency rules actually fire.
#
# A depguard rule whose file glob silently stops matching reports nothing and
# looks green. This script plants a deliberate violation (a pkg/* package
# importing the root package), runs golangci-lint, and requires the finding.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
fixture="$root/pkg/zz_depguard_probe"

cleanup() { rm -rf "$fixture"; }
trap cleanup EXIT

if [ -e "$fixture" ]; then
	echo "fixture dir $fixture already exists; refusing to overwrite" >&2
	exit 2
fi

mkdir -p "$fixture"
cat >"$fixture/probe.go" <<'EOF'
// Package zz_depguard_probe is a temporary fixture planted by scripts/check-lint-rules.sh.
package zz_depguard_probe

import _ "github.com/guilhermebr/gox" // deliberate violation: pkg/* must not import the root
EOF

out=$(cd "$root" && golangci-lint run --config "$root/.golangci.yml" ./pkg/zz_depguard_probe/... 2>&1 || true)

if grep -q "is not allowed from list 'pkg'" <<<"$out"; then
	echo "ok: depguard pkg rule fires on a pkg/* -> root import"
	exit 0
fi

echo "FAIL: depguard did not flag pkg/* importing the root package. Output:" >&2
echo "$out" >&2
exit 1
