#!/usr/bin/env bash
# check-recipes.sh: every recipe in docs/recipes must build.
#
# A recipe is a Markdown file whose fenced code blocks carry a path, for
# example ```go path=main.go or ```templ path=views/home.templ. This script
# extracts them into examples/recipes_build/<recipe>/, rewrites the
# placeholder module path example.com/shop to the build location, runs
# templ generate when needed, and vets the result inside the examples
# module (which replaces every gox module with the working tree).
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
build="$root/examples/recipes_build"
templ_version=$(grep -E 'github.com/a-h/templ ' "$root/web/go.mod" | awk '{print $2}')
rm -rf "$build"
mkdir -p "$build"

fail=0
for recipe in "$root"/docs/recipes/*.md; do
	name=$(basename "$recipe" .md)
	dir="$build/$name"
	mkdir -p "$dir"
	python3 - "$recipe" "$dir" "$name" <<'EOF'
import re, sys, os
src, dir, name = sys.argv[1], sys.argv[2], sys.argv[3]
text = open(src).read()
module = "github.com/guilhermebr/gox/examples/recipes_build/" + name
blocks = re.findall(r"^```(\w+)\s+path=(\S+)\n(.*?)^```", text, re.S | re.M)
if not blocks:
    print(f"{name}: no fenced block with path=", file=sys.stderr)
    sys.exit(1)
for lang, path, body in blocks:
    body = body.replace("example.com/shop", module)
    full = os.path.join(dir, path)
    os.makedirs(os.path.dirname(full), exist_ok=True)
    open(full, "w").write(body)
EOF
	if find "$dir" -name '*.templ' | grep -q .; then
		(cd "$root/examples" && go run "github.com/a-h/templ/cmd/templ@$templ_version" generate -path "$dir" >/dev/null)
	fi
	if (cd "$root/examples" && go vet "./recipes_build/$name/..." 2>"$dir/vet.log"); then
		echo "ok: recipe $name builds"
	else
		echo "FAIL: recipe $name" >&2
		cat "$dir/vet.log" >&2
		fail=1
	fi
done
rm -rf "$build"
exit $fail
