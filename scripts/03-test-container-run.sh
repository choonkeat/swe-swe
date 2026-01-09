#!/bin/bash
set -euox pipefail

# Run the test container stack using docker-compose
# The stack includes: traefik, auth, chrome, swe-swe, vscode-proxy, code-server

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

# Test endpoint
HOST_IP="${HOST_IP:-host.docker.internal}"
echo "Testing server response at http://$HOST_IP:$SWE_PORT/ ..."

# The auth endpoint returns 401 without credentials, which is expected
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "http://$HOST_IP:$SWE_PORT/" 2>/dev/null || echo "000")
if [[ "$HTTP_CODE" == "200" || "$HTTP_CODE" == "302" || "$HTTP_CODE" == "401" ]]; then
    echo "Phase 3 complete: Test stack running at http://$HOST_IP:$SWE_PORT/"
    echo ""
    echo "For MCP browser testing, use:"
    echo "  http://host.docker.internal:$SWE_PORT/"
else
    echo "Warning: Server may not be responding correctly (HTTP $HTTP_CODE)"
    echo "Check logs with: cd $PROJECT_PATH && docker compose logs"
    exit 1
fi
