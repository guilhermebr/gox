#!/usr/bin/env bash
# check-deps.sh: prove that an example binary does (or does not) link a module.
#
# Usage:
#   scripts/check-deps.sh <example-dir> absent  <module-path>...
#   scripts/check-deps.sh <example-dir> present <module-path>...
#
# Builds the example and inspects `go version -m` on the binary. This is the
# ground truth for the import-as-opt-in rule: a service that does not import
# gox/postgres must not carry pgx in its binary, and one that does must.
set -euo pipefail

if [ $# -lt 3 ]; then
	echo "usage: $0 <example-dir> absent|present <module-path>..." >&2
	exit 2
fi

dir=$1
mode=$2
shift 2

case "$mode" in
absent | present) ;;
*)
	echo "mode must be 'absent' or 'present', got '$mode'" >&2
	exit 2
	;;
esac

out=$(mktemp)
trap 'rm -f "$out"' EXIT

(cd "$dir" && go build -o "$out" .)
deps=$(go version -m "$out")

fail=0
for mod in "$@"; do
	if grep -qE "^\s+dep\s+$mod(\s|/)" <<<"$deps"; then
		found=1
	else
		found=0
	fi
	case "$mode" in
	absent)
		if [ "$found" = 1 ]; then
			echo "FAIL: $dir links $mod but must not" >&2
			fail=1
		else
			echo "ok: $dir does not link $mod"
		fi
		;;
	present)
		if [ "$found" = 0 ]; then
			echo "FAIL: $dir does not link $mod but must" >&2
			fail=1
		else
			echo "ok: $dir links $mod"
		fi
		;;
	esac
done

exit $fail
