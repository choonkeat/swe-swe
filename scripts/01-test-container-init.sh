#!/bin/bash
set -euox pipefail

# Generate fresh swe-swe project files in an isolated location
# Uses slot-based semaphore to support multiple concurrent test stacks

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(dirname "$SCRIPT_DIR")"

# --- Slot-based Semaphore: supports multiple concurrent test stacks ---
# Each slot gets a unique port assignment:
# - Slot 0: PORT 19770, PROJECT_NAME=swe-test-0
# - Slot 1: PORT 19771, PROJECT_NAME=swe-test-1
# - etc.
MAX_SLOTS="${MAX_SLOTS:-3}"  # Default to 3 slots
LOCK_TIMEOUT=3600  # Consider lock stale after 1 hour
LOCK_BASE="/tmp/swe-swe-test-slot"

acquire_slot() {
    local slot=0
    local waited=0
    local wait_interval=5

    while true; do
        for slot in $(seq 0 $((MAX_SLOTS - 1))); do
            local lock_dir="${LOCK_BASE}-${slot}.lock"

            # Check for stale lock
            if [ -d "$lock_dir" ]; then
                if [ -f "$lock_dir/pid" ]; then
                    local lock_pid
                    lock_pid=$(cat "$lock_dir/pid" 2>/dev/null || echo "")

                    # Check if process is still alive
                    if [ -n "$lock_pid" ] && ! kill -0 "$lock_pid" 2>/dev/null; then
                        echo "Removing stale lock for slot $slot (process $lock_pid no longer exists)"
                        rm -rf "$lock_dir"
                    fi
                fi

                # Check for timeout
                if [ -f "$lock_dir/timestamp" ]; then
                    local lock_time
                    lock_time=$(cat "$lock_dir/timestamp" 2>/dev/null || echo "0")
                    local now
                    now=$(date +%s)
                    if [ $((now - lock_time)) -gt $LOCK_TIMEOUT ]; then
                        echo "Removing stale lock for slot $slot (timed out after $LOCK_TIMEOUT seconds)"
                        rm -rf "$lock_dir"
                    fi
                fi
            fi

            # Try to acquire lock (mkdir is atomic)
            if mkdir "$lock_dir" 2>/dev/null; then
                echo $$ > "$lock_dir/pid"
                date +%s > "$lock_dir/timestamp"

                # Calculate port and project name
                local port=$((19770 + slot))
                local project_name="swe-test-${slot}"

                echo "$port" > "$lock_dir/port"
                echo "$project_name" > "$lock_dir/project_name"

                echo "Acquired slot $slot (PID $$, PORT=$port, PROJECT_NAME=$project_name)"

                # Export for subsequent scripts
                export SWE_TEST_SLOT="$slot"
                export SWE_PORT="$port"
                export PROJECT_NAME="$project_name"
                export SWE_LOCK_DIR="$lock_dir"
                return 0
            fi
        done

        # All slots busy, wait and retry
        echo "All $MAX_SLOTS slots busy, waiting... (${waited}s elapsed)"
        sleep $wait_interval
        waited=$((waited + wait_interval))
    done
}

acquire_slot
# --- End slot-based semaphore ---

# Test stack directory based on slot
TEST_STACK_DIR="/tmp/swe-swe-test-${SWE_TEST_SLOT}"

# Clean up previous test stack
rm -rf "$TEST_STACK_DIR"
mkdir -p "$TEST_STACK_DIR"

# Initialize as git repo (required for swe-swe git worktree operations)
cd "$TEST_STACK_DIR"
git init
git config user.email "test@example.com"
git config user.name "Test User"
git commit --allow-empty -m "initial commit"

# Run swe-swe init
"$WORKSPACE_DIR/dist/swe-swe.linux-amd64" init --project-directory="$TEST_STACK_DIR"

# Find the generated metadata directory
HOME_DIR="${HOME:-/home/app}"
PROJECT_PATH=$(ls -d "$HOME_DIR/.swe-swe/projects/"*/ | head -1)
echo "Generated project at: $PROJECT_PATH"

# Update .env with our slot-specific values
ENV_FILE="$PROJECT_PATH/.env"
if [ -f "$ENV_FILE" ]; then
    # Update existing PROJECT_NAME
    sed -i "s/^PROJECT_NAME=.*/PROJECT_NAME=${PROJECT_NAME}/" "$ENV_FILE"
else
    # Create .env if it doesn't exist
    echo "PROJECT_NAME=${PROJECT_NAME}" > "$ENV_FILE"
fi
echo "SWE_PORT=${SWE_PORT}" >> "$ENV_FILE"
echo "Updated $ENV_FILE with PROJECT_NAME=$PROJECT_NAME, SWE_PORT=$SWE_PORT"

# Write slot info for subsequent scripts
echo "$SWE_TEST_SLOT" > "$TEST_STACK_DIR/.swe-test-slot"
echo "$PROJECT_PATH" > "$TEST_STACK_DIR/.swe-test-project"

# Verify expected files exist
for f in Dockerfile docker-compose.yml entrypoint.sh; do
    if [[ ! -f "$PROJECT_PATH/$f" ]]; then
        echo "ERROR: Missing expected file: $f"
        exit 1
    fi
done

echo "Phase 1 complete: Project files generated successfully"
echo "  Slot: $SWE_TEST_SLOT"
echo "  Port: $SWE_PORT"
echo "  Project: $PROJECT_NAME"
echo "  Path: $PROJECT_PATH"
