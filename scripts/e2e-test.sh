#!/bin/bash
set -euo pipefail

# Run Playwright e2e tests against running e2e environment(s).
#
# Usage: ./scripts/e2e-test.sh [simple|compose] [playwright-args...]
#
# If mode given, tests that mode only.
# If no mode given, tests all running e2e environments.
#
# Extra arguments are passed to `npx playwright test`.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_DIR="$(dirname "$SCRIPT_DIR")"
E2E_DIR="$WORKSPACE_DIR/e2e"

MODE="${1:-}"
PLAYWRIGHT_ARGS=()

# If first arg is a known mode, shift it off; otherwise test all
if [[ "$MODE" == "simple" || "$MODE" == "compose" ]]; then
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

    PORT="$port" \
    SWE_SWE_PASSWORD="$password" \
    E2E_BASE_URL="http://${host_ip}:${port}" \
        npx playwright test "${PLAYWRIGHT_ARGS[@]+"${PLAYWRIGHT_ARGS[@]}"}"

    echo "=== e2e-${mode}: PASSED ==="
}

FAILED=0

if [[ -n "$MODE" ]]; then
    test_mode "$MODE" || FAILED=1
else
    for m in simple compose; do
        test_mode "$m" || FAILED=1
    done
fi

if [[ "$FAILED" -ne 0 ]]; then
    echo "=== e2e-test: SOME TESTS FAILED ==="
    exit 1
fi

echo "=== e2e-test complete ==="
