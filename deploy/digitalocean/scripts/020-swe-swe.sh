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

# Clone git repository if URL provided
if [ -n "${GIT_CLONE_URL:-}" ]; then
    echo "==> Cloning git repository to /workspace..."
    git clone "$GIT_CLONE_URL" /workspace
    chown -R swe-swe:swe-swe /workspace
else
    # Create workspace directory
    mkdir -p /workspace
fi

# Change to workspace directory to avoid project lookup errors
cd /workspace

echo "==> swe-swe installation complete"
