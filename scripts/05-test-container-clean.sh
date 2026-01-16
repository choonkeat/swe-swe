#!/bin/bash
set -euox pipefail

# Clean up all test container artifacts

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(dirname "$SCRIPT_DIR")"
TMP_DIR="$WORKSPACE_DIR/tmp"

CONTAINER_NAME="swe-swe-test"
IMAGE_NAME="swe-swe-test:latest"

# Remove container if exists
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true

# Remove image if exists
docker rmi "$IMAGE_NAME" 2>/dev/null || true

# Remove tmp directory
rm -rf "$TMP_DIR"

echo "Phase 5 complete: All test artifacts cleaned up"
