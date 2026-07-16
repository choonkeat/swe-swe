#!/bin/bash
#
# Agent View REMOTE e2e: proves a lean dockerless swe-swe host can offload
# Agent View to a browser-backend and the tab actually works end-to-end --
# allocation over the API, CDP reverse-proxy, VNC proxy to the remote
# websockify, vnc-ready probe, and the noVNC canvas rendering in the UI.
#
# Two backend tiers, selected by E2E_AV_BACKEND:
#   binary (default) -- run the dumped swe-swe-server -mode browser-backend
#                       directly on this host (no Docker needed; needs the
#                       display stack: Xvfb/chromium/x11vnc/websockify)
#   image            -- build + run the docker/browser-backend image (needs
#                       Docker; exercises the Dockerfile itself)
#
# Usage: ./scripts/e2e-agent-view-remote.sh
#        E2E_AV_BACKEND=image ./scripts/e2e-agent-view-remote.sh
set -uo pipefail

WORKSPACE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
E2E_PORT="${E2E_PORT:-19833}"
BACKEND_PORT="${BACKEND_PORT:-19844}"
E2E_AV_BACKEND="${E2E_AV_BACKEND:-binary}"
# High, unlikely-to-collide ranges; distinct pools for the instance's own
# (unused-in-remote-mode) allocator vs the backend's real one.
INSTANCE_CDP=42300-42309
INSTANCE_VNC=42400-42409
BACKEND_CDP=42500-42509
BACKEND_VNC=42600-42609
TOKEN="e2e-agent-view-secret"
TEST_DIR="$(mktemp -d "${TMPDIR:-/tmp}/swe-agent-view-e2e.XXXXXX")"
HOME_DIR="$TEST_DIR/home"
CLI="$WORKSPACE_DIR/dist/swe-swe.$(go env GOOS)-$(go env GOARCH)"
SCREENSHOT_DIR="${E2E_SCREENSHOT_DIR:-$WORKSPACE_DIR/e2e/test-results/agent-view}"
SERVER_PID=""
BACKEND_PID=""
BACKEND_CONTAINER=""
MARKER_PID=""
MARKER_PORT=42999
FAILS=0

pass() { echo "  [PASS] $1"; }
warn() { echo "  [WARN] $1"; }
fail() { echo "  [FAIL] $1"; FAILS=$((FAILS + 1)); }

cleanup() {
    echo "=== Cleanup ==="
    [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
    pkill -f "bin/swe-swe-server.*127.0.0.1:$E2E_PORT" 2>/dev/null || true
    [ -n "$BACKEND_PID" ] && kill "$BACKEND_PID" 2>/dev/null || true
    [ -n "$BACKEND_CONTAINER" ] && docker rm -f "$BACKEND_CONTAINER" >/dev/null 2>&1 || true
    [ -n "$MARKER_PID" ] && kill "$MARKER_PID" 2>/dev/null || true
    rm -rf "$TEST_DIR"
}
trap cleanup EXIT

echo "=== Phase 1: Build CLI (+ dockerless payload) ==="
cd "$WORKSPACE_DIR"
make build-cli >/dev/null
[ -x "$CLI" ] || { echo "CLI not built at $CLI"; exit 1; }

echo "=== Phase 2: init --dockerless in a throwaway repo ==="
mkdir -p "$TEST_DIR/proj" "$HOME_DIR" "$SCREENSHOT_DIR"
PROJ="$TEST_DIR/proj"
( cd "$PROJ" && git init -q && git config user.email e2e@test.local && git config user.name e2e \
    && printf '# e2e\nagent view remote\n' > README.md && git add -A && git commit -qm init )
HOME="$HOME_DIR" "$CLI" init --dockerless --project-directory "$PROJ" >/dev/null
SWEDIR="$(ls -d "$HOME_DIR"/.swe-swe/projects/*/ | head -1)"
echo "  metadata dir: $SWEDIR"

echo "=== Phase 3: start the browser-backend ($E2E_AV_BACKEND tier) ==="
LOCALHOST_NAV_PORT=""
BACKEND_HOST="127.0.0.1"
if [ "$E2E_AV_BACKEND" = "image" ]; then
    make browser-backend-image >/dev/null || { fail "browser-backend image build"; exit 1; }
    # --network=host: the service must be dialable at the SAME host:port from
    # this harness AND from browsers/vnc-clients; per-range -p publishing binds
    # the HOST loopback, which is unreachable when the harness itself runs
    # inside a container (the dogfood box). Host networking works in both
    # environments; ports are high + short-lived, and the API is token-gated.
    # When the harness runs in a container, the host-network service is at the
    # default-route gateway; on a bare host it is plain loopback.
    if [ -f /.dockerenv ]; then
        BACKEND_HOST="$(ip route | awk '/default/{print $3}')"
        MARKER_BIND=""    # bind all: unpublished container ports stay private
    else
        MARKER_BIND="--bind 127.0.0.1"
    fi
    # localhost-resolution proof: the remote chromium must load THIS page via
    # http://localhost:<port>. In the containerized-harness case that is a
    # genuine cross-namespace hop (chromium in the host netns -> mapping ->
    # this harness's netns); its own localhost serves nothing on that port.
    mkdir -p "$TEST_DIR/marker"
    printf '<title>swe-swe-marker</title>cross-namespace localhost OK\n' > "$TEST_DIR/marker/index.html"
    # shellcheck disable=SC2086
    python3 -m http.server "$MARKER_PORT" $MARKER_BIND --directory "$TEST_DIR/marker" \
        > /dev/null 2>&1 &
    MARKER_PID=$!
    LOCALHOST_NAV_PORT="$MARKER_PORT"
    BACKEND_CONTAINER="swe-av-e2e-$$"
    # --hostname: host networking would otherwise inherit the HOST's hostname,
    # which then shows in the noVNC status bar (and thus in screenshots).
    docker run -d --name "$BACKEND_CONTAINER" --network=host --hostname browser-backend \
        -e SWE_PORT="$BACKEND_PORT" \
        -e SWE_CDP_PORTS="$BACKEND_CDP" -e SWE_VNC_PORTS="$BACKEND_VNC" \
        -e SWE_BROWSER_BACKEND_TOKEN="$TOKEN" \
        swe-swe/browser-backend >/dev/null || { fail "backend container start"; exit 1; }
else
    env SWE_CDP_PORTS="$BACKEND_CDP" SWE_VNC_PORTS="$BACKEND_VNC" \
        SWE_BROWSER_BACKEND_TOKEN="$TOKEN" HOME="$HOME_DIR" \
        "$SWEDIR/bin/swe-swe-server" -mode browser-backend -bind "127.0.0.1:$BACKEND_PORT" \
        > "$TEST_DIR/backend.log" 2>&1 &
    BACKEND_PID=$!
fi

backend_ready=0
for _ in $(seq 1 30); do
    if curl -s --max-time 2 "http://$BACKEND_HOST:$BACKEND_PORT/health" | grep -q '"sessions"'; then
        backend_ready=1; break
    fi
    sleep 1
done
[ "$backend_ready" = 1 ] && pass "backend /health answering" \
    || { fail "backend never became healthy"; [ -f "$TEST_DIR/backend.log" ] && tail -10 "$TEST_DIR/backend.log"; exit 1; }

echo "=== Phase 4: boot the dockerless instance pointed at the backend ==="
( cd "$PROJ" && exec env \
    -u SWE_TUNNEL_SERVER_URL -u SWE_TUNNEL_UNIQUE -u SWE_TUNNEL_BIN -u SWE_TUNNEL_CLIENT_CERT \
    -u SWE_SWE_PASSWORD \
    HOME="$HOME_DIR" SWE_PORT="$E2E_PORT" \
    SWE_AGENT_VIEW="http://$BACKEND_HOST:$BACKEND_PORT" \
    SWE_BROWSER_BACKEND_TOKEN="$TOKEN" \
    SWE_PREVIEW_PORTS=42000-42019 SWE_AGENT_CHAT_PORTS=42100-42119 \
    SWE_PUBLIC_PORTS=42200-42219 SWE_CDP_PORTS="$INSTANCE_CDP" SWE_VNC_PORTS="$INSTANCE_VNC" \
    SWE_PROXY_PORT_OFFSET=11000 \
    "$CLI" up ) > "$TEST_DIR/up.log" 2>&1 &
SERVER_PID=$!

ready=0
for _ in $(seq 1 60); do
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "http://127.0.0.1:$E2E_PORT/" 2>/dev/null)"
    [ "$code" = "200" ] && { ready=1; break; }
    kill -0 "$SERVER_PID" 2>/dev/null || { echo "server exited early; log:"; tail -20 "$TEST_DIR/up.log"; exit 1; }
    sleep 1
done
[ "$ready" = 1 ] && pass "instance serves homepage (200)" || { fail "instance never became ready"; tail -20 "$TEST_DIR/up.log"; exit 1; }

echo "=== Phase 5: Playwright -- Agent View over the remote backend ==="
if ( cd "$WORKSPACE_DIR/e2e" && npm install --silent 2>/dev/null \
        && E2E_DOCKERLESS=1 E2E_AGENT_VIEW=1 E2E_BASE_URL="http://127.0.0.1:$E2E_PORT" \
           E2E_BACKEND_URL="http://$BACKEND_HOST:$BACKEND_PORT" \
           E2E_LOCALHOST_NAV_PORT="$LOCALHOST_NAV_PORT" \
           E2E_SCREENSHOT_DIR="$SCREENSHOT_DIR" \
           npx playwright test agent-view-remote.spec.js ); then
    pass "Playwright Agent View remote suite passed"
else
    fail "Playwright Agent View remote suite failed"
fi

echo "=== Phase 6: backend saw the allocation ==="
# The allocated stack must be on the CONFIGURED ranges -- browser-backend mode
# once ignored SWE_CDP_PORTS/SWE_VNC_PORTS and silently allocated defaults.
if [ "$E2E_AV_BACKEND" = "image" ]; then
    BACKEND_LOGS="$(docker logs "$BACKEND_CONTAINER" 2>&1)"
else
    BACKEND_LOGS="$(cat "$TEST_DIR/backend.log")"
fi
echo "$BACKEND_LOGS" | grep -q "CDP port ${BACKEND_CDP%%-*}" \
    && pass "chromium allocated on configured CDP range ($BACKEND_CDP)" \
    || fail "chromium NOT on configured CDP range; got: $(echo "$BACKEND_LOGS" | grep -o 'CDP port [0-9]*' | head -1)"
HEALTH="$(curl -s "http://$BACKEND_HOST:$BACKEND_PORT/health")"
echo "  backend /health: $HEALTH"
# The spec closes its page, but session teardown is WS-disconnect driven and
# async -- a live or already-freed session are both correct here. Assert the
# backend is still healthy; allocation itself was asserted inside the spec.
echo "$HEALTH" | grep -q '"sessions"' && pass "backend healthy after run" || fail "backend health check failed"

echo "=== Result ==="
if [ "$FAILS" -eq 0 ]; then
    echo "AGENT VIEW REMOTE E2E ($E2E_AV_BACKEND): PASS"
    echo "screenshots: $SCREENSHOT_DIR"
else
    echo "AGENT VIEW REMOTE E2E ($E2E_AV_BACKEND): $FAILS assertion(s) FAILED"
    exit 1
fi
