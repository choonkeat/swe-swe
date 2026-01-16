#!/bin/bash
set -euox pipefail

# Generate fresh swe-swe project files in an isolated location
# This allows testing new container builds without affecting the current stack

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(dirname "$SCRIPT_DIR")"
TMP_DIR="$WORKSPACE_DIR/tmp"

# --- Semaphore: ensure only one agent uses test container at a time ---
# This prevents port conflicts when multiple agents try to use host.docker.internal
LOCK_DIR="/tmp/swe-swe-test-container.lock"
LOCK_TIMEOUT=3600  # Consider lock stale after 1 hour

acquire_lock() {
    local waited=0
    local wait_interval=5

    while true; do
        # Check for stale lock
        if [ -d "$LOCK_DIR" ]; then
            if [ -f "$LOCK_DIR/pid" ]; then
                local lock_pid
                lock_pid=$(cat "$LOCK_DIR/pid" 2>/dev/null || echo "")

                # Check if process is still alive
                if [ -n "$lock_pid" ] && ! kill -0 "$lock_pid" 2>/dev/null; then
                    echo "Removing stale lock (process $lock_pid no longer exists)"
                    rm -rf "$LOCK_DIR"
                fi
            fi

            # Check for timeout
            if [ -f "$LOCK_DIR/timestamp" ]; then
                local lock_time
                lock_time=$(cat "$LOCK_DIR/timestamp" 2>/dev/null || echo "0")
                local now
                now=$(date +%s)
                if [ $((now - lock_time)) -gt $LOCK_TIMEOUT ]; then
                    echo "Removing stale lock (timed out after $LOCK_TIMEOUT seconds)"
                    rm -rf "$LOCK_DIR"
                fi
            fi
        fi

        # Try to acquire lock (mkdir is atomic)
        if mkdir "$LOCK_DIR" 2>/dev/null; then
            echo $$ > "$LOCK_DIR/pid"
            date +%s > "$LOCK_DIR/timestamp"
            echo "Lock acquired by PID $$"
            return 0
        fi

        # Lock held by someone else, wait
        echo "Test container lock held, waiting... (${waited}s elapsed)"
        sleep $wait_interval
        waited=$((waited + wait_interval))
    done
}

acquire_lock
# --- End semaphore ---

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
