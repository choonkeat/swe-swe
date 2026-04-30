#!/bin/bash
set -euo pipefail

# Bring up a manual tunnel-mode test container against
# https://tunnel.example.com. Verifies that the {{IF TUNNEL}} branch of
# the Dockerfile actually fetches and installs swe-swe-tunnel and that
# the supervisor can spawn it end-to-end.
#
# Why a separate target instead of folding into make e2e: the existing
# e2e flow only passes a fake SWE_PUBLIC_HOSTNAME env -- it never builds
# the real swe-swe-tunnel binary into the image and never spawns the
# supervisor child. This script does both.
#
# Tear down with: ./scripts/tunnel-down-manual.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(dirname "$SCRIPT_DIR")"

TEST_STACK_DIR="${WORKSPACE_DIR}/tmp/tunnel-manual"
EFFECTIVE_HOME="${WORKSPACE_DIR}/tmp/tunnel-manual-home"

# Offset port ranges so we don't collide with the live stack (3000-3019)
# or the existing e2e modes (3100/3200/3300/...).
PREVIEW_PORTS="${PREVIEW_PORTS:-3500-3529}"
AGENT_CHAT_PORTS="${AGENT_CHAT_PORTS:-4500-4529}"
PUBLIC_PORTS="${PUBLIC_PORTS:-5500-5529}"
CDP_PORTS="${CDP_PORTS:-6500-6529}"
VNC_PORTS="${VNC_PORTS:-7500-7529}"

TUNNEL_SERVER_URL="${TUNNEL_SERVER_URL:-https://tunnel.example.com}"
# Default to a stable label so reruns reuse the same tunnel identity
# (avoids burning the per-pubkey new-unique rate limit on each run).
# Override with SWE_TUNNEL_UNIQUE=foo for ad-hoc runs.
SWE_TUNNEL_UNIQUE="${SWE_TUNNEL_UNIQUE:-swe-swe-manual}"
SWE_SWE_PASSWORD="${SWE_SWE_PASSWORD:-tunnel-manual-password}"

echo "=== tunnel-up-manual ==="
echo "  TUNNEL_SERVER_URL=${TUNNEL_SERVER_URL}"
echo "  SWE_TUNNEL_UNIQUE=${SWE_TUNNEL_UNIQUE}"
echo "  TEST_STACK_DIR=${TEST_STACK_DIR}"

# --- Phase 0: Detect host workspace path ---
# When this script runs inside a dev container that talks to the host
# Docker socket, bind-mount sources have to be the host path. We need
# this for cleanup as well as for the docker-compose override below.
detect_host_workspace() {
    local container_name
    container_name=$(docker ps --format "{{.Names}}" 2>/dev/null \
        | grep -E '^[^[:space:]]+-swe-swe-1$' \
        | grep -v "tunnel-manual\|test\|e2e" \
        | head -1)
    if [ -n "$container_name" ]; then
        docker inspect "$container_name" 2>/dev/null \
            | jq -r '.[0].Mounts[] | select(.Destination=="/workspace") | .Source' 2>/dev/null \
            || echo ""
    fi
}
HOST_WORKSPACE="${HOST_WORKSPACE:-$(detect_host_workspace)}"
SIBLING_DOCKER=0
if [ -n "${HOST_WORKSPACE}" ] && [ "${HOST_WORKSPACE}" != "/workspace" ]; then
    SIBLING_DOCKER=1
    HOST_TMP="${HOST_WORKSPACE}/tmp"
    echo "  Sibling-docker mode: HOST_WORKSPACE=${HOST_WORKSPACE}"
else
    HOST_TMP="${WORKSPACE_DIR}/tmp"
fi

# --- Phase 1: Build CLI ---
echo "--- Building CLI ---"
cd "${WORKSPACE_DIR}"
make build-cli

# --- Phase 2: Init project ---
echo "--- Initializing project ---"
# Previous failed runs may have left root-owned dirs (docker creates
# bind-mount sub-mount intermediates as root, which our app uid 1000
# can't remove). Wipe them via a privileged docker run first.
if [ -d "${TEST_STACK_DIR}" ] || [ -d "${EFFECTIVE_HOME}" ]; then
    docker run --rm \
        -v "${HOST_TMP}:/cleanup" \
        --entrypoint /bin/sh alpine:3.20 \
        -c "rm -rf /cleanup/tunnel-manual /cleanup/tunnel-manual-home" \
        2>/dev/null || true
fi
mkdir -p "${TEST_STACK_DIR}" "${EFFECTIVE_HOME}"

cd "${TEST_STACK_DIR}"
git init -q
git config user.email "tunnel-manual@test.local"
git config user.name "Tunnel Manual Test"
git commit -q --allow-empty -m "initial"

HOME="${EFFECTIVE_HOME}" "${WORKSPACE_DIR}/dist/swe-swe.linux-amd64" init \
    --project-directory="${TEST_STACK_DIR}" \
    --agents=opencode \
    --tunnel-server-url="${TUNNEL_SERVER_URL}" \
    --preview-ports="${PREVIEW_PORTS}" \
    --public-ports="${PUBLIC_PORTS}"

PROJECT_PATH=$(ls -d "${EFFECTIVE_HOME}/.swe-swe/projects/"*tunnel-manual* 2>/dev/null | head -1)
if [ -z "${PROJECT_PATH}" ]; then
    echo "ERROR: Could not find generated project directory under ${EFFECTIVE_HOME}/.swe-swe/projects/"
    exit 1
fi
[[ "${PROJECT_PATH}" == */ ]] || PROJECT_PATH="${PROJECT_PATH}/"
echo "Project: ${PROJECT_PATH}"

# Translate to host paths when running in sibling-docker mode.
if [ "${SIBLING_DOCKER}" = "1" ]; then
    HOST_TEST_STACK_DIR="${TEST_STACK_DIR/#\/workspace\//${HOST_WORKSPACE}/}"
    HOST_PROJECT_PATH="${PROJECT_PATH/#\/workspace\//${HOST_WORKSPACE}/}"
    echo "Host project   : ${HOST_PROJECT_PATH}"
else
    HOST_TEST_STACK_DIR="${TEST_STACK_DIR}"
    HOST_PROJECT_PATH="${PROJECT_PATH}"
fi

# --- Phase 3: Configure for the manual run ---
echo "--- Configuring ---"
ENV_FILE="${PROJECT_PATH}.env"
cat >> "${ENV_FILE}" <<EOF
SWE_SWE_PASSWORD=${SWE_SWE_PASSWORD}
SWE_TUNNEL_UNIQUE=${SWE_TUNNEL_UNIQUE}
SWE_AGENT_CHAT_PORTS=${AGENT_CHAT_PORTS}
SWE_CDP_PORTS=${CDP_PORTS}
SWE_VNC_PORTS=${VNC_PORTS}
WORKSPACE_DIR=${HOST_TEST_STACK_DIR}
EOF

# Persist the tunnel client's Ed25519 identity across runs in a named
# volume so we don't burn the per-pubkey new-unique rate limit and so
# the server recognizes us via signed-identity reclaim. The volume is
# created out-of-band the first time and survives `docker compose down`
# (it's declared external).
TUNNEL_IDENTITY_VOLUME="${TUNNEL_IDENTITY_VOLUME:-swe-swe-tunnel-manual-identity}"
if ! docker volume inspect "${TUNNEL_IDENTITY_VOLUME}" >/dev/null 2>&1; then
    echo "  Creating identity volume ${TUNNEL_IDENTITY_VOLUME} (first run)"
    docker volume create "${TUNNEL_IDENTITY_VOLUME}" >/dev/null
fi
# chown the volume to uid 1000 so the tunnel client (running as app)
# can write identity.key on first use. Idempotent -- second-run
# identity.key is already 1000:1000.
docker run --rm --user 0 -v "${TUNNEL_IDENTITY_VOLUME}":/v alpine:3.20 \
    chown 1000:1000 /v >/dev/null 2>&1 || true

# Override file: rewrite bind mounts to host paths (sibling-docker
# only) and always attach the identity volume.
{
    if [ "${SIBLING_DOCKER}" = "1" ]; then
        cat <<EOF
# Auto-generated by tunnel-up-manual.sh.
services:
  swe-swe:
    volumes:
      - ${HOST_TEST_STACK_DIR}:/workspace
      - ${HOST_TEST_STACK_DIR}/.swe-swe/worktrees:/worktrees
      - ${HOST_PROJECT_PATH}home:/home/app
      - ${TUNNEL_IDENTITY_VOLUME}:/home/app/.swe-swe-tunnel
volumes:
  ${TUNNEL_IDENTITY_VOLUME}:
    external: true
EOF
    else
        cat <<EOF
# Auto-generated by tunnel-up-manual.sh.
services:
  swe-swe:
    volumes:
      - ${TUNNEL_IDENTITY_VOLUME}:/home/app/.swe-swe-tunnel
volumes:
  ${TUNNEL_IDENTITY_VOLUME}:
    external: true
EOF
    fi
} > "${PROJECT_PATH}docker-compose.override.yml"

# --- Phase 4: Pre-create home/.swe-swe as app-owned ---
# docker-compose's `${HOME}/.swe-swe/proxy:/home/app/.swe-swe/proxy`
# sub-mount needs /home/app/.swe-swe/ to exist inside the parent home
# bind. Without this, the daemon creates the intermediate directory as
# root, and the container's entrypoint (running as app) can't write
# beneath it. Pre-create it as the running user so ownership matches.
mkdir -p "${PROJECT_PATH}home/.swe-swe/bin" "${PROJECT_PATH}home/.swe-swe/proxy"

# --- Phase 5: Up ---
echo "--- docker compose up (build+start) ---"
DC=$(command -v docker-compose >/dev/null 2>&1 && echo "docker-compose" || echo "docker compose")
cd "${PROJECT_PATH}"
${DC} up -d --build

echo
echo "=== Up complete ==="
echo "Project dir : ${PROJECT_PATH}"
echo "Tail logs   : cd ${PROJECT_PATH} && ${DC} logs -f swe-swe"
echo "Public URL  : check logs for 'hostname=' line, then visit https://9898.<hostname>"
echo "Tear down   : ./scripts/tunnel-down-manual.sh"
