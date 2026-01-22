# Debug Injection Proxy

## Goal

Upgrade the existing App Preview proxy to inject a debug script that forwards console logs, errors, and network requests to the agent, enabling the agent to debug user's app without needing visual access to the preview iframe.

## Background

Current state:
- User sees: preview iframe → their app on port 3000 (native browser experience)
- Agent sees: its own CDP browser → separate from user's view
- Problem: Agent can't debug what user sees; user can't show agent their errors

Solution:
- Proxy injects debug script into HTML responses
- Script forwards console/errors/network to agent via WebSocket
- Agent gets full observability without needing visual access
- User keeps native browser experience (no lag, full functionality)

---

## Phase 1: Script Injection Infrastructure ✅ COMPLETED

### What will be achieved

The reverse proxy at `startPreviewProxy()` will intercept `text/html` responses and inject a `<script>` tag that loads our debug script. Non-HTML responses pass through unchanged. Compressed responses (gzip/brotli) are decompressed before injection. CSP headers are modified to allow our injected script and WebSocket connection.

### Steps

1. ✅ **Add `ModifyResponse` callback to the reverse proxy**
   - Check `Content-Type` header for `text/html`
   - If not HTML, return early (pass through unchanged)

2. ✅ **Handle compressed responses**
   - Check `Content-Encoding` header
   - If `gzip`: decompress with `compress/gzip`
   - If `br`: pass through unchanged (no brotli library added)
   - Strip `Content-Encoding` header after decompression (send uncompressed)

3. ✅ **Inject script tag into HTML**
   - Use regex to find FIRST `<head>` or `<body>` tag (case insensitive)
   - Insert `<script src="/__swe-swe-debug__/inject.js"></script>` immediately after
   - Update `Content-Length` header

4. ✅ **Modify CSP headers if present**
   - Parse `Content-Security-Policy` header
   - Add `'self'` to `script-src` directive (for our script)
   - Add `ws:` and `wss:` to `connect-src` (for WebSocket)
   - If directive doesn't exist but CSP is present, append it

5. ✅ **Serve placeholder inject.js**
   - Add route `/__swe-swe-debug__/inject.js` that returns placeholder JS
   - This allows testing injection without the full debug script

### Verification ✅

**Tests (TDD red-green-refactor):**
- ✅ `TestInjectDebugScript` - 8 test cases for HTML injection
- ✅ `TestModifyCSPHeader` - 6 test cases for CSP modification
- ✅ `TestDebugInjectJSEndpoint` - placeholder script served
- ✅ `TestProxyHTMLInjection` - integration test documentation
- ✅ `TestGzipDecompression` - gzip roundtrip and injection

**Regression check:**
- ✅ All existing tests pass (`make test`)
- ✅ Build succeeds (`make build`)

---

## Phase 2: Debug Script & WebSocket Channel

### What will be achieved

The injected JavaScript (`inject.js`) captures console logs, uncaught errors, fetch requests, and XHR requests, forwarding them via WebSocket to a debug hub. The proxy serves this script and hosts WebSocket endpoints for both the iframe (sender) and agent (receiver).

### Steps

1. **Create the inject.js script**
   - Wrap console methods (log, warn, error, info) to forward messages
   - Add `window.onerror` and `unhandledrejection` handlers
   - Wrap `window.fetch` to capture requests/responses
   - Wrap `XMLHttpRequest.prototype.open/send` to capture XHR
   - Open WebSocket to `/__swe-swe-debug__/ws`
   - Handle incoming messages for DOM queries
   - Serialize arguments safely (handle circular refs, DOM nodes)

2. **Create DebugHub struct in Go**
   - Track connected iframe clients (`map[*websocket.Conn]bool`)
   - Track connected agent client (single `*websocket.Conn`)
   - `BroadcastToAgent(msg)`: forward iframe messages to agent
   - `BroadcastToIframes(msg)`: forward agent queries to iframes
   - Thread-safe with mutex

3. **Add WebSocket endpoint for iframe clients**
   - Route: `/__swe-swe-debug__/ws`
   - Upgrade to WebSocket, register with DebugHub
   - Read messages, forward to agent
   - Handle disconnect cleanup

4. **Add WebSocket endpoint for agent**
   - Route: `/__swe-swe-debug__/agent`
   - Upgrade to WebSocket, register as agent in DebugHub
   - Read messages (DOM queries), forward to iframes
   - Receive iframe messages, forward to agent

5. **Serve inject.js from Go**
   - Embed script as const string
   - Route `/__swe-swe-debug__/inject.js` serves with `application/javascript`

6. **Message protocol**
   ```
   From iframe to agent:
   {"t":"console", "m":"log", "args":["hello", 123]}
   {"t":"error", "msg":"...", "file":"...", "line":1, "stack":"..."}
   {"t":"fetch", "url":"/api/x", "method":"GET", "status":200, "ms":45}
   {"t":"xhr", "url":"/api/y", "method":"POST", "status":500, "ms":120}

   From agent to iframe:
   {"t":"query", "id":"abc", "selector":".error-msg"}

   From iframe to agent (response):
   {"t":"queryResult", "id":"abc", "text":"Invalid", "visible":true}
   ```

### Verification

**Tests (TDD red-green-refactor):**
- `TestInjectScriptServesJS` - GET returns valid JS
- `TestDebugHubForwardsToAgent` - iframe message reaches agent
- `TestDebugHubForwardsQueryToIframe` - agent query reaches iframe
- `TestDebugWSEndpointUpgrades` - WebSocket upgrade succeeds

**Manual verification:**
- Create test HTML with console.log statements
- Open in browser through proxy
- Connect to `/__swe-swe-debug__/agent` with wscat
- Verify console messages appear on agent connection
- Send DOM query, verify response

**Regression check:**
- Phase 1 injection still works
- Normal app functionality unaffected
- Multiple iframe clients can connect

---

## Phase 3: Agent Integration

### What will be achieved

The agent can connect to the debug channel and receive real-time debug messages from the user's app. For v1, agent uses a simple WebSocket client via Bash. Agent can also send DOM queries and receive responses.

### Steps

1. **Create simple debug client script**
   - Shell script or small Go binary: `swe-swe-debug-client`
   - Connects to `ws://localhost:9899/__swe-swe-debug__/agent`
   - Prints received messages as JSON lines to stdout
   - Accepts stdin for sending queries
   - Lives in container, agent can invoke it

2. **Add debug client to container**
   - Add `swe-swe-debug-client` binary to container build
   - Or use existing tool like `websocat` if available

3. **Document agent usage pattern**
   - Agent runs: `swe-swe-debug-client` in background
   - Reads output to see console logs, errors, network requests
   - Sends query: `echo '{"t":"query","id":"1","selector":".error"}' | swe-swe-debug-client --query`

4. **Add convenience wrapper (optional)**
   - `swe-swe debug listen` - starts listening, prints messages
   - `swe-swe debug query ".selector"` - sends query, waits for response

### Verification

**Tests (TDD red-green-refactor):**
- `TestDebugClientConnects` - client connects to agent endpoint
- `TestDebugClientReceivesMessages` - client prints messages from hub
- `TestDebugClientSendsQuery` - client sends query, receives response

**Manual verification:**
- Start proxy with test app
- Open app in browser through proxy
- Run debug client in another terminal
- Trigger console.log, verify client prints it
- Send DOM query, verify response

**Regression check:**
- Phase 1 & 2 still work independently
- Client disconnect doesn't crash proxy
- Multiple client reconnections work

---

## Phase 4: End-to-End Testing

### What will be achieved

Full integration test verifying the complete flow: user's app runs, proxy injects script, browser executes script, debug messages flow to agent, agent can query DOM.

### Steps

1. **Create test app for E2E testing**
   - Simple HTML page with:
     - Console.log on page load
     - Button that triggers console.error
     - Fetch request on button click
     - XHR request on another button
     - Element with known text for DOM query testing
   - Serve via Go's `httptest`

2. **Write E2E test using Playwright MCP**
   - Start proxy pointing to test app
   - Start debug client (connect agent endpoint)
   - Navigate browser to proxy URL
   - Verify: page load console.log received
   - Click error button, verify: console.error received
   - Click fetch button, verify: fetch message received
   - Send DOM query, verify: correct response

3. **Test CSP scenarios**
   - Test app with strict CSP header
   - Verify injection still works
   - Verify WebSocket connection not blocked

4. **Test compression scenarios**
   - Test app serving gzip HTML
   - Verify injection works through compression

5. **Test edge cases**
   - App not running (502 error page still works)
   - App returns non-HTML (passes through unchanged)
   - App uses WebSocket itself (not interfered with)
   - Multiple browser tabs open same app

6. **Document the feature**
   - Add section to docs explaining debug channel
   - Document message format
   - Document how agent uses it

### Verification

**Tests (TDD red-green-refactor):**
- `TestE2EConsoleLogFlowsToAgent`
- `TestE2EFetchFlowsToAgent`
- `TestE2EDOMQueryWorks`
- `TestE2EWithCSP`
- `TestE2EWithGzip`

**Manual verification (using test container):**
- Boot test container via `docs/dev/test-container-workflow.md`
- Start a real app inside container (e.g., Vite dev server)
- Open in browser preview pane
- Have agent connect to debug channel
- Interact with app, verify agent sees everything

**Regression check:**
- All unit tests from Phases 1-3 still pass
- Existing proxy functionality unchanged
- Preview pane UX unchanged (user doesn't notice injection)

---

## Files to Modify

| File | Changes |
|------|---------|
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | Add ModifyResponse, CSP handling, DebugHub, WebSocket endpoints, inject.js serving |
| `cmd/swe-swe/templates/host/swe-swe-server/main_test.go` | Add tests for injection, hub, endpoints (new file) |
| `cmd/swe-swe/templates/host/swe-swe-server/go.mod.txt` | Add brotli dependency if needed |
| Container Dockerfile | Add debug client binary |
| `docs/` | Document debug channel feature |

---

## Out of Scope (Future)

- MCP tool for debug channel (v1 uses bash client)
- Visual screenshot relay
- React/Vue devtools integration
- Network request/response body capture (only metadata in v1)
- Source map support for stack traces
