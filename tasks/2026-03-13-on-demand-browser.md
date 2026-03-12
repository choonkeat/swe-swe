# On-Demand Browser for swe-swe Sessions

## Goal

Defer all 4 browser processes (Xvfb, Chromium, x11vnc, websockify) from session creation time to first Playwright MCP tool call. Sessions that never use browser tools save ~4 processes and their memory. Network ports (CDP 6000-6019, VNC 7000-7019) remain pre-allocated; only process startup is deferred.

## Architecture

```
Agent ←stdin/stdout→ mcp-lazy-init (Go proxy) ←stdin/stdout→ @playwright/mcp (spawned immediately)
                          │
                          │ (on first tools/call)
                          ▼
              POST /api/session/{uuid}/browser/start
                          │
                          ▼
              swe-swe-server starts Xvfb + Chromium + x11vnc + websockify
```

- **mcp-lazy-init** is a generic lazy-init MCP proxy. Not browser-specific.
- Playwright MCP is spawned immediately (cheap — no Chrome connection until first tool call).
- `initialize` and `tools/list` pass through transparently (Playwright MCP handles them without CDP).
- On first `tools/call`, the proxy makes an HTTP request to start browser, then forwards the call.

---

## Phase 1: Server API Endpoint ✅

### What will be achieved
A new `POST /api/session/{uuid}/browser/start` endpoint in `main.go` that triggers `startSessionBrowser()` on demand. Idempotent — calling it when browser is already running returns success.

### Steps

1. **Add `BrowserStarted` field to Session struct** (after `BrowserDataDir` at line ~426)
   - `BrowserStarted bool` — set to true at end of `startSessionBrowser()`

2. **Set flag in `startSessionBrowser()`**
   - At the end of the function (before `return nil`), set `sess.BrowserStarted = true`

3. **Add `handleBrowserStartAPI()` handler function**
   - Follow same pattern as `handleSessionEndAPI()` (line ~5763)
   - Parse UUID from path: `/api/session/{uuid}/browser/start`
   - Validate POST method (405 otherwise)
   - Look up session in `sessions` map with RLock (404 if not found)
   - If `sess.BrowserStarted` is true, return 200 with `{"status":"already_started"}`
   - Otherwise call `startSessionBrowser(sess)`, return 200 on success or 500 on error

4. **Register route in main handler** (near line ~2045)
   - Match `strings.HasPrefix(r.URL.Path, "/api/session/") && strings.HasSuffix(r.URL.Path, "/browser/start")`

5. **Remove `startSessionBrowser()` call from `getOrCreateSession()`** (lines 4130-4134)
   - Delete the `if parentUUID == ""` block that calls `startSessionBrowser`
   - This makes browser startup fully lazy

### Verification ✅

- **Unit test `TestHandleBrowserStartAPI`** with cases:
  - ✅ GET returns 405 Method Not Allowed
  - ✅ Unknown UUID returns 404 Not Found
  - ✅ Valid POST calls startSessionBrowser (accepts 200 or 500 depending on binary availability)
  - ✅ Second POST returns 200 with `{"status":"already_started"}`
- ✅ `make test` passes — no regressions
- ✅ Golden files updated (template changes reflected in all 36 golden variants)

---

## Phase 2: Generic Lazy-Init MCP Proxy (`mcp-lazy-init`) ✅

### What will be achieved
A standalone Go binary that wraps any stdio MCP server. Before the first `tools/call` reaches the wrapped server, it makes a configurable HTTP request. After that, it's a transparent relay.

### CLI Interface

```
mcp-lazy-init \
  --init-method POST \
  --init-url http://localhost:9898/api/session/$SESSION_UUID/browser/start \
  --init-header "Content-Type: application/json" \
  --init-request-body '{}' \
  -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT
```

Flags:
- `--init-method` — HTTP method (required)
- `--init-url` — HTTP endpoint (required)
- `--init-header` — Repeatable, format `Key: Value`
- `--init-request-body` — Request body string (optional)
- Everything after `--` is the wrapped MCP server command

### Steps

1. **Create `cmd/mcp-lazy-init/main.go`**
   - Parse CLI flags and `--` separator
   - Spawn wrapped MCP server subprocess with piped stdin/stdout

2. **Implement stdin relay (agent → subprocess)**
   - Read JSON-RPC messages line by line from our stdin
   - For each message, parse minimally to check the `"method"` field
   - If method is `"tools/call"` and `initDone` is false:
     - Make the configured HTTP request (init-method, init-url, init-header, init-request-body)
     - Log result (success or failure)
     - Set `initDone = true`
     - Forward the message to subprocess stdin
   - For all other messages: forward directly

3. **Implement stdout relay (subprocess → agent)**
   - Separate goroutine reads from subprocess stdout line by line
   - Writes to our stdout — always transparent, no inspection needed

4. **Process lifecycle**
   - When our stdin closes (agent disconnects): signal and kill subprocess
   - When subprocess exits: exit with same code
   - Handle SIGTERM/SIGINT gracefully

5. **Init failure behavior**
   - If HTTP request fails, log the error but still forward the tools/call
   - The wrapped MCP server will return its own error (e.g., CDP connection refused)
   - This avoids the proxy silently swallowing tool calls

### Verification

- **Unit test: message routing**
  - Mock MCP server (simple echo program)
  - Send `initialize`, `tools/list` — verify they pass through without triggering init
  - Send `tools/call` — verify init HTTP request is made, then message forwarded
  - Send second `tools/call` — verify no second init request

- **Unit test: init HTTP request**
  - Use `httptest.NewServer` as the init endpoint
  - Verify correct method, URL, headers, body are sent

- ✅ **Integration test** (TestRunMessageRouting uses `cat` as mock MCP server, pipes JSON-RPC messages through)

- ✅ `make test` passes

---

## Phase 3: Integration & Wiring ✅

### What will be achieved
The proxy binary is compiled, deployed into containers, and wired into the MCP registration. Sessions no longer start browser at creation time.

### Steps

1. **Add Go module for mcp-lazy-init**
   - `cmd/mcp-lazy-init/go.mod` (minimal, no external deps needed — just stdlib)
   - Or build it as part of the main module if simpler

2. **Dockerfile changes** (template at `cmd/swe-swe/templates/host/Dockerfile`)
   - Add build step: compile `cmd/mcp-lazy-init/main.go` → `/usr/local/bin/mcp-lazy-init`
   - This happens during the Docker image build, so the binary is available in the container

3. **entrypoint.sh changes** (template at `cmd/swe-swe/templates/host/entrypoint.sh`)
   - Update Playwright MCP registration from:
     ```
     claude mcp add --scope user --transport stdio swe-swe-playwright -- \
       npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT
     ```
     to:
     ```
     claude mcp add --scope user --transport stdio swe-swe-playwright -- \
       mcp-lazy-init \
         --init-method POST \
         --init-url http://localhost:9898/api/session/$SESSION_UUID/browser/start \
         -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT
     ```

4. **Golden file updates**
   - `make build golden-update`
   - Verify diff shows only: Dockerfile build step, entrypoint.sh MCP registration change

### Verification

- ✅ `make test` passes (golden tests match updated templates)
- **Manual test with test container:**
  1. Start a session — confirm no Xvfb/Chromium/x11vnc/websockify processes running (`ps aux | grep -E 'Xvfb|chromium|x11vnc|websockify'`)
  2. Open Agent View tab — VNC should show nothing / connection waiting (no browser yet)
  3. Use a Playwright MCP tool (e.g., `browser_navigate`) — confirm browser starts (~2-3s delay)
  4. Use another Playwright tool — confirm no second startup delay
  5. Check Agent View tab — should now show the browser
  6. Verify child sessions share parent's browser (no separate startup)

---

## Phase 4: Cleanup & Edge Cases ✅

### What will be achieved
Handle edge cases and clean up any loose ends.

### Steps

1. **Agent View before browser starts**
   - The VNC tab will show a connection error until browser starts. This is acceptable — the user will see the browser appear once a Playwright tool is used.
   - Consider: should clicking "Agent View" also trigger browser start? (Deferred — can be a follow-up if needed)

2. **Documentation**
   - Update `browser-automation.md` to note that browser starts on first Playwright tool use
   - Note the ~2-3 second one-time delay

3. **Logging**
   - Ensure mcp-lazy-init logs init request timing to stderr (visible in compose logs)
   - Server logs browser start with session UUID as before

### Verification

- Full manual test cycle
- Review compose logs for clean startup/init sequence
- Verify no regressions in existing functionality

---

## Phase 5: Hide Agent View Until Browser Starts

### What will be achieved
The Agent View tab is hidden by default and only appears (auto-activating) when browser processes are started on-demand. New browser visits to an already-started session see the Agent View tab immediately.

### Steps

1. **Add `browserStarted` to `BroadcastStatus()` JSON**
   - In `BroadcastStatus()`, add `"browserStarted": sess.BrowserStarted` to the status message
   - This field is sent on every status broadcast (client connect, resize, etc.)

2. **Broadcast status after browser start in `handleBrowserStartAPI()`**
   - After `startSessionBrowser(sess)` succeeds, call `sess.BroadcastStatus()`
   - This pushes `browserStarted: true` to all connected WebSocket clients

3. **Hide Agent View in terminal-ui.js when `browserStarted` is false**
   - In `handleJSONMessage()` for `"status"` type, store `this.browserStarted`
   - Hide the Agent View tab button (desktop), mobile dropdown option, and service link when `browserStarted` is false
   - Use CSS class or `style.display` toggle based on the flag

4. **Auto-switch to Agent View when browser starts**
   - In `handleJSONMessage()`, detect when `browserStarted` transitions from false to true
   - On that transition, call `switchPanelTab('browser')` to auto-activate the Agent View tab

### Verification

- `make test` passes
- **Manual test with test container:**
  1. Start a session — Agent View tab is NOT visible
  2. Use a Playwright MCP tool (e.g., `browser_navigate`) — Agent View tab appears and auto-activates
  3. Refresh the page — Agent View tab is still visible (session already has browser started)
  4. Open a new browser window to the same session — Agent View tab is visible immediately
  5. Check that other tabs (Preview, Code, Terminal) still work normally
