#!/bin/bash
set -euox pipefail

# Build the test container using docker-compose
# Reads slot info from previous init step

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(dirname "$SCRIPT_DIR")"

# Find active slot from lock files
find_active_slot() {
    for lock_dir in /tmp/swe-swe-test-slot-*.lock; do
        if [ -d "$lock_dir" ] && [ -f "$lock_dir/pid" ]; then
            local slot
            slot=$(basename "$lock_dir" | sed 's/swe-swe-test-slot-//' | sed 's/.lock//')
            echo "$slot"
            return 0
        fi
    done
    return 1
}

# Get slot from environment or find it
if [ -z "${SWE_TEST_SLOT:-}" ]; then
    SWE_TEST_SLOT=$(find_active_slot) || {
        echo "ERROR: No active test slot found. Run 01-test-container-init.sh first."
        exit 1
    }
fi

TEST_STACK_DIR="/workspace/.test-repos/swe-swe-test-${SWE_TEST_SLOT}"
PROJECT_PATH=$(cat "$TEST_STACK_DIR/.swe-test-project" 2>/dev/null) || {
    echo "ERROR: Could not find project path. Run 01-test-container-init.sh first."
    exit 1
}

echo "Building test container for slot $SWE_TEST_SLOT"
echo "Project path: $PROJECT_PATH"

cd "$PROJECT_PATH"

# Build using docker-compose (use NO_CACHE=1 to force clean build)
BUILD_ARGS=""
if [[ "${NO_CACHE:-}" == "1" ]]; then
    BUILD_ARGS="--no-cache"
fi
docker compose build $BUILD_ARGS

echo "Phase 2 complete: Container images built successfully"
