#!/bin/bash
set -euox pipefail

# Build the swe-swe test image from generated project files

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(dirname "$SCRIPT_DIR")"
TMP_DIR="$WORKSPACE_DIR/tmp"

# Find the generated project directory
PROJECT_PATH=$(ls -d "$TMP_DIR/home/.swe-swe/projects/"*/)
echo "Building from: $PROJECT_PATH"

cd "$PROJECT_PATH"

# Build just the swe-swe service
docker compose build swe-swe

# Get the auto-generated image name and tag it
COMPOSE_PROJECT=$(basename "$PROJECT_PATH")
AUTO_IMAGE="${COMPOSE_PROJECT}-swe-swe"
docker tag "$AUTO_IMAGE" swe-swe-test:latest

echo "Tagged image as swe-swe-test:latest"

# Smoke test: verify swe-swe-server binary works
docker run --rm swe-swe-test:latest /usr/local/bin/swe-swe-server --help

echo "Phase 2 complete: Image built and smoke test passed"
