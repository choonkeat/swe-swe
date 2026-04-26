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
#
# Each mode reserves a 30-port-wide range per role to avoid colliding with
# the live swe-swe stack (which uses the 3000/4000/5000/6000/7000 +20000
# defaults). Ranges per mode are offset by 100 (compose), 200 (simple), or
# 300 (docker) -- giving 30 sessions worth of headroom before "no available
# port quintuple" hits, which is what the full e2e suite needs.
#
# Why 30, not 20: the agent-browser + ports + terminal-ui-tabs specs each
# spin up several sessions; running back-to-back against a 20-port pool
# exhausted preview ports before any reaper could free them.
if [[ "$MODE" == "simple" ]]; then
    E2E_PORT=9780
    PREVIEW_PORTS="3200-3229"
    AGENT_CHAT_PORTS="4200-4229"
    PUBLIC_PORTS="5200-5229"
    CDP_PORTS="6200-6229"
    VNC_PORTS="7200-7229"
    INIT_EXTRA_FLAGS=""
elif [[ "$MODE" == "docker" ]]; then
    E2E_PORT=9760
    PREVIEW_PORTS="3300-3329"
    AGENT_CHAT_PORTS="4300-4329"
    PUBLIC_PORTS="5300-5329"
    CDP_PORTS="6300-6329"
    VNC_PORTS="7300-7329"
    INIT_EXTRA_FLAGS="--with-docker"
else
    E2E_PORT=9770
    PREVIEW_PORTS="3100-3129"
    AGENT_CHAT_PORTS="4100-4129"
    PUBLIC_PORTS="5100-5129"
    CDP_PORTS="6100-6129"
    VNC_PORTS="7100-7129"
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

# Optional: pass through SWE_PUBLIC_HOSTNAME for tunnel-mode e2e runs.
# When unset (typical), the frontend stays in port-based mode (no behavior
# change). When set (e.g. SWE_PUBLIC_HOSTNAME=fake-tunnel.example.com), the
# frontend builds cross-port URLs as {port}.{hostname}; tunnel.spec.js
# asserts that shape. Page won't actually load (DNS is fake) but iframe src
# is the assertion target.
SWE_PUBLIC_HOSTNAME_PASSTHROUGH="${SWE_PUBLIC_HOSTNAME:-}"

# Create docker-compose.override.yml with host path translation.
#
# Hardcode ALL SWE_*_PORTS in the override so they aren't subject to leaks
# from the dev shell's environment. The base compose uses ${VAR:-default}
# substitution; if the parent shell exports SWE_PREVIEW_PORTS=3000-3019
# (which the live swe-swe-server does), it would override the .env file.
# An explicit `- SWE_FOO=bar` line in the override env list wins over the
# substitution form in the base, regardless of what the shell exports.
if [[ "$MODE" == "simple" ]]; then
    cat > "${PROJECT_PATH}docker-compose.override.yml" <<EOF
# Auto-generated for sibling-container e2e testing (simple mode)
services:
  swe-swe:
    environment:
      - SWE_SWE_PASSWORD=${E2E_PASSWORD}
      - SWE_PUBLIC_HOSTNAME=${SWE_PUBLIC_HOSTNAME_PASSTHROUGH}
      - SWE_PREVIEW_PORTS=${PREVIEW_PORTS}
      - SWE_AGENT_CHAT_PORTS=${AGENT_CHAT_PORTS}
      - SWE_PUBLIC_PORTS=${PUBLIC_PORTS}
      - SWE_CDP_PORTS=${CDP_PORTS}
      - SWE_VNC_PORTS=${VNC_PORTS}
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
      - SWE_PREVIEW_PORTS=${PREVIEW_PORTS}
      - SWE_AGENT_CHAT_PORTS=${AGENT_CHAT_PORTS}
      - SWE_PUBLIC_PORTS=${PUBLIC_PORTS}
      - SWE_CDP_PORTS=${CDP_PORTS}
      - SWE_VNC_PORTS=${VNC_PORTS}
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
      - SWE_PREVIEW_PORTS=${PREVIEW_PORTS}
      - SWE_AGENT_CHAT_PORTS=${AGENT_CHAT_PORTS}
      - SWE_PUBLIC_PORTS=${PUBLIC_PORTS}
      - SWE_CDP_PORTS=${CDP_PORTS}
      - SWE_VNC_PORTS=${VNC_PORTS}
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
