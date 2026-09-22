#!/bin/bash
set -euo pipefail

# Run Playwright e2e tests against running e2e environment(s).
#
# Usage: ./scripts/e2e-test.sh [simple|compose|docker|single-port] [playwright-args...]
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
if [[ "$MODE" == "simple" || "$MODE" == "compose" || "$MODE" == "docker" || "$MODE" == "single-port" ]]; then
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
    local port password host_ip scheme
    port=$(grep "^PORT=" "$state_file" | cut -d= -f2-)
    password=$(grep "^PASSWORD=" "$state_file" | cut -d= -f2-)
    host_ip=$(grep "^HOST_IP=" "$state_file" | cut -d= -f2-)
    # SCHEME is written by e2e-up.sh (compose = https via selfsign Traefik,
    # simple/docker = http). Default http for older state files.
    scheme=$(grep "^SCHEME=" "$state_file" | cut -d= -f2-)
    scheme=${scheme:-http}

    echo "=== Testing e2e-${mode} at ${scheme}://${host_ip}:${port}/ ==="

    cd "$E2E_DIR"
    npm install --silent 2>/dev/null

    # Wildcard mode: the app must be REACHED at the apex, not just configured
    # with it. The frontend only builds "{port}.{apex}" links when the page it
    # is running in was itself loaded from that domain (terminal-ui.js,
    # effectivePublicHostname) -- and the login cookie is only issued with
    # Domain=.{apex} on that path. Loading at the container's own host instead
    # leaves every subdomain URL untested, which is how the subdomain branch of
    # tunnel.spec.js sat unrun for so long.
    #
    # E2E_RESOLVE_IP is where the browser must actually send those names;
    # playwright.config.js turns it into a resolver rule, so no real DNS for
    # the apex is required (and lvh.me's real record -- 127.0.0.1 -- would be
    # the wrong machine anyway).
    local base_host="$host_ip"
    local forwarder_pid=""
    if [[ -n "${SWE_PUBLIC_HOSTNAME:-}" ]]; then
        base_host="$SWE_PUBLIC_HOSTNAME"
        echo "    wildcard mode: reaching the app at ${scheme}://${base_host}:${port} (resolver-pinned to ${host_ip})"
        # Chromium is pinned by --host-resolver-rules, but Playwright's Node
        # side (page.request) uses the OS resolver, and lvh.me's real record --
        # 127.0.0.1 -- is this runner's own loopback, not the stack. /etc/hosts
        # is root-owned here, so forward the one port Node talks to instead.
        # Pane traffic is all chromium and never comes through this.
        if ! curl -s --max-time 2 -o /dev/null "http://127.0.0.1:${port}/swe-swe-auth/login"; then
            python3 "$SCRIPT_DIR/e2e-apex-forwarder.py" "$port" "$host_ip" &
            forwarder_pid=$!
            for _ in $(seq 1 20); do
                curl -s --max-time 1 -o /dev/null "http://127.0.0.1:${port}/swe-swe-auth/login" && break
                sleep 0.5
            done
        fi
    fi

    # Single-port tier: the specs that test DISCOVERY, or that need the
    # per-port listeners, skip themselves on this. The new single-port.spec.js
    # is the one that needs it set.
    local single_port=""
    if [[ "$mode" == "single-port" ]]; then
        single_port=1
    fi

    local rc=0
    E2E_SINGLE_PORT="$single_port" \
    E2E_PREVIEW_PORTS="$(grep "^PREVIEW_PORTS=" "$state_file" | cut -d= -f2-)" \
    E2E_AGENT_CHAT_PORTS="$(grep "^AGENT_CHAT_PORTS=" "$state_file" | cut -d= -f2-)" \
    E2E_VNC_PORTS="$(grep "^VNC_PORTS=" "$state_file" | cut -d= -f2-)" \
    PORT="$port" \
    SWE_SWE_PASSWORD="$password" \
    E2E_BASE_URL="${scheme}://${base_host}:${port}" \
    E2E_RESOLVE_IP="${host_ip}" \
    SWE_PUBLIC_HOSTNAME="${SWE_PUBLIC_HOSTNAME:-}" \
    CHROMIUM_BIN="${CHROMIUM_BIN:-}" \
        npx playwright test "${PLAYWRIGHT_ARGS[@]+"${PLAYWRIGHT_ARGS[@]}"}" || rc=$?

    [[ -n "$forwarder_pid" ]] && kill "$forwarder_pid" 2>/dev/null

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
    for m in simple compose docker single-port; do
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
