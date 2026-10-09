#!/usr/bin/env bash
# What CI runs. Any failure blocks the merge. Run locally: ./scripts/ci.sh
# The tool ships no app: the only slices are the adapter fixtures under
# adapter/testdata (a parse-only fixture app, see adapter/testdata/go.mod).
# scripts/smoke-app.sh builds them into a real app and runs their checks.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== no references to the old module name (incl. hidden dirs like .github)"
old='pierre10101/'"bridge-en"
if git grep -n -I -e "$old" -- . ':!.git' >/dev/null 2>&1 || grep -rnI --exclude-dir=.git -e "$old" . >/dev/null; then
  echo "old module path $old found:"; grep -rnI --exclude-dir=.git -e "$old" . || true; exit 1
fi

echo "== gofmt"
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then echo "not gofmt'd:"; echo "$unformatted"; exit 1; fi

echo "== go vet"
go vet ./...

echo "== bridge-en -check on the good fixtures (pin, refusals, golden .en, F-ID cross-checks)"
go run ./cmd/bridge-en -check adapter/testdata/good/*/

echo "== bridge-en binary (go build, version = VERSION)"
mkdir -p bin
go build -o bin/bridge-en ./cmd/bridge-en
[ "$(bin/bridge-en -version)" = "bridge-en $(cat VERSION)" ] || { echo "bridge-en -version does not match VERSION" >&2; exit 1; }

echo "== go test"
go test ./...

echo "CI green"
