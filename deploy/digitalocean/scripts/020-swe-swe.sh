#!/bin/bash
# Download and install swe-swe binary
set -euo pipefail

echo "==> Installing swe-swe..."

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  SWE_ARCH="amd64" ;;
    aarch64) SWE_ARCH="arm64" ;;
    *)       echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Get latest release version
SWE_VERSION=$(curl -fsSL https://api.github.com/repos/anthropics/swe-swe/releases/latest | grep '"tag_name":' | sed -E 's/.*"v?([^"]+)".*/\1/')

# Download binary
curl -fsSL "https://github.com/anthropics/swe-swe/releases/download/v${SWE_VERSION}/swe-swe-linux-${SWE_ARCH}" -o /usr/local/bin/swe-swe
chmod +x /usr/local/bin/swe-swe

# Verify installation
swe-swe --version

# Create workspace directory
mkdir -p /workspace

echo "==> swe-swe installation complete"
