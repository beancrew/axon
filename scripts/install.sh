#!/bin/sh
# Usage: curl -fsSL https://axon.dev/install | sh -s -- [server|agent|cli]
#
# Detects OS/ARCH, downloads the appropriate binary from GitHub Releases,
# installs to /usr/local/bin (or ~/.axon/bin if no root access).

set -e

COMPONENT="${1:-cli}"
REPO="beancrew/axon"
INSTALL_DIR="/usr/local/bin"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    linux|darwin) ;;
    *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)          ARCH="amd64" ;;
    aarch64|arm64)   ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Map component to binary name
case "$COMPONENT" in
    server) BINARY="axon-server" ;;
    agent)  BINARY="axon-agent" ;;
    cli)    BINARY="axon" ;;
    *) echo "Unknown component: $COMPONENT"; echo "Usage: $0 [server|agent|cli]"; exit 1 ;;
esac

# Fall back to user-local install dir if we don't have write access to /usr/local/bin
if [ ! -w "$INSTALL_DIR" ]; then
    INSTALL_DIR="$HOME/.axon/bin"
    mkdir -p "$INSTALL_DIR"
    echo "No write access to /usr/local/bin, installing to $INSTALL_DIR"
    echo "Add the following to your shell profile to use $BINARY:"
    echo "  export PATH=\"\$HOME/.axon/bin:\$PATH\""
fi

# Get latest release tag from GitHub API
VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | head -n1 \
    | cut -d '"' -f4)

if [ -z "$VERSION" ]; then
    echo "Failed to fetch latest release version from GitHub"
    exit 1
fi

# Download binary
ASSET="${BINARY}-${OS}-${ARCH}"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"

echo "Downloading ${BINARY} ${VERSION} for ${OS}/${ARCH}..."
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

curl -fsSL "$URL" -o "$TMPDIR/${BINARY}"
install -m 755 "$TMPDIR/${BINARY}" "${INSTALL_DIR}/${BINARY}"

echo ""
echo "Installed ${BINARY} ${VERSION} to ${INSTALL_DIR}/${BINARY}"
echo ""

# Next-step hints
case "$COMPONENT" in
    server)
        echo "Next steps:"
        echo "  axon-server init --admin admin --password <pass>"
        echo "  axon-server start"
        ;;
    agent)
        echo "Next steps:"
        echo "  axon-agent join <server-addr> <join-token>"
        ;;
    cli)
        echo "Next steps:"
        echo "  axon config set server <server-addr>"
        echo "  axon auth login"
        ;;
esac
