#!/bin/bash
set -euo pipefail

# Bring up an e2e test environment in the specified mode.
#
# Usage: ./scripts/e2e-up.sh <simple|compose|docker>
#
# simple  - dockerfile-only mode (no Traefik), port 9780
# compose - Traefik compose mode (--with-vscode, skip vscode/chrome), port 9770
# docker  - with Docker socket access (--with-docker), port 9760

MODE="${1:-}"
if [[ "$MODE" != "simple" && "$MODE" != "compose" && "$MODE" != "docker" ]]; then
    echo "Usage: $0 <simple|compose|docker>"
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(dirname "$SCRIPT_DIR")"
E2E_PASSWORD="${E2E_PASSWORD:-e2e-test-password}"

# --- Mode-specific configuration ---
if [[ "$MODE" == "simple" ]]; then
    E2E_PORT=9780
    PREVIEW_PORTS="3200-3219"
    PUBLIC_PORTS="5200-5219"
    INIT_EXTRA_FLAGS=""
elif [[ "$MODE" == "docker" ]]; then
    E2E_PORT=9760
    PREVIEW_PORTS="3300-3319"
    PUBLIC_PORTS="5300-5319"
    INIT_EXTRA_FLAGS="--with-docker"
else
    E2E_PORT=9770
    PREVIEW_PORTS="3100-3119"
    PUBLIC_PORTS="5100-5119"
    INIT_EXTRA_FLAGS="--with-vscode"
fi

TEST_STACK_DIR="/workspace/tmp/e2e-${MODE}"
EFFECTIVE_HOME="/workspace/tmp/e2e-${MODE}-home"
STATE_FILE="/workspace/tmp/e2e-${MODE}/.e2e-state"

echo "=== e2e-up: mode=$MODE port=$E2E_PORT ==="

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

# --- Phase 1: Build CLI ---
echo "--- Building CLI ---"
cd "$WORKSPACE_DIR"
make build-cli

# --- Phase 2: Init project ---
echo "--- Initializing project (${MODE} mode) ---"
rm -rf "$TEST_STACK_DIR"
rm -rf "$EFFECTIVE_HOME/.swe-swe/projects/"*e2e* 2>/dev/null || true
mkdir -p "$TEST_STACK_DIR" "$EFFECTIVE_HOME"

cd "$TEST_STACK_DIR"
git init -q
git config user.email "e2e@test.local"
git config user.name "E2E Test"
git commit -q --allow-empty -m "initial"

HOME="$EFFECTIVE_HOME" "$WORKSPACE_DIR/dist/swe-swe.linux-amd64" init \
    --project-directory="$TEST_STACK_DIR" \
    --agents=opencode \
    --preview-ports="$PREVIEW_PORTS" \
    --public-ports="$PUBLIC_PORTS" \
    $INIT_EXTRA_FLAGS

PROJECT_PATH=$(ls -d "$EFFECTIVE_HOME/.swe-swe/projects/"*e2e* 2>/dev/null | head -1)
if [ -z "$PROJECT_PATH" ]; then
    echo "ERROR: Could not find generated project directory"
    exit 1
fi
# Ensure trailing slash
[[ "$PROJECT_PATH" == */ ]] || PROJECT_PATH="${PROJECT_PATH}/"
echo "Project: $PROJECT_PATH"

# --- Phase 3: Configure for e2e ---
echo "--- Configuring ---"
ENV_FILE="${PROJECT_PATH}.env"
HOST_TEST_STACK_DIR="${TEST_STACK_DIR/#\/workspace\//${HOST_WORKSPACE}/}"
HOST_PROJECT_PATH="${PROJECT_PATH/#\/workspace\//${HOST_WORKSPACE}/}"

# Update .env
cat >> "$ENV_FILE" <<EOF
SWE_PORT=$E2E_PORT
SWE_SWE_PASSWORD=$E2E_PASSWORD
WORKSPACE_DIR=$HOST_TEST_STACK_DIR
EOF

# Create docker-compose.override.yml with host path translation
if [[ "$MODE" == "simple" ]]; then
    cat > "${PROJECT_PATH}docker-compose.override.yml" <<EOF
# Auto-generated for sibling-container e2e testing (simple mode)
services:
  swe-swe:
    environment:
      - SWE_SWE_PASSWORD=${E2E_PASSWORD}
    volumes:
      - ${HOST_TEST_STACK_DIR}:/workspace
      - ${HOST_TEST_STACK_DIR}/.swe-swe/worktrees:/worktrees
      - ${HOST_PROJECT_PATH}home:/home/app
EOF
elif [[ "$MODE" == "docker" ]]; then
    cat > "${PROJECT_PATH}docker-compose.override.yml" <<EOF
# Auto-generated for sibling-container e2e testing (docker mode)
services:
  swe-swe:
    environment:
      - SWE_SWE_PASSWORD=${E2E_PASSWORD}
    volumes:
      - ${HOST_TEST_STACK_DIR}:/workspace
      - ${HOST_TEST_STACK_DIR}/.swe-swe/worktrees:/worktrees
      - ${HOST_PROJECT_PATH}home:/home/app
EOF
else
    cat > "${PROJECT_PATH}docker-compose.override.yml" <<EOF
# Auto-generated for sibling-container e2e testing (compose mode)
services:
  traefik:
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ${HOST_PROJECT_PATH}traefik-dynamic.yml:/etc/traefik/dynamic.yml:ro

  swe-swe:
    environment:
      - SWE_SWE_PASSWORD=${E2E_PASSWORD}
    volumes:
      - ${HOST_TEST_STACK_DIR}:/workspace
      - ${HOST_TEST_STACK_DIR}/.swe-swe/worktrees:/worktrees
      - ${HOST_PROJECT_PATH}home:/home/app
EOF
fi

echo "  Port: $E2E_PORT"
echo "  Password: $E2E_PASSWORD"

# --- Phase 4: Build container ---
echo "--- Building container ---"
cd "$PROJECT_PATH"
docker compose build

# --- Phase 5: Start container ---
echo "--- Starting container ---"
if [[ "$MODE" == "compose" ]]; then
    # Only start swe-swe + traefik to save memory (skip vscode, chrome, etc.)
    docker compose up -d swe-swe traefik
else
    docker compose up -d
fi

# Wait for server to be ready
echo "Waiting for server..."
HOST_IP="${HOST_IP:-host.docker.internal}"
SERVER_READY=false
for i in $(seq 1 60); do
    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 "http://$HOST_IP:$E2E_PORT/" 2>/dev/null) || HTTP_CODE="000"
    if [[ "$HTTP_CODE" =~ ^[1-5][0-9][0-9]$ ]]; then
        echo "Server ready (HTTP $HTTP_CODE) after ${i}s"
        SERVER_READY=true
        break
    fi
    sleep 1
done

if [[ "$SERVER_READY" != "true" ]]; then
    echo "ERROR: Server did not start within 60s (last HTTP_CODE=$HTTP_CODE)"
    docker compose logs
    exit 1
fi

# --- Write state file ---
cat > "$STATE_FILE" <<EOF
MODE=$MODE
PORT=$E2E_PORT
PASSWORD=$E2E_PASSWORD
PROJECT_PATH=$PROJECT_PATH
HOST_IP=$HOST_IP
EOF

echo "=== e2e-up complete: ${MODE} mode running at http://${HOST_IP}:${E2E_PORT}/ ==="
echo "State file: $STATE_FILE"
