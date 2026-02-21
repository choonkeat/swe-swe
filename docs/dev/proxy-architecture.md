# Proxy Architecture: Preview & Agent Chat

How the Preview tab and Agent Chat tab work end-to-end.

## Port Layout

Each session gets a **preview port** (e.g., 3000) and a derived **agent chat port** (+1000, e.g., 4000). Both are container-internal only — no host port bindings needed.

Both proxies are path-based, hosted inside swe-swe-server on the main port (:9898):

```
Preview:     User's app  ←  swe-swe-server /proxy/{uuid}/preview/    ←  Traefik :1977  ←  Browser
               :3000          :9898

Agent Chat:  MCP sidecar ←  swe-swe-server /proxy/{uuid}/agentchat/  ←  Traefik :1977  ←  Browser
               :4000          :9898
```

- `agentChatPortFromPreview(previewPort) = previewPort + 1000`
- Only **1 port** goes through Traefik (the main server port)
- No per-session Traefik entrypoints, ports, or routers

swe-swe-server passes these to the container environment:
- `PORT={previewPort}` — tells the user's app which port to bind (e.g., 3000)
- `AGENT_CHAT_PORT={agentChatPort}` — tells the MCP sidecar which port to bind (e.g., 4000)
- `SESSION_UUID={uuid}` — used by the stdio bridge and open shim to address the correct proxy instance

### Port reservation

`findAvailablePortPair()` reserves two ports (preview app, agent chat app) by binding and then releasing them. No proxy port listeners are needed — both proxies are handled as path-based routes inside swe-swe-server's main HTTP handler.

## Agent Chat Proxy (swe-swe-server)

The agent chat proxy uses `agentChatProxyHandler()` which:

1. Sets `X-Agent-Reverse-Proxy: 1` header on **every response** (before any body writing)
2. Detects WebSocket upgrades → raw byte relay via `handleWebSocketRelay()`
3. For HTTP: forwards the request to `http://localhost:{targetPort}`, copies response via `processProxyResponse()`
4. On connection error: returns 502 with `agentChatWaitingPage` ("Waiting for Agent Chat…" with auto-poll)

`processProxyResponse()` copies headers (stripping cookie Domain/Secure attributes) then streams the body. No modification of response content.

Since both proxies are same-origin (path-based on the main server port), no CORS headers are needed. The browser can always read the `X-Agent-Reverse-Proxy` header from JavaScript.

## The `X-Agent-Reverse-Proxy` Header

This header serves one purpose: **let the browser-side probe detect that our proxy handler is active**.

- swe-swe-server agent chat proxy sets it to `"1"` on every response (including 502 error pages)
- The preview proxy (hosted in swe-swe-server) also sets this header (with its version string) on every response
- If the session doesn't exist, swe-swe-server returns 404 (no header)

The probe uses `resp.headers.has('X-Agent-Reverse-Proxy')` — it doesn't care about the status code. A 502 *with* the header means "our proxy is running but the backend app isn't up yet."

## Preview Tab Flow

The Preview tab uses a **placeholder → probe → iframe load** pattern. The preview proxy is hosted inside swe-swe-server as an embedded Go library (`github.com/choonkeat/agent-reverse-proxy`), with each session getting a proxy instance at `/proxy/{session-uuid}/preview/...`.

AI agents communicate with the preview proxy via a lightweight stdio bridge process (`npx @choonkeat/agent-reverse-proxy --bridge http://swe-swe:3000/proxy/$SESSION_UUID/preview/mcp`).

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
   - Debug WebSocket receives `urlchange` or `init` message from the preview proxy (primary)

The preview proxy serves a shell page at `/__agent-reverse-proxy-debug__/shell` and provides a UI observer WebSocket at `/__agent-reverse-proxy-debug__/ui` for URL bar updates and navigation state.

### What the user sees

```
[Session not ready]     → "Connecting to preview..." (placeholder)
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
[Session not ready]     → "Connecting to chat..." (placeholder)
[Proxy up, app not up]  → "Waiting for Agent Chat…" (swe-swe-server's 502 page with auto-poll)
[App up]                → Agent chat UI
```
