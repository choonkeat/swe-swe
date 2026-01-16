#!/bin/bash
set -euox pipefail

# Generate fresh swe-swe project files in an isolated location
# This allows testing new container builds without affecting the current stack

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(dirname "$SCRIPT_DIR")"
TMP_DIR="$WORKSPACE_DIR/tmp"

# EFFECTIVE_HOME: where swe-swe stores config and container mounts /home/app
# - Default: persistent at $WORKSPACE_DIR/.home (survives clean)
# - Set to $TMP_DIR/home for ephemeral (cleaned by script 05)
EFFECTIVE_HOME="${EFFECTIVE_HOME:-$WORKSPACE_DIR/.home}"

# Clean up previous project artifacts
rm -rf "$TMP_DIR/project-directory"
rm -rf "$EFFECTIVE_HOME/.swe-swe/projects"

# Create fresh directories
mkdir -p "$EFFECTIVE_HOME" "$TMP_DIR/project-directory"

# Run swe-swe init with EFFECTIVE_HOME
env HOME="$EFFECTIVE_HOME" "$WORKSPACE_DIR/dist/swe-swe.linux-amd64" init \
    --project-directory="$TMP_DIR/project-directory"

# Find and print the generated project path
PROJECT_PATH=$(ls -d "$EFFECTIVE_HOME/.swe-swe/projects/"*/)
echo "Generated project at: $PROJECT_PATH"

# Verify expected files exist
for f in Dockerfile docker-compose.yml entrypoint.sh; do
    if [[ ! -f "$PROJECT_PATH/$f" ]]; then
        echo "ERROR: Missing expected file: $f"
        exit 1
    fi
done

if [[ ! -d "$PROJECT_PATH/swe-swe-server" ]]; then
    echo "ERROR: Missing expected directory: swe-swe-server"
    exit 1
fi

echo "Phase 1 complete: Project files generated successfully"
