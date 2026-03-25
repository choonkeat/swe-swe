#!/bin/bash
set -euo pipefail

# Tear down e2e test environment(s).
#
# Usage: ./scripts/e2e-down.sh [simple|compose]
#
# If mode given, tears down that mode only.
# If no mode given, tears down all running e2e environments.

MODE="${1:-}"

teardown_mode() {
    local mode="$1"
    local state_file="/workspace/tmp/e2e-${mode}/.e2e-state"

    if [[ ! -f "$state_file" ]]; then
        echo "No running e2e environment for mode: $mode"
        return 0
    fi

    # Read state
    local project_path
    project_path=$(grep "^PROJECT_PATH=" "$state_file" | cut -d= -f2-)

    echo "Tearing down e2e-${mode}..."
    if [[ -n "$project_path" && -d "$project_path" ]]; then
        cd "$project_path"
        docker compose down 2>/dev/null || true
    fi

    rm -f "$state_file"
    echo "  Done: e2e-${mode} torn down"
}

if [[ -n "$MODE" ]]; then
    if [[ "$MODE" != "simple" && "$MODE" != "compose" ]]; then
        echo "Usage: $0 [simple|compose]"
        exit 1
    fi
    teardown_mode "$MODE"
else
    # Tear down all
    for m in simple compose; do
        teardown_mode "$m"
    done
fi

echo "=== e2e-down complete ==="
