#!/bin/bash
set -euo pipefail

# E2E test runner: builds a real container in dockerfile-only mode,
# runs playwright tests against it, and tears down.
#
# Usage: ./scripts/e2e.sh [playwright-args...]
# Example: ./scripts/e2e.sh tests/login.spec.js

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(dirname "$SCRIPT_DIR")"
E2E_DIR="$WORKSPACE_DIR/e2e"

# --- Configuration ---
E2E_PORT="${E2E_PORT:-9780}"
E2E_PASSWORD="${E2E_PASSWORD:-e2e-test-password}"
TEST_STACK_DIR="/workspace/tmp/e2e"
EFFECTIVE_HOME="/workspace/tmp/e2e-home"

# --- Docker-in-Docker path translation ---
detect_host_workspace() {
    local container_name
    container_name=$(docker ps --filter "name=swe-swe" --format "{{.Names}}" | grep -v "test\|e2e" | head -1)
    if [ -n "$container_name" ]; then
        docker inspect "$container_name" 2>/dev/null | jq -r '.[0].Mounts[] | select(.Destination=="/workspace") | .Source' 2>/dev/null || echo ""
    fi
}

HOST_WORKSPACE="${HOST_WORKSPACE:-$(detect_host_workspace)}"
if [ -z "$HOST_WORKSPACE" ]; then
    echo "WARNING: Could not detect HOST_WORKSPACE, falling back to /home/app/workspace/swe-swe"
    HOST_WORKSPACE="/home/app/workspace/swe-swe"
fi
echo "Host workspace: $HOST_WORKSPACE"

# --- Cleanup function ---
cleanup() {
    echo "Tearing down e2e test container..."
    if [ -f "$TEST_STACK_DIR/.swe-test-project" ]; then
        local project_path
        project_path=$(cat "$TEST_STACK_DIR/.swe-test-project")
        cd "$project_path" && docker compose down 2>/dev/null || true
    fi
}
trap cleanup EXIT

# --- Phase 1: Build CLI ---
echo "=== Phase 1: Building CLI ==="
cd "$WORKSPACE_DIR"
make build-cli

# --- Phase 2: Init project (dockerfile-only mode) ---
echo "=== Phase 2: Initializing project (dockerfile-only mode) ==="
rm -rf "$TEST_STACK_DIR"
rm -rf "$EFFECTIVE_HOME/.swe-swe/projects/"*e2e* 2>/dev/null || true
mkdir -p "$TEST_STACK_DIR" "$EFFECTIVE_HOME"

cd "$TEST_STACK_DIR"
git init -q
git config user.email "e2e@test.local"
git config user.name "E2E Test"
git commit -q --allow-empty -m "initial"

# No SSL + no vscode → dockerfile-only mode auto-detected
# Use non-default port ranges to avoid conflicts with the production stack
HOME="$EFFECTIVE_HOME" "$WORKSPACE_DIR/dist/swe-swe.linux-amd64" init \
    --project-directory="$TEST_STACK_DIR" \
    --agents=opencode \
    --preview-ports=3200-3219 \
    --public-ports=5200-5219

PROJECT_PATH=$(ls -d "$EFFECTIVE_HOME/.swe-swe/projects/"*e2e*/ 2>/dev/null | head -1)
if [ -z "$PROJECT_PATH" ]; then
    echo "ERROR: Could not find generated project directory"
    exit 1
fi
echo "Project: $PROJECT_PATH"
echo "$PROJECT_PATH" > "$TEST_STACK_DIR/.swe-test-project"

# --- Phase 3: Configure for e2e ---
echo "=== Phase 3: Configuring ==="
ENV_FILE="$PROJECT_PATH/.env"
HOST_TEST_STACK_DIR="${TEST_STACK_DIR/#\/workspace\//${HOST_WORKSPACE}/}"
HOST_PROJECT_PATH="${PROJECT_PATH/#\/workspace\//${HOST_WORKSPACE}/}"

# Update .env
cat >> "$ENV_FILE" <<EOF
SWE_PORT=$E2E_PORT
SWE_SWE_PASSWORD=$E2E_PASSWORD
WORKSPACE_DIR=$HOST_TEST_STACK_DIR
EOF

# Sibling container override: translate container paths to host paths
# (Docker daemon runs on host, so volume mounts need host paths)
# Explicitly set environment vars to override host env (host SWE_SWE_PASSWORD
# takes precedence over .env if not explicitly set in the compose override)
cat > "$PROJECT_PATH/docker-compose.override.yml" <<EOF
# Auto-generated for sibling-container e2e testing
services:
  swe-swe:
    environment:
      - SWE_SWE_PASSWORD=${E2E_PASSWORD}
    volumes:
      - ${HOST_TEST_STACK_DIR}:/workspace
      - ${HOST_TEST_STACK_DIR}/.swe-swe/worktrees:/worktrees
      - ${HOST_PROJECT_PATH}home:/home/app
EOF

echo "  Port: $E2E_PORT"
echo "  Password: $E2E_PASSWORD"

# --- Phase 4: Build container ---
echo "=== Phase 4: Building container ==="
cd "$PROJECT_PATH"
docker compose build

# --- Phase 5: Start container ---
echo "=== Phase 5: Starting container ==="
docker compose up -d

# Wait for server to be ready
echo "Waiting for server..."
HOST_IP="${HOST_IP:-host.docker.internal}"
SERVER_READY=false
for i in $(seq 1 30); do
    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 "http://$HOST_IP:$E2E_PORT/" 2>/dev/null) || HTTP_CODE="000"
    if [[ "$HTTP_CODE" =~ ^[1-5][0-9][0-9]$ ]]; then
        echo "Server ready (HTTP $HTTP_CODE) after ${i}s"
        SERVER_READY=true
        break
    fi
    sleep 1
done

if [[ "$SERVER_READY" != "true" ]]; then
    echo "ERROR: Server did not start within 30s (last HTTP_CODE=$HTTP_CODE)"
    docker compose logs
    exit 1
fi

# --- Phase 6: Run playwright tests ---
echo "=== Phase 6: Running e2e tests ==="
cd "$E2E_DIR"
npm install --silent 2>/dev/null

PORT="$E2E_PORT" \
SWE_SWE_PASSWORD="$E2E_PASSWORD" \
E2E_BASE_URL="http://$HOST_IP:$E2E_PORT" \
    npx playwright test "$@"

echo "=== E2E tests passed ==="
