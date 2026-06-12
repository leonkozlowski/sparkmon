#!/usr/bin/env bash
# sparkmon installer
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/leonkozlowski/sparkmon/main/install.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/leonkozlowski/sparkmon/main/install.sh | bash -s -- --bin-dir ~/bin
#
# Flags / env vars:
#   --bin-dir DIR    where the binary goes        (BIN_DIR,  default /usr/local/bin)
#   --version TAG    release tag to install       (VERSION,  default: latest)
#
# Installs the prebuilt binary from GitHub Releases for your OS/arch
# (linux/darwin × amd64/arm64). If no release asset is available, falls
# back to `go install` when a Go toolchain is present.

set -euo pipefail

REPO="leonkozlowski/sparkmon"
BIN_DIR="${BIN_DIR:-/usr/local/bin}"
VERSION="${VERSION:-latest}"

while [ $# -gt 0 ]; do
    case "$1" in
        --bin-dir)  BIN_DIR="$2"; shift 2 ;;
        --version)  VERSION="$2"; shift 2 ;;
        *) echo "unknown flag: $1" >&2; exit 2 ;;
    esac
done

case "$(uname -s)" in
    Linux)  OS=linux ;;
    Darwin) OS=darwin ;;
    *) echo "unsupported OS: $(uname -s) (build from source: go install github.com/$REPO/cmd/sparkmon@latest)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
    x86_64|amd64)  ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

if [ "$VERSION" = "latest" ]; then
    URL="https://github.com/$REPO/releases/latest/download/sparkmon-$OS-$ARCH"
else
    URL="https://github.com/$REPO/releases/download/$VERSION/sparkmon-$OS-$ARCH"
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "sparkmon: downloading $URL"
if curl -fsSL "$URL" -o "$TMP/sparkmon"; then
    chmod +x "$TMP/sparkmon"
elif command -v go >/dev/null 2>&1; then
    echo "sparkmon: no release asset for $OS/$ARCH — building from source with $(go version | cut -d' ' -f3)"
    GOBIN="$TMP" go install "github.com/$REPO/cmd/sparkmon@latest"
else
    echo "sparkmon: download failed and no Go toolchain found." >&2
    echo "Install Go 1.25+ and run: go install github.com/$REPO/cmd/sparkmon@latest" >&2
    exit 1
fi

# sudo only if we can't write to BIN_DIR ourselves
SUDO=""
mkdir -p "$BIN_DIR" 2>/dev/null || SUDO=sudo
if [ ! -w "$BIN_DIR" ]; then
    SUDO=sudo
fi
$SUDO mkdir -p "$BIN_DIR"
$SUDO mv "$TMP/sparkmon" "$BIN_DIR/sparkmon"

echo "sparkmon: installed $("$BIN_DIR/sparkmon" version 2>/dev/null || echo sparkmon) to $BIN_DIR/sparkmon"
echo ""
echo "Quick start:"
echo "  1. Deploy the exporters to your nodes:   sparkmon deploy me@spark-01 me@spark-02"
echo "  2. Check they're reachable:              sparkmon health spark-01 spark-02"
echo "  3. Run the dashboard:                    sparkmon -nodes spark-01=<ip>,spark-02=<ip>"
echo ""
echo "Or create a config file (then plain 'sparkmon' works):"
echo "  mkdir -p ~/.config/sparkmon"
echo "  curl -fsSL https://raw.githubusercontent.com/$REPO/main/config.yaml.example -o ~/.config/sparkmon/config.yaml"
echo "  \$EDITOR ~/.config/sparkmon/config.yaml"
