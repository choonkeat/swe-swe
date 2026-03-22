#!/bin/bash
# Install swe-swe launcher and fetch latest binary from npm registry
set -euo pipefail

echo "==> Installing swe-swe launcher..."

# Launcher script should already be copied by Packer file provisioner
if [ ! -f /usr/local/bin/swe-swe ]; then
    echo "ERROR: swe-swe launcher not found at /usr/local/bin/swe-swe"
    echo "This means Packer did not copy files/ to /"
    exit 1
fi

chmod +x /usr/local/bin/swe-swe

# Pre-fetch the latest binary so the image ships with a cached copy
echo "==> Fetching latest swe-swe binary from npm registry..."
/usr/local/bin/swe-swe version || echo "WARNING: initial fetch failed, will retry on first boot"

# Clone git repository if URL provided
if [ -n "${GIT_CLONE_URL:-}" ]; then
    echo "==> Cloning git repository to /workspace..."
    git clone "$GIT_CLONE_URL" /workspace
else
    # Create workspace directory
    mkdir -p /workspace
fi

# Change to workspace directory to avoid project lookup errors
cd /workspace

echo "==> swe-swe installation complete"
