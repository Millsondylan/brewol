#!/bin/bash
# brewol installation script

set -e

VERSION="0.2.0"
REPO="Millsondylan/brewol"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$OS" in
  darwin)
    OS="darwin"
    ;;
  linux)
    OS="linux"
    ;;
  *)
    echo "Unsupported OS: $OS"
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64)
    ARCH="amd64"
    ;;
  arm64|aarch64)
    ARCH="arm64"
    ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

# For Linux, only amd64 is built
if [ "$OS" = "linux" ] && [ "$ARCH" = "arm64" ]; then
  echo "Linux ARM64 not supported yet. Using AMD64 binary."
  ARCH="amd64"
fi

BINARY_NAME="brewol-${OS}-${ARCH}"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/v${VERSION}/${BINARY_NAME}"

echo "Installing brewol v${VERSION} for ${OS}-${ARCH}..."
echo "Download URL: ${DOWNLOAD_URL}"

# Create temporary directory
TMP_DIR=$(mktemp -d)
trap "rm -rf $TMP_DIR" EXIT

# Download binary
if command -v curl >/dev/null 2>&1; then
  curl -sL "$DOWNLOAD_URL" -o "$TMP_DIR/brewol"
elif command -v wget >/dev/null 2>&1; then
  wget -q "$DOWNLOAD_URL" -O "$TMP_DIR/brewol"
else
  echo "Error: curl or wget is required"
  exit 1
fi

# Make executable
chmod +x "$TMP_DIR/brewol"

# Install to system
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP_DIR/brewol" "$INSTALL_DIR/brewol"
  echo "✓ Installed to $INSTALL_DIR/brewol"
else
  echo "Installing to $INSTALL_DIR requires sudo..."
  sudo mv "$TMP_DIR/brewol" "$INSTALL_DIR/brewol"
  echo "✓ Installed to $INSTALL_DIR/brewol"
fi

echo ""
echo "brewol v${VERSION} installed successfully!"
echo "Run 'brewol' to get started."
