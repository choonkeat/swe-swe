#!/bin/bash
#
# Host-native (dockerless) e2e: builds the CLI, runs `swe-swe init --dockerless`
# in a throwaway git repo, boots `swe-swe up` directly (NO Docker daemon), and
# asserts the dockerless contract -- dumped payload, generated wiring, and the
# server actually serving the homepage + a session page rooted at the project.
#
# Designed for a CLEAN Linux host / CI. On a box that already runs another
# swe-swe (e.g. the dogfood container), the global abstract socket
# `@swe-swe-broker` and shared ports collide; the core assertions below avoid
# those and still pass, while per-session proxy/md-serve binding is best-effort
# (warns, does not fail) since it needs a clean host.
#
# Usage: ./scripts/e2e-dockerless.sh
set -uo pipefail

WORKSPACE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
E2E_PORT="${E2E_PORT:-19790}"
TEST_DIR="$(mktemp -d "${TMPDIR:-/tmp}/swe-dockerless-e2e.XXXXXX")"
HOME_DIR="$TEST_DIR/home"
UUID="e2e00000-0000-4000-8000-000000000001"
CLI="$WORKSPACE_DIR/dist/swe-swe.$(go env GOOS)-$(go env GOARCH)"
SERVER_PID=""
FAILS=0

pass() { echo "  [PASS] $1"; }
warn() { echo "  [WARN] $1"; }
fail() { echo "  [FAIL] $1"; FAILS=$((FAILS + 1)); }

cleanup() {
    echo "=== Cleanup ==="
    [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
    # The server is exec'd as a child of `swe-swe up`; kill the whole group.
    pkill -f "bin/swe-swe-server.*127.0.0.1:$E2E_PORT" 2>/dev/null || true
    rm -rf "$TEST_DIR"
}
trap cleanup EXIT

echo "=== Phase 1: Build CLI (+ dockerless payload) ==="
cd "$WORKSPACE_DIR"
make build-cli >/dev/null
[ -x "$CLI" ] || { echo "CLI not built at $CLI"; exit 1; }

echo "=== Phase 2: init --dockerless in a throwaway repo ==="
mkdir -p "$TEST_DIR/proj" "$HOME_DIR"
PROJ="$TEST_DIR/proj"
( cd "$PROJ" && git init -q && git config user.email e2e@test.local && git config user.name e2e \
    && printf '# e2e\nhello dockerless\n' > README.md && git add -A && git commit -qm init )
HOME="$HOME_DIR" "$CLI" init --dockerless --project-directory "$PROJ" >/dev/null
SWEDIR="$(ls -d "$HOME_DIR"/.swe-swe/projects/*/ | head -1)"
echo "  metadata dir: $SWEDIR"

echo "=== Phase 3: assert generated artifacts ==="
for b in swe-swe-server git-credential-swe-swe git-sign-swe-swe mcp-lazy-init swe-swe-broker-probe swe-swe-fork-convo swe-swe-tunnel; do
    [ -x "$SWEDIR/bin/$b" ] && pass "binary $b dumped + executable" || fail "binary $b missing/not executable"
done
[ -x "$SWEDIR/bin/swe-swe-open" ] && pass "swe-swe-open shim present" || fail "swe-swe-open shim missing"
for n in xdg-open open x-www-browser www-browser sensible-browser; do
    [ "$(readlink "$SWEDIR/bin/$n")" = "swe-swe-open" ] || fail "symlink $n -> swe-swe-open missing"
done
[ -z "$(for n in xdg-open open x-www-browser www-browser sensible-browser; do [ "$(readlink "$SWEDIR/bin/$n")" = swe-swe-open ] || echo x; done)" ] && pass "xdg-open/open/... symlinks present"
[ "$(cat "$SWEDIR/mode")" = "dockerless" ] && pass "mode marker = dockerless" || fail "mode marker wrong/missing"
grep -q '"mcpServers"' "$PROJ/.mcp.json" && grep -q 'swe-swe-agent-chat' "$PROJ/.mcp.json" \
    && pass "project .mcp.json has MCP servers" || fail ".mcp.json missing/incomplete"

echo "=== Phase 4: boot the dumped server (no Docker) ==="
# Clean env: drop any ambient tailscale/tunnel/auth from the host; pick high,
# unlikely-to-collide port ranges so a stray neighbour does not clash.
# `swe-swe up` locates the project by CWD, so launch from inside it.
( cd "$PROJ" && exec env -u TS_AUTHKEY -u TS_HOSTNAME -u TS_STATE_DIR \
    -u SWE_TUNNEL_SERVER_URL -u SWE_TUNNEL_UNIQUE -u SWE_TUNNEL_BIN -u SWE_TUNNEL_CLIENT_CERT \
    -u SWE_SWE_PASSWORD \
    HOME="$HOME_DIR" SWE_PORT="$E2E_PORT" \
    SWE_PREVIEW_PORTS=41000-41019 SWE_AGENT_CHAT_PORTS=41100-41119 \
    SWE_PUBLIC_PORTS=41200-41219 SWE_CDP_PORTS=41300-41319 SWE_VNC_PORTS=41400-41419 \
    SWE_PROXY_PORT_OFFSET=10000 \
    "$CLI" up ) > "$TEST_DIR/up.log" 2>&1 &
SERVER_PID=$!

ready=0
for _ in $(seq 1 60); do
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "http://127.0.0.1:$E2E_PORT/" 2>/dev/null)"
    [ "$code" = "200" ] && { ready=1; break; }
    kill -0 "$SERVER_PID" 2>/dev/null || { echo "server exited early; log:"; tail -20 "$TEST_DIR/up.log"; exit 1; }
    sleep 1
done
[ "$ready" = 1 ] && pass "server serves homepage (200)" || { fail "server never became ready"; tail -20 "$TEST_DIR/up.log"; }

echo "=== Phase 5: assert tab-serving endpoints ==="
# Homepage identifies as swe-swe.
curl -s "http://127.0.0.1:$E2E_PORT/" | grep -q '<title>swe-swe</title>' \
    && pass "homepage is swe-swe" || fail "homepage title wrong"

# Session page renders the terminal-ui element rooted at OUR project dir
# (proves the path-agnostic server: Agent Terminal/Terminal/Preview/Files/Chat
# all hang off this page + per-session env).
SESS="$(curl -s "http://127.0.0.1:$E2E_PORT/session/$UUID?assistant=shell")"
echo "$SESS" | grep -q '<terminal-ui' && pass "session page renders terminal-ui" || fail "session page missing terminal-ui"
echo "$SESS" | grep -qF "data-where-key=\"$PROJ\"" && pass "session rooted at project dir (path-agnostic)" || fail "session not rooted at project dir"
echo "$SESS" | grep -q '/terminal-ui.js' && pass "session loads terminal-ui.js bundle" || fail "terminal-ui.js not referenced"

# Files tab backend (md-serve via npx) is best-effort: needs npm/network and a
# free files port. Warn (not fail) so the harness stays green on shared hosts.
sleep 3
if grep -q 'Started md-serve' "$TEST_DIR/up.log"; then
    if grep -q 'md-serve exited with error' "$TEST_DIR/up.log"; then
        warn "md-serve started but exited (port clash on shared host?) -- check on a clean box"
    else
        pass "Files md-serve launched"
    fi
else
    warn "md-serve not observed yet (lazy/clean-host dependent)"
fi

echo "=== Phase 6: Playwright live-tab coverage ==="
# Drives the actual tabs over a real browser/websocket -- the parts curl
# cannot reach (PTY transport, md-serve via npx, preview proxy wiring).
# Needs chromium + an agent CLI (opencode) on the host; set
# E2E_SKIP_PLAYWRIGHT=1 to run the curl-only contract on a bare runner.
if [ "${E2E_SKIP_PLAYWRIGHT:-}" = "1" ]; then
    warn "Playwright live-tab suite skipped (E2E_SKIP_PLAYWRIGHT=1)"
else
    if ( cd "$WORKSPACE_DIR/e2e" && npm install --silent 2>/dev/null \
            && E2E_DOCKERLESS=1 E2E_BASE_URL="http://127.0.0.1:$E2E_PORT" \
               npx playwright test dockerless-tabs.spec.js ); then
        pass "Playwright live-tab suite passed"
    else
        fail "Playwright live-tab suite failed"
    fi
fi

echo "=== Result ==="
if [ "$FAILS" -eq 0 ]; then
    echo "DOCKERLESS E2E: PASS"
else
    echo "DOCKERLESS E2E: $FAILS assertion(s) FAILED"
    exit 1
fi
