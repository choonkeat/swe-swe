#!/bin/bash
set -euo pipefail

# Tear down the manual tunnel-mode test container started by
# ./scripts/tunnel-up-manual.sh. Removes containers + named volumes.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(dirname "$SCRIPT_DIR")"
EFFECTIVE_HOME="${WORKSPACE_DIR}/tmp/tunnel-manual-home"

PROJECT_PATH=$(ls -d "${EFFECTIVE_HOME}/.swe-swe/projects/"*tunnel-manual* 2>/dev/null | head -1 || true)
if [ -z "${PROJECT_PATH:-}" ]; then
    echo "No tunnel-manual project found under ${EFFECTIVE_HOME}/.swe-swe/projects/. Nothing to do."
    exit 0
fi
[[ "${PROJECT_PATH}" == */ ]] || PROJECT_PATH="${PROJECT_PATH}/"

DC=$(command -v docker-compose >/dev/null 2>&1 && echo "docker-compose" || echo "docker compose")

echo "=== tunnel-down-manual ==="
echo "Project: ${PROJECT_PATH}"
cd "${PROJECT_PATH}"
${DC} down -v --remove-orphans

# Detect host path and clean up root-owned leftovers via privileged
# docker run. Same logic as tunnel-up-manual.sh.
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
if [ -n "${HOST_WORKSPACE}" ] && [ "${HOST_WORKSPACE}" != "/workspace" ]; then
    HOST_TMP="${HOST_WORKSPACE}/tmp"
else
    HOST_TMP="${WORKSPACE_DIR}/tmp"
fi

echo "--- Wiping ${WORKSPACE_DIR}/tmp/tunnel-manual{,-home} ---"
docker run --rm \
    -v "${HOST_TMP}:/cleanup" \
    --entrypoint /bin/sh alpine:3.20 \
    -c "rm -rf /cleanup/tunnel-manual /cleanup/tunnel-manual-home" \
    2>/dev/null || true

echo "Down complete."
