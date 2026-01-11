#!/bin/bash
# Install swe-swe binary (pre-bundled by Packer from local ./dist/)
set -euo pipefail

echo "==> Installing swe-swe..."

# Binary should already be copied by Packer file provisioner
if [ ! -f /usr/local/bin/swe-swe ]; then
    echo "ERROR: swe-swe binary not found at /usr/local/bin/swe-swe"
    echo "This means Packer did not copy the binary from ./dist/"
    echo "Ensure you ran 'make build' at the repo root before building with Packer"
    exit 1
fi

chmod +x /usr/local/bin/swe-swe

# Verify installation
swe-swe --version

# Create workspace directory
mkdir -p /workspace

echo "==> swe-swe installation complete"
