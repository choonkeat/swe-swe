#!/bin/bash
set -euo pipefail

# Bring up an e2e test environment in the specified mode.
#
# Usage: ./scripts/e2e-up.sh <simple|compose|docker>
#
# simple  - dockerfile-only mode (no Traefik), port 9780
# compose - Traefik compose mode, port 9770
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
    E2E_PORT=${E2E_PORT:-9780}
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
    # compose mode uses no extra init flags. (`--with-vscode` was never a real
    # init flag -- passing it made `swe-swe init` abort with "flag provided but
    # not defined", breaking `make e2e-up-compose`.)
    INIT_EXTRA_FLAGS=""
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
    --agents="${SWE_SWE_E2E_AGENTS:-opencode}" \
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
#
# SWE_TUNNEL_VIA selects how the server learns its hostname:
#   "env"        (default) -- pass SWE_PUBLIC_HOSTNAME to the container
#   "state-file" -- write /workspace/.swe-swe/tunnel-state.json before
#                   container start; do NOT pass SWE_PUBLIC_HOSTNAME env.
#                   Server picks up hostname via the file fallback path.
# Playwright assertions in tunnel.spec.js read SWE_PUBLIC_HOSTNAME on the
# RUNNER (set by e2e-test.sh) for the "expected" value; the same hostname
# is the one written into the state file in this branch.
SWE_TUNNEL_VIA="${SWE_TUNNEL_VIA:-env}"
SWE_PUBLIC_HOSTNAME_PASSTHROUGH="${SWE_PUBLIC_HOSTNAME:-}"
if [[ "$SWE_TUNNEL_VIA" == "state-file" && -n "$SWE_PUBLIC_HOSTNAME_PASSTHROUGH" ]]; then
    echo "  Tunnel mode: state-file (writing tunnel-state.json, NOT passing SWE_PUBLIC_HOSTNAME env)"
    mkdir -p "${TEST_STACK_DIR}/.swe-swe"
    cat > "${TEST_STACK_DIR}/.swe-swe/tunnel-state.json" <<TUNNEL_STATE_JSON
{"hostname":"${SWE_PUBLIC_HOSTNAME_PASSTHROUGH}","unique":"e2e","registered_at":"2026-04-28T00:00:00Z"}
TUNNEL_STATE_JSON
    chmod 0600 "${TEST_STACK_DIR}/.swe-swe/tunnel-state.json"
    SWE_PUBLIC_HOSTNAME_PASSTHROUGH=""
fi

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
# Defensive env override: docker compose's variable substitution lets the
# parent shell beat .env file values (a process-env-takes-precedence
# rule, not a docker-compose-specific bug). Without this, running e2e on
# a host that has the live swe-swe stack exported would silently rebind
# the e2e container to the live ports (typically SWE_PORT=1977 + the
# 3000 / 4000 / 5000 / 6000 / 7000 ranges). The leak triggers a confusing
# "Bind for 0.0.0.0:1977 failed: port is already allocated" 100% of the
# time when the live and e2e stacks run side-by-side.
#
# We don't `unset` -- the user may legitimately want SWE_PORT in their
# shell. Instead we override on the docker compose call alone so the
# substitution sees our values, not theirs.
COMPOSE_ENV=(
    "SWE_PORT=$E2E_PORT"
    "SWE_SWE_PASSWORD=$E2E_PASSWORD"
    "SWE_PREVIEW_PORTS=$PREVIEW_PORTS"
    "SWE_AGENT_CHAT_PORTS=$AGENT_CHAT_PORTS"
    "SWE_PUBLIC_PORTS=$PUBLIC_PORTS"
    "SWE_CDP_PORTS=$CDP_PORTS"
    "SWE_VNC_PORTS=$VNC_PORTS"
    "SWE_PROXY_PORT_OFFSET=20000"
    "WORKSPACE_DIR=$HOST_TEST_STACK_DIR"
)

echo "--- Starting container ---"
if [[ "$MODE" == "compose" ]]; then
    # Only start swe-swe + traefik to save memory (skip vscode, chrome, etc.)
    env "${COMPOSE_ENV[@]}" docker compose up -d swe-swe traefik
else
    env "${COMPOSE_ENV[@]}" docker compose up -d
fi

# Wait for server to be ready.
#
# Two-phase probe:
#   1. Any HTTP response on /          -- container is listening
#   2. /swe-swe-auth/login returns 200 -- swe-swe-server is fully wired up
#
# Compose mode runs Traefik in front of swe-swe-server; until the backend
# is reachable Traefik returns 502 Bad Gateway on /swe-swe-auth/login, but
# / can already return 302 (redirect to login). The old probe accepted any
# 1xx-5xx code on /, which let test-e2e race the playwright globalSetup
# against an unready stack -- the login page would 502 and Playwright would
# timeout looking for the password input. Requiring 200 on the login route
# is the actual precondition for our test setup.
echo "Waiting for server..."
HOST_IP="${HOST_IP:-host.docker.internal}"
SERVER_READY=false
LAST_ROOT=""
LAST_LOGIN=""
for i in $(seq 1 60); do
    LAST_ROOT=$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 "http://$HOST_IP:$E2E_PORT/" 2>/dev/null) || LAST_ROOT="000"
    LAST_LOGIN=$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 "http://$HOST_IP:$E2E_PORT/swe-swe-auth/login" 2>/dev/null) || LAST_LOGIN="000"
    if [[ "$LAST_LOGIN" == "200" ]]; then
        echo "Server ready (/=$LAST_ROOT, /swe-swe-auth/login=$LAST_LOGIN) after ${i}s"
        SERVER_READY=true
        break
    fi
    sleep 1
done

if [[ "$SERVER_READY" != "true" ]]; then
    echo "ERROR: Server did not become ready within 60s (last /=$LAST_ROOT, /swe-swe-auth/login=$LAST_LOGIN)"
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
