#!/usr/bin/env bash
# Prove a brand-new app depends on bridge-en like any Go module, imports its
# runtime, and copies no bridge-en source:
#   1. install the bridge-en binary (default: built from this checkout, as a
#      release binary is; --go-install: setup-bridge-en's install.sh, which
#      reads the version from the app's go.mod and runs
#      `go install github.com/pierre10101/go-ai-bridge/cmd/bridge-en@v<version>`
#      against a throw-away tag on a local mirror of HEAD)
#   2. a new module requires github.com/pierre10101/go-ai-bridge v$(cat VERSION)
#      (default: with a replace to this checkout; --go-install: `go get` from
#      the mirror) and gets the fixture app (adapter/testdata) as its own code:
#      schema, internal/domain, cmd/server and good/* as features/*
#   3. sqlc generates the slices' db packages; gofmt, go vet and go test ./...
#      pass, so the slices' checks run for real against SQLite
#   4. bridge-en -check passes and renders every slice exactly as the source
#      fixture's golden English
#   5. a go.mod that pins another bridge-en version is refused
#   6. `bridge-en init` in the brand-new app writes AGENTS.md and the agent
#      pointer files (docs only, no Go code) and never overwrites without -force
#   7. `bridge-en pr-comment` and the pr-english action script (dry run) show a
#      changed feature's intent next to its English diff
set -euo pipefail
cd "$(dirname "$0")/.."
SRC=$(pwd)
VERSION=$(cat VERSION)
MODULE=github.com/pierre10101/go-ai-bridge
SQLC=$(command -v sqlc || echo "$(go env GOPATH)/bin/sqlc")
[ -x "$SQLC" ] || { echo "sqlc not installed: go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1" >&2; exit 1; }
WORK=$(mktemp -d)
trap 'chmod -R u+w "$WORK" 2>/dev/null; rm -rf "$WORK"' EXIT
BIN="$WORK/bin"
mkdir -p "$BIN"
MODE=replace
[ "${1:-}" = "--go-install" ] && MODE=go-install

if [ "$MODE" = go-install ]; then
  echo "== local mirror of HEAD tagged v$VERSION (stands in for github.com/pierre10101/go-ai-bridge)"
  git clone -q --bare "$SRC" "$WORK/src.git"
  git -C "$WORK/src.git" tag "v$VERSION" HEAD
  export HOME="$WORK/home" GOPATH="$WORK/gopath" GOMODCACHE="$WORK/gopath/pkg/mod" GOFLAGS=-modcacherw
  export GOPRIVATE="$MODULE" # fetch it with git, not from the proxy or checksum database
  export GIT_CONFIG_GLOBAL="$WORK/gitconfig" GIT_ALLOW_PROTOCOL="file:https" GIT_TERMINAL_PROMPT=0
  git config --global url."file://$WORK/src.git".insteadOf "https://$MODULE"
else
  echo "== build bridge-en $VERSION from this checkout"
  CGO_ENABLED=0 go build -trimpath -o "$BIN/bridge-en" ./cmd/bridge-en
fi
export PATH="$BIN:$PATH"

APP="$WORK/seat-app"
FIX="$SRC/adapter/testdata"
echo "== new app example.com/seat-app requires $MODULE v$VERSION ($MODE)"
mkdir -p "$APP/features" "$APP/internal"
cd "$APP"
go mod init example.com/seat-app >/dev/null 2>&1
if [ "$MODE" = go-install ]; then
  go get "$MODULE@v$VERSION" >/dev/null 2>&1
else
  go mod edit -require="$MODULE@v$VERSION" -replace="$MODULE=$SRC"
fi
cp "$FIX/schema.sql" "$FIX/schema.go" .
cp -r "$FIX/internal/domain" internal/
cp -r "$FIX/cmd" .
for slice in "$FIX"/good/*/; do
  name=$(basename "$slice")
  cp -r "$slice" "features/$name"
  cat >> sqlc.yaml.parts <<YAML
  - engine: "sqlite"
    schema: "schema.sql"
    queries: "features/$name/queries"
    gen:
      go:
        package: "db"
        out: "features/$name/db"
YAML
done
{ echo 'version: "2"'; echo 'sql:'; cat sqlc.yaml.parts; } > sqlc.yaml
rm sqlc.yaml.parts
grep -rl 'example.com/fixtures' --include=*.go . | xargs sed -i 's#example\.com/fixtures#example.com/seat-app#g'

echo "== install bridge-en"
if [ "$MODE" = go-install ]; then
  # No BRIDGE_EN_VERSION: the version comes from the app's go.mod.
  BRIDGE_EN_BIN_DIR="$BIN" "$SRC/.github/actions/setup-bridge-en/install.sh"
fi
[ "$(bridge-en -version)" = "bridge-en $VERSION" ]

echo "== bridge-en init in the brand-new app: docs only, no Go code"
INIT="$WORK/init-app"
mkdir -p "$INIT" && cp go.mod "$INIT/"
(cd "$INIT" && bridge-en init)
got=$(cd "$INIT" && find . -type f ! -name go.mod | sed 's#^\./##' | LC_ALL=C sort | tr '\n' ' ')
want=".cursor/rules/bridge-en.mdc .github/copilot-instructions.md AGENTS.md CLAUDE.md GEMINI.md "
[ "$got" = "$want" ] || { echo "init wrote: $got; want: $want" >&2; exit 1; }
if find "$INIT" -name '*.go' | grep -q .; then echo "init wrote Go code" >&2; exit 1; fi
grep -q "go install github.com/pierre10101/go-ai-bridge/cmd/bridge-en@v$VERSION" "$INIT/AGENTS.md"
for f in .cursor/rules/bridge-en.mdc CLAUDE.md .github/copilot-instructions.md GEMINI.md; do
  grep -q "Follow AGENTS.md" "$INIT/$f" || { echo "$f does not point to AGENTS.md" >&2; exit 1; }
done
echo "my notes" > "$INIT/CLAUDE.md"
(cd "$INIT" && bridge-en init) | grep -q "kept      CLAUDE.md"
[ "$(cat "$INIT/CLAUDE.md")" = "my notes" ] || { echo "init overwrote CLAUDE.md without -force" >&2; exit 1; }
(cd "$INIT" && bridge-en init -force) | grep -q "overwrote CLAUDE.md"
grep -q "Follow AGENTS.md" "$INIT/CLAUDE.md"
echo "ok  init wrote $want(no Go code; kept existing files without -force)"
bridge-en init . >/dev/null   # the app itself gets the docs too

echo "== sqlc generate, go mod tidy"
"$SQLC" generate
go mod tidy >/dev/null 2>&1
git init -q . # gofmt/vet see a normal checkout

echo "== the app copies no bridge-en source; it imports the runtime"
if [ -d runtime ] || [ -d internal/httpx ] || [ -d adapter ]; then
  echo "bridge-en source was copied into the app" >&2; exit 1
fi
if grep -rqE '^package (httpx|txn|store|page|shape|assert|failure)$' --include=*.go .; then
  echo "an app package re-declares a bridge-en runtime package" >&2; exit 1
fi
deps=$(go list -deps ./...)
for pkg in httpx txn store page shape assert failure; do
  grep -qx "$MODULE/runtime/$pkg" <<<"$deps" || { echo "app does not import $MODULE/runtime/$pkg" >&2; exit 1; }
done
grep -q "^require $MODULE v$VERSION\$" go.mod || grep -q "^	$MODULE v$VERSION\$" go.mod
echo "ok  imports $MODULE/runtime/{httpx,txn,store,page,shape,assert,failure} v$VERSION"

echo "== gofmt, go vet, go test ./..."
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then echo "not gofmt'd:"; echo "$unformatted"; exit 1; fi
go vet ./...
go test ./...

echo "== bridge-en -check features/*/"
bridge-en -check features/*/

echo "== same English as the source fixtures"
for slice in "$FIX"/good/*/; do
  name=$(basename "$slice")
  bridge-en "features/$name" > "$WORK/$name.en"
  diff -u "$slice/$name.en" "$WORK/$name.en"
  echo "ok  features/$name renders exactly as adapter/testdata/good/$name/$name.en"
done

echo "== a go.mod that pins another bridge-en is refused"
cp go.mod "$WORK/go.mod.keep"
go mod edit -require="$MODULE@v0.0.0-skew"
if bridge-en -check features/claim_example/ 2>"$WORK/err"; then
  echo "version skew was not refused" >&2; exit 1
fi
cat "$WORK/err"
cp "$WORK/go.mod.keep" go.mod

echo "== pr-comment: a changed feature's intent next to its English"
git add -A >/dev/null && git -c user.name=smoke -c user.email=smoke@example.com commit -qm base
BASE=$(git rev-parse HEAD)
sed -i 's/^- F2: there is no valid session cookie (the session is 0): nothing is$/- F2: there is no valid session cookie (no cookie at all): nothing is/' features/claim_example/intent.md
grep -q '(no cookie at all)' features/claim_example/intent.md
sed -i 's/^# Claim example$/# Claim example (edited for the smoke test)/' features/claim_example/claim_example.en
git add -A >/dev/null && git -c user.name=smoke -c user.email=smoke@example.com commit -qm change
bridge-en pr-comment -base "$BASE" features/*/ > "$WORK/comment.md"
head -n1 "$WORK/comment.md" | grep -qx '<!-- bridge-en:pr-english -->'
grep -q '^### `features/claim_example`$' "$WORK/comment.md"
grep -q '^+- F2: there is no valid session cookie (no cookie at all): nothing is$' "$WORK/comment.md"
grep -q '^+# Claim example (edited for the smoke test)$' "$WORK/comment.md"
if grep -q 'features/create_invoice' "$WORK/comment.md"; then echo "unchanged feature in the comment" >&2; exit 1; fi
BRIDGE_EN_DRY_RUN=1 BRIDGE_EN_BASE="$BASE" BRIDGE_EN_FEATURES="features/*/" BRIDGE_EN_MAX=60000 \
  "$SRC/.github/actions/pr-english/post.sh" > "$WORK/comment2.md"
diff -u "$WORK/comment.md" "$WORK/comment2.md"
echo "ok  pr-comment and pr-english/post.sh (dry run): $(wc -c < "$WORK/comment.md") chars"

echo "smoke-app green: bridge-en $VERSION ($MODE)"
