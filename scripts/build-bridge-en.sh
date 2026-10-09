#!/usr/bin/env bash
# Build the release archives of bridge-en v$(cat VERSION) into dist/:
#   bridge-en_<version>_<os>_<arch>.tar.gz (windows: .zip), RULEBOOK.md, SHA256SUMS.
# Pure Go (CGO_ENABLED=0), reproducible paths (-trimpath).
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION=$(cat VERSION)
OUT="${1:-dist}"
rm -rf "$OUT"
mkdir -p "$OUT"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  os=${target%/*}
  arch=${target#*/}
  name="bridge-en_${VERSION}_${os}_${arch}"
  dir=$(mktemp -d)
  exe=bridge-en
  [ "$os" = windows ] && exe=bridge-en.exe
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w" -o "$dir/$exe" ./cmd/bridge-en
  cp RULEBOOK.md "$dir/"
  if [ "$os" = windows ]; then
    (cd "$dir" && zip -q "$OLDPWD/$OUT/$name.zip" "$exe" RULEBOOK.md)
  else
    tar -czf "$OUT/$name.tar.gz" -C "$dir" "$exe" RULEBOOK.md
  fi
  rm -rf "$dir"
done
cp RULEBOOK.md "$OUT/"
(cd "$OUT" && sha256sum -- * > SHA256SUMS)
echo "built bridge-en $VERSION into $OUT/"
ls -1 "$OUT"
