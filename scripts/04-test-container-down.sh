#!/bin/bash
set -euox pipefail

# Stop and remove the test container

CONTAINER_NAME="swe-swe-test"

# Stop and remove container (idempotent - no error if doesn't exist)
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true

echo "Phase 4 complete: Test container stopped and removed"
