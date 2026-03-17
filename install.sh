#!/bin/sh
# Install script for the abby CLI.
# Downloads a pre-built binary from GitHub Releases.
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/teabranch/abbyfile/main/install.sh | sh
#
# Environment variables:
#   VERSION      Pin to a specific version (e.g. "1.0.0"). Default: latest.
#   INSTALL_DIR  Where to place the binary. Default: /usr/local/bin.

set -e

REPO="teabranch/abbyfile"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  darwin|linux) ;;
  *)
    echo "Error: unsupported OS: $OS" >&2
    exit 1
    ;;
esac

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Error: unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

# Build download URL
BINARY="abby-${OS}-${ARCH}"
if [ -n "$VERSION" ]; then
  URL="https://github.com/${REPO}/releases/download/v${VERSION}/${BINARY}"
else
  URL="https://github.com/${REPO}/releases/latest/download/${BINARY}"
fi

echo "Downloading abby for ${OS}/${ARCH}..."
echo "  ${URL}"

# Download to temp file
TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

if command -v curl >/dev/null 2>&1; then
  curl -sSL --fail -o "$TMP" "$URL"
elif command -v wget >/dev/null 2>&1; then
  wget -qO "$TMP" "$URL"
else
  echo "Error: curl or wget is required" >&2
  exit 1
fi

chmod +x "$TMP"

# Install
mkdir -p "$INSTALL_DIR"
mv "$TMP" "${INSTALL_DIR}/abby"
trap - EXIT

echo "Installed abby to ${INSTALL_DIR}/abby"

# Verify
if "${INSTALL_DIR}/abby" --version >/dev/null 2>&1; then
  echo "  $("${INSTALL_DIR}/abby" --version)"
else
  echo "  (could not verify version)"
fi
