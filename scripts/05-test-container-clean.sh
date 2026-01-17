#!/bin/bash
set -euox pipefail

# Clean up all test container artifacts for all slots

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(dirname "$SCRIPT_DIR")"

echo "Cleaning up all test container artifacts..."

# Clean up all slots
for lock_dir in /tmp/swe-swe-test-slot-*.lock; do
    if [ -d "$lock_dir" ]; then
        slot=$(basename "$lock_dir" | sed 's/swe-swe-test-slot-//' | sed 's/.lock//')
        TEST_STACK_DIR="/workspace/.test-repos/swe-swe-test-${slot}"

        if [ -f "$TEST_STACK_DIR/.swe-test-project" ]; then
            PROJECT_PATH=$(cat "$TEST_STACK_DIR/.swe-test-project")
            echo "Cleaning slot $slot: $PROJECT_PATH"

            # Stop containers if running
            cd "$PROJECT_PATH" 2>/dev/null && docker compose down --rmi local 2>/dev/null || true
        fi

        # Clean up test stack directory
        rm -rf "$TEST_STACK_DIR"

        # Release lock
        rm -rf "$lock_dir"
        echo "Released slot $slot"
    fi
done

# Also clean up old-style lock (from previous implementation)
rm -rf /workspace/.test-repos/swe-swe-test-container.lock 2>/dev/null || true

# Clean up any dangling test images
docker images --filter "reference=*swe-test*" -q | xargs -r docker rmi 2>/dev/null || true

echo "Phase 5 complete: All test artifacts cleaned up"
