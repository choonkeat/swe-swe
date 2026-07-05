#!/bin/bash
set -euo pipefail

# Run Playwright e2e tests against running e2e environment(s).
#
# Usage: ./scripts/e2e-test.sh [simple|compose|docker] [playwright-args...]
#
# If mode given, tests that mode only.
# If no mode given, tests all running e2e environments.
#
# Extra arguments are passed to `npx playwright test`.
# Runs docker system prune before and after tests to prevent disk exhaustion.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(dirname "$SCRIPT_DIR")"
E2E_DIR="$WORKSPACE_DIR/e2e"

MODE="${1:-}"
PLAYWRIGHT_ARGS=()

# Probe whether a chromium binary can actually launch. Some hosts ship a
# chromium build whose zygote crashes on this kernel (SIGTRAP on startup),
# which would fail every spec in global-setup before any test runs. Returns
# 0 if a headless about:blank render succeeds.
chromium_launches() {
    local bin="$1"
    [[ -x "$bin" ]] || return 1
    local prof
    prof="$(mktemp -d)"
    timeout 25 "$bin" --headless --no-sandbox --disable-gpu \
        --disable-dev-shm-usage --user-data-dir="$prof" \
        --dump-dom about:blank >/dev/null 2>&1
    local rc=$?
    rm -rf "$prof"
    return $rc
}

# Resolve a working chromium into CHROMIUM_BIN (consumed by playwright.config.js
# + global-setup.js). Honors an explicit CHROMIUM_BIN; otherwise uses the system
# chromium when it launches, and falls back to a Playwright-bundled chromium when
# it doesn't. Leaves CHROMIUM_BIN unset (harness default) if nothing is probed.
resolve_chromium() {
    if [[ -n "${CHROMIUM_BIN:-}" ]]; then
        echo "--- Using CHROMIUM_BIN=$CHROMIUM_BIN (explicit) ---"
        export CHROMIUM_BIN
        return
    fi
    if chromium_launches /usr/bin/chromium; then
        echo "--- Using system /usr/bin/chromium ---"
        return
    fi
    echo "--- /usr/bin/chromium failed to launch; probing Playwright-bundled chromium ---"
    local cand
    # Newest bundled build first.
    for cand in $(ls -dt "$HOME"/.cache/ms-playwright/chromium-*/chrome-linux*/chrome 2>/dev/null); do
        if chromium_launches "$cand"; then
            export CHROMIUM_BIN="$cand"
            echo "--- Using CHROMIUM_BIN=$CHROMIUM_BIN (fallback) ---"
            return
        fi
    done
    echo "--- WARNING: no working chromium found; proceeding with harness default ---"
}

# If first arg is a known mode, shift it off; otherwise test all
if [[ "$MODE" == "simple" || "$MODE" == "compose" || "$MODE" == "docker" ]]; then
    shift
    PLAYWRIGHT_ARGS=("$@")
else
    MODE=""
    PLAYWRIGHT_ARGS=("$@")
fi

test_mode() {
    local mode="$1"
    local state_file="/workspace/tmp/e2e-${mode}/.e2e-state"

    if [[ ! -f "$state_file" ]]; then
        echo "SKIP: No running e2e environment for mode: $mode"
        return 0
    fi

    # Read state
    local port password host_ip
    port=$(grep "^PORT=" "$state_file" | cut -d= -f2-)
    password=$(grep "^PASSWORD=" "$state_file" | cut -d= -f2-)
    host_ip=$(grep "^HOST_IP=" "$state_file" | cut -d= -f2-)

    echo "=== Testing e2e-${mode} at http://${host_ip}:${port}/ ==="

    cd "$E2E_DIR"
    npm install --silent 2>/dev/null

    local rc=0
    PORT="$port" \
    SWE_SWE_PASSWORD="$password" \
    E2E_BASE_URL="http://${host_ip}:${port}" \
    SWE_PUBLIC_HOSTNAME="${SWE_PUBLIC_HOSTNAME:-}" \
    CHROMIUM_BIN="${CHROMIUM_BIN:-}" \
        npx playwright test "${PLAYWRIGHT_ARGS[@]+"${PLAYWRIGHT_ARGS[@]}"}" || rc=$?

    if [[ "$rc" -ne 0 ]]; then
        echo "=== e2e-${mode}: FAILED (exit $rc) ==="
        return "$rc"
    fi

    echo "=== e2e-${mode}: PASSED ==="
}

# Prune unused Docker resources to prevent disk exhaustion
echo "--- Pruning Docker resources ---"
docker system prune -f 2>/dev/null || true

# Pick a chromium that actually launches on this host.
resolve_chromium

FAILED=0

if [[ -n "$MODE" ]]; then
    test_mode "$MODE" || FAILED=1
else
    for m in simple compose docker; do
        test_mode "$m" || FAILED=1
    done
fi

# Prune again after tests
echo "--- Pruning Docker resources ---"
docker system prune -f 2>/dev/null || true

if [[ "$FAILED" -ne 0 ]]; then
    echo "=== e2e-test: SOME TESTS FAILED ==="
    exit 1
fi

echo "=== e2e-test complete ==="
