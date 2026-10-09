#!/usr/bin/env bash
# Install one pinned bridge-en, the way any app (or person) does it. Run it
# from the app's module root (the directory with go.mod):
#   ./install.sh                                    # version from go.mod, go install
#   BRIDGE_EN_VERSION=0.1.0 ./install.sh            # go install ...@v0.1.0
#   BRIDGE_EN_METHOD=release ./install.sh           # released binary + SHA256 check
# The version defaults to the one the app's go.mod requires
# (`go list -m github.com/pierre10101/go-ai-bridge`), so the binary always matches
# the runtime the app imports. Installs into $BRIDGE_EN_BIN_DIR (default
# $HOME/.local/bin) and checks `bridge-en -version`.
set -euo pipefail

REPO="pierre10101/go-ai-bridge"
MODULE="github.com/pierre10101/go-ai-bridge"
VERSION="${BRIDGE_EN_VERSION:-}"
if [ -z "$VERSION" ] && command -v go >/dev/null 2>&1; then
  VERSION=$(go list -m -f '{{.Version}}' "$MODULE" 2>/dev/null || true)
fi
if [ -z "$VERSION" ] && [ -f go.mod ]; then # no go on PATH (release method)
  VERSION=$(awk -v m="$MODULE" '$1 == "require" && $2 == m { print $3 } $1 == m { print $2 }' go.mod | head -n1)
fi
VERSION="${VERSION#v}"
if [ -z "$VERSION" ]; then
  echo "setup-bridge-en: no version: pass one, or run from a module whose go.mod requires $MODULE (go get $MODULE@v<version>)" >&2
  exit 1
fi
BIN="${BRIDGE_EN_BIN_DIR:-$HOME/.local/bin}"
mkdir -p "$BIN"

case "${BRIDGE_EN_METHOD:-go-install}" in
  go-install)
    # The Go module proxy and checksum database pin the tagged source.
    GOBIN="$BIN" go install "${MODULE}/cmd/bridge-en@v${VERSION}"
    ;;
  release)
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    case "$(uname -m)" in
      x86_64 | amd64) arch=amd64 ;;
      arm64 | aarch64) arch=arm64 ;;
      *) echo "setup-bridge-en: unsupported architecture $(uname -m)" >&2; exit 1 ;;
    esac
    asset="bridge-en_${VERSION}_${os}_${arch}.tar.gz"
    tmp=$(mktemp -d)
    trap 'rm -rf "$tmp"' EXIT
    base="https://github.com/${REPO}/releases/download/v${VERSION}"
    curl -fsSL -o "$tmp/$asset" "$base/$asset"
    curl -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS"
    if [ -n "${BRIDGE_EN_SHA256:-}" ]; then
      echo "${BRIDGE_EN_SHA256}  $tmp/$asset" | sha256sum -c -   # pinned in the app, not taken from the release
    else
      (cd "$tmp" && grep " ${asset}\$" SHA256SUMS | sha256sum -c -)
    fi
    tar -xzf "$tmp/$asset" -C "$tmp"
    install -m 0755 "$tmp/bridge-en" "$BIN/bridge-en"
    ;;
  *)
    echo "setup-bridge-en: method must be go-install or release" >&2
    exit 1
    ;;
esac

got=$("$BIN/bridge-en" -version)
if [ "$got" != "bridge-en ${VERSION}" ]; then
  echo "setup-bridge-en: installed '$got', want 'bridge-en ${VERSION}'" >&2
  exit 1
fi
[ -n "${GITHUB_PATH:-}" ] && echo "$BIN" >> "$GITHUB_PATH"
[ -n "${GITHUB_OUTPUT:-}" ] && echo "version=${VERSION}" >> "$GITHUB_OUTPUT"
echo "$got installed in $BIN"
