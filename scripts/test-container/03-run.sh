#!/bin/bash
set -euox pipefail

# Run the test container stack using docker-compose
# The single-container stack now includes just the swe-swe service (embedded
# auth, browser, preview, agent-chat -- no separate traefik/vscode-proxy/etc.)

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

# Read port from .env
SWE_PORT=$(grep "^SWE_PORT=" "$PROJECT_PATH/.env" | cut -d= -f2)
PROJECT_NAME=$(grep "^PROJECT_NAME=" "$PROJECT_PATH/.env" | cut -d= -f2)

echo "Running test stack for slot $SWE_TEST_SLOT"
echo "  Project: $PROJECT_NAME"
echo "  Port: $SWE_PORT"
echo "  Path: $PROJECT_PATH"

cd "$PROJECT_PATH"

# Start the stack
docker compose up -d

# Wait for startup
echo "Waiting for services to start..."
sleep 5

# Show container status
docker compose ps

# Show logs (last 20 lines)
echo "Recent logs:"
docker compose logs --tail=20

# Test endpoint. Probe from INSIDE the container: this script may run inside the
# dev container, whose shell cannot reach the host-published port over
# host.docker.internal (only the MCP browser's context can), so a host-side curl
# gives a false negative. An in-container check is the authoritative readiness
# signal regardless of where this script runs.
echo "Waiting for server readiness (probing inside the container) ..."
READY=0
for i in $(seq 1 20); do
    HTTP_CODE=$(docker compose exec -T swe-swe sh -c \
        "curl -s -o /dev/null -w '%{http_code}' http://localhost:${SWE_PORT}/ 2>/dev/null || wget -qO- --server-response http://localhost:${SWE_PORT}/ 2>&1 | awk '/HTTP\//{print \$2; exit}'" \
        2>/dev/null || echo "000")
    if [[ "$HTTP_CODE" == "200" || "$HTTP_CODE" == "302" || "$HTTP_CODE" == "401" ]]; then
        READY=1
        break
    fi
    sleep 2
done

if [[ "$READY" == "1" ]]; then
    echo "Phase 3 complete: server responding inside container (HTTP $HTTP_CODE)"
    echo ""
    echo "For MCP browser testing, use:"
    echo "  http://host.docker.internal:$SWE_PORT/"
else
    echo "Warning: Server did not become ready (last HTTP $HTTP_CODE)"
    echo "Check logs with: cd $PROJECT_PATH && docker compose logs"
    exit 1
fi
