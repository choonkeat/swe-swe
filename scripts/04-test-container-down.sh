#!/bin/bash
set -euox pipefail

# Stop the test container stack and release the slot

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
        echo "No active test slot found. Nothing to stop."
        exit 0
    }
fi

TEST_STACK_DIR="/workspace/.test-repos/swe-swe-test-${SWE_TEST_SLOT}"
LOCK_DIR="/tmp/swe-swe-test-slot-${SWE_TEST_SLOT}.lock"

if [ -f "$TEST_STACK_DIR/.swe-test-project" ]; then
    PROJECT_PATH=$(cat "$TEST_STACK_DIR/.swe-test-project")
    echo "Stopping test stack for slot $SWE_TEST_SLOT"
    echo "  Path: $PROJECT_PATH"

    cd "$PROJECT_PATH"
    docker compose down || true
else
    echo "No project path found for slot $SWE_TEST_SLOT"
fi

# Release the slot lock
if [ -d "$LOCK_DIR" ]; then
    rm -rf "$LOCK_DIR"
    echo "Released slot $SWE_TEST_SLOT"
fi

echo "Phase 4 complete: Test stack stopped and slot released"
