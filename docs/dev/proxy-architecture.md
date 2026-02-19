# Proxy Architecture: Preview & Agent Chat

How the Preview tab and Agent Chat tab work end-to-end, from Traefik through to the iframe.

## Port Layout

Each session gets a **preview port** (e.g., 3000) and a derived **agent chat port** (+1000, e.g., 4000).

- Preview is proxied by agent-reverse-proxy (MCP tool) running inside the container
- Agent Chat is proxied by swe-swe-server on `20000 + port`

```
Preview:     User's app  ←  agent-reverse-proxy (in-container)  ←  Traefik  ←  Browser
               :3000          :23000                                :23000

Agent Chat:  MCP sidecar ←  swe-swe-server proxy                ←  Traefik  ←  Browser
               :4000          :24000                                :24000
```

- `previewProxyPort(port) = proxyPortOffset + port` (default offset: 20000)
- `agentChatProxyPort(port) = proxyPortOffset + port` (default offset: 20000)
- `agentChatPortFromPreview(previewPort) = previewPort + 1000`
- The offset is configurable via `swe-swe init --proxy-port-offset <N>`

swe-swe-server passes these ports to the container environment so in-container processes know where to listen:
- `PORT={previewPort}` — tells the user's app which port to bind (e.g., 3000)
- `PROXY_PORT={previewProxyPort}` — tells agent-reverse-proxy which port to listen on (e.g., 23000)
- `AGENT_CHAT_PORT={agentChatPort}` — tells the MCP sidecar which port to bind (e.g., 4000)

### Port reservation

`findAvailablePortPair()` reserves all four ports (preview proxy, preview app, agent chat proxy, agent chat app) by binding and then releasing them. The preview proxy listener is immediately closed (agent-reverse-proxy will bind it later inside the container). The agent chat proxy listener is passed to `acquireAgentChatProxyServer` which keeps it.

Traefik routes each port to the corresponding proxy via per-port entrypoints and routers (generated in `templates.go`). All routers go through `forwardauth` middleware for session cookie validation.

## Agent Chat Proxy (swe-swe-server)

The agent chat proxy uses `handleProxyRequest()` which:

1. Sets `X-Agent-Reverse-Proxy: 1` header on **every response** (before any body writing)
2. Reads the target URL from `previewProxyState` (dynamically updatable)
3. Detects WebSocket upgrades → raw byte relay via `handleWebSocketRelay()`
4. For HTTP: forwards the request to `http://localhost:{targetPort}`, copies response via `processProxyResponse()`
5. On connection error: returns 502 with `agentChatWaitingPage` ("Waiting for Agent Chat…" with auto-poll)

`processProxyResponse()` copies headers (stripping cookie Domain/Secure attributes) then streams the body. No modification of response content.

The agent chat proxy wraps with CORS (`Access-Control-Allow-Origin`, `Allow-Credentials`, `Allow-Methods`, `Expose-Headers: X-Agent-Reverse-Proxy`) because the probe `fetch()` from the main UI (on port 1977) to the agent chat port (e.g., 24000) is cross-origin. Without `Access-Control-Expose-Headers`, the browser hides the `X-Agent-Reverse-Proxy` header from JS.

Lifecycle is ref-counted via `acquireAgentChatProxyServer` / `releaseAgentChatProxyServer`.

## The `X-Agent-Reverse-Proxy` Header

This header serves one purpose: **let the browser-side probe distinguish "our proxy is up" from "Traefik returned 502 because our proxy container hasn't started."**

- swe-swe-server agent chat proxy sets it to `"1"` on every response (including 502 error pages)
- Traefik's own 502 does NOT have this header
- The `swe-swe-preview` MCP tool (agent-reverse-proxy) also sets this header (with its version string) on the preview proxy port

The probe uses `resp.headers.has('X-Agent-Reverse-Proxy')` — it doesn't care about the status code. A 502 *with* the header means "our proxy is running but the backend app isn't up yet." A 502 *without* the header means "Traefik can't reach our proxy."

## Preview Tab Flow

The Preview tab uses a **placeholder → probe → iframe load** pattern. The preview proxy runs inside the container via agent-reverse-proxy (MCP tool), not swe-swe-server.

### HTML structure

Both the agent chat (left panel) and preview (right panel) have placeholder divs with the shared base class `terminal-ui__iframe-placeholder`:

```html
<!-- Left panel: agent chat -->
<div class="terminal-ui__agent-chat">
    <div class="terminal-ui__iframe-placeholder terminal-ui__agent-chat-placeholder">
        ...Connecting to chat...
    </div>
    <iframe class="terminal-ui__agent-chat-iframe" ...></iframe>
</div>

<!-- Right panel: preview -->
<div class="terminal-ui__iframe-container">
    <div class="terminal-ui__iframe-placeholder">
        ...Connecting to preview...
    </div>
    <iframe class="terminal-ui__iframe" ...></iframe>
</div>
```

Because both placeholders share the base class, **preview selectors must be scoped** to `.terminal-ui__iframe-container .terminal-ui__iframe-placeholder` to avoid matching the agent chat placeholder (which appears first in DOM order). Agent chat code uses the specific class `.terminal-ui__agent-chat-placeholder`.

Each placeholder overlays its iframe and stays visible until dismissed.

### Sequence

1. **User opens Preview** → `openIframePane('preview')` → calls `setPreviewURL()`
2. **Placeholder shown** → `placeholder.classList.remove('hidden')`, `_previewWaiting = true`
3. **Probe starts** → `probeUntilReady(base + '/', { method: 'GET', isReady: resp => resp.headers.has('X-Agent-Reverse-Proxy') })` — retries up to 10 times with exponential backoff (2s → 30s). Uses GET instead of the default HEAD to avoid an iOS Safari preflight bug.
4. **Probe succeeds** (proxy is up) → `iframe.src = base + '/__agent-reverse-proxy-debug__/shell?path=...'`
5. **Placeholder dismissed** when either:
   - `iframe.onload` fires while `_previewWaiting` is true (fallback)
   - Debug WebSocket receives `urlchange` or `init` message from agent-reverse-proxy (primary)

The `swe-swe-preview` MCP tool (agent-reverse-proxy) serves a shell page at `/__agent-reverse-proxy-debug__/shell` and provides a UI observer WebSocket at `/__agent-reverse-proxy-debug__/ui` for URL bar updates and navigation state.

### What the user sees

```
[Traefik not ready]     → "Connecting to preview..." (placeholder)
[Proxy up, app not up]  → agent-reverse-proxy waiting page (shown inside iframe after probe succeeds)
[App up]                → Actual app content
```

## Agent Chat Tab Flow

### Sequence

1. **`session=chat`**: Tab shown immediately. Placeholder ("Connecting to chat...") shown over iframe area.
2. **`session=terminal`**: Server-side `SessionMode` controls this — when `SessionMode != "chat"`, the server sends `agentChatPort: 0` in status messages, so the client never probes and never shows the tab.
3. **Probe** uses `probeUntilReady(acUrl + '/', { method: 'GET', isReady: resp => resp.headers.has('X-Agent-Reverse-Proxy') })` — same pattern as preview (GET to avoid iOS Safari preflight bug).
4. **On probe success**: set `chatIframe.src` to the agent chat URL. On iframe load, dismiss placeholder.

### What the user sees

```
[Traefik not ready]     → "Connecting to chat..." (placeholder)
[Proxy up, app not up]  → "Waiting for Agent Chat…" (swe-swe-server's 502 page with auto-poll)
[App up]                → Agent chat UI
```
