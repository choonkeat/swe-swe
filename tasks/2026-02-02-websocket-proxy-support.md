# WebSocket Proxy Support

**Spec**: `research/2026-02-02-preview-tab-navigation-spec.md` (section "App WebSocket proxy support")

## Goal

Add WebSocket proxy support to `handleProxyRequest` so that user apps running behind the preview proxy can establish WebSocket connections. Currently, the proxy strips `Upgrade` and `Connection` headers (treating them as hop-by-hop), which breaks any app that uses WebSockets (e.g., hot-reload dev servers, real-time apps).

## Key Files

- `cmd/swe-swe/templates/host/swe-swe-server/main.go` — Proxy implementation (`handleProxyRequest`, `isHopByHopHeader`)
- `cmd/swe-swe/templates/host/swe-swe-server/debug_inject_test.go` — Existing proxy tests (or new `websocket_proxy_test.go`)

## Approach

Detect `Upgrade: websocket` in the catch-all `/` handler, hijack the connection, dial the backend, and relay raw bytes. The `/__swe-swe-debug__/*` endpoints are matched by the mux before `/` and are completely unaffected.

## Testing Approach

TDD with Go unit tests (`httptest.NewServer` + gorilla/websocket, already a dependency). Manual smoke test via dev server (`docs/dev/swe-swe-server-workflow.md`).

---

## Progress

- [x] Phase 1: WebSocket upgrade relay + unit tests (TDD)
- [ ] Phase 2: Manual smoke test + golden update

---

## Phase 1: WebSocket upgrade relay + unit tests (TDD)

### What will be achieved

Add WebSocket upgrade detection and raw byte relay to `handleProxyRequest`, verified by Go unit tests that run entirely in-process.

### Steps

1. **Write the test first (red)** — Write a test that:
   - Starts a backend `httptest.Server` that upgrades WebSocket connections (using gorilla/websocket, already a dependency)
   - Starts the proxy server pointing at the backend
   - Connects to the proxy with a WebSocket client
   - Sends a message, expects an echo back
   - Verify the test fails with current code (upgrade headers stripped, connection fails)

2. **Write a second test for non-WebSocket regression (green already)** — A test that makes a normal HTTP GET through the proxy and verifies it still works. This should pass before and after our changes.

3. **Add upgrade detection at the top of `handleProxyRequest`** — Check `r.Header.Get("Upgrade")` for `"websocket"` (case-insensitive). If detected, branch into relay path.

4. **Implement the relay**:
   - Hijack incoming connection via `http.Hijacker`
   - `net.Dial` to `localhost:PORT`
   - Write the raw HTTP upgrade request to the backend (reconstruct from `*http.Request`)
   - Read the backend's 101 response, forward to client
   - `io.Copy` bidirectionally in two goroutines
   - Close both sides when either closes

5. **Run tests (green)** — The WebSocket test should now pass. The normal HTTP test should still pass.

6. **Write an edge case test** — WebSocket upgrade to a backend that isn't listening (port closed). Verify the proxy returns an HTTP error (502 or similar) instead of hanging or panicking.

7. **Run `make test`** — Full test suite passes.

### Verification

- **TDD red→green**: WebSocket proxy test fails before implementation, passes after.
- **Regression**: Normal HTTP proxy test passes throughout.
- **Edge case**: Backend-down test returns clean error.
- **Full suite**: `make test` green.

---

## Phase 2: Manual smoke test + golden update

### What will be achieved

Verify the WebSocket proxy works with a real app via the dev server, update golden snapshots, and confirm no regressions across the full build.

### Steps

1. **Create a minimal WebSocket test app** — A simple script that serves an HTML page with a WebSocket connection back to itself, displays "connected" status, and echoes messages. Place in scratchpad directory (not committed).

2. **Start the dev server** — `make run` to start swe-swe-server on port 3000.

3. **Start the test app** — Run the test app on a port (e.g., 3007) behind the proxy.

4. **Browser test via MCP browser** — Navigate to the preview URL through the proxy, verify:
   - The HTML page loads (normal HTTP proxy, existing functionality)
   - The WebSocket connection establishes successfully (new functionality)
   - Sending a message through the WebSocket gets an echo back
   - The connection stays alive (not immediately dropped)

5. **Stop dev server and test app** — `make stop`, kill test app.

6. **Run `make build golden-update`** — Rebuild binary, regenerate golden snapshots.

7. **Inspect golden diff** — `git diff` to verify changes are limited to:
   - `main.go`: new WebSocket relay code in `handleProxyRequest`
   - Test files: new WebSocket proxy tests
   - Golden files: only reflect the `main.go` changes (no HTML/CSS/JS changes expected since this is server-side only)

8. **Run `make test`** — Full suite green.

### Verification

- **End-to-end**: Real WebSocket connection works through the proxy in a browser.
- **Golden diffs**: Only expected changes, no surprises.
- **Full suite**: `make test` green.
