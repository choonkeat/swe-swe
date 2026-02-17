# Proxy Architecture: Preview & Agent Chat

How the Preview tab and Agent Chat tab work end-to-end, from Traefik through to the iframe.

## Port Layout

Each session gets a **preview port** (e.g., 3000) and a derived **agent chat port** (+1000, e.g., 4000). swe-swe-server runs a reverse proxy for each on `20000 + port`:

```
User's app  ←  swe-swe-server proxy  ←  Traefik  ←  Browser
  :3000          :23000                   :23000
  :4000          :24000                   :24000
```

- `previewProxyPort(port) = 20000 + port`
- `agentChatProxyPort(port) = 20000 + port`
- `agentChatPortFromPreview(previewPort) = previewPort + 1000`

Traefik routes each port to the corresponding swe-swe-server proxy via per-port entrypoints and routers (generated in `templates.go`). All routers go through `forwardauth` middleware for session cookie validation.

## swe-swe-server Proxy (shared code)

Both preview and agent chat proxies share `handleProxyRequest()` which:

1. Sets `X-Agent-Reverse-Proxy: 1` header on **every response** (before any body writing)
2. Reads the target URL from `previewProxyState` (dynamically updatable)
3. Detects WebSocket upgrades → raw byte relay via `handleWebSocketRelay()`
4. For HTTP: forwards the request to `http://localhost:{targetPort}`, copies response via `processProxyResponse()`
5. On connection error: returns 502 with `previewProxyErrorPage` (an HTML page that auto-polls until the app comes up)

`processProxyResponse()` copies headers (stripping cookie Domain/Secure attributes) then streams the body. No modification of response content.

### Differences between preview and agent chat proxies

| | Preview (`startPreviewProxy`) | Agent Chat (`startAgentChatProxy`) |
|---|---|---|
| Routes | `mux.HandleFunc("/", ...)` | Same |
| CORS | None (same-origin) | Wraps with CORS: `Access-Control-Allow-Origin`, `Allow-Credentials`, `Allow-Methods`, `Expose-Headers: X-Agent-Reverse-Proxy` |
| Lifecycle | Ref-counted via `acquirePreviewProxyServer` | Ref-counted via `acquireAgentChatProxyServer` |

The CORS wrapper on agent chat is needed because the probe `fetch()` from the main UI (on port 1977) to the agent chat port (e.g., 24000) is cross-origin. Without `Access-Control-Expose-Headers`, the browser hides the `X-Agent-Reverse-Proxy` header from JS.

## The `X-Agent-Reverse-Proxy` Header

This header serves one purpose: **let the browser-side probe distinguish "our proxy is up" from "Traefik returned 502 because our proxy container hasn't started."**

- swe-swe-server proxy sets it to `"1"` on every response (including 502 error pages)
- Traefik's own 502 does NOT have this header
- The `swe-swe-preview` MCP tool (agent-reverse-proxy) also sets this header (with its version string) on the preview proxy port

The probe uses `resp.headers.has('X-Agent-Reverse-Proxy')` — it doesn't care about the status code. A 502 *with* the header means "our proxy is running but the backend app isn't up yet." A 502 *without* the header means "Traefik can't reach our proxy."

## Preview Tab Flow

The Preview tab uses a **placeholder → probe → iframe load** pattern:

### HTML structure

```html
<div class="terminal-ui__iframe-placeholder">
    <div class="terminal-ui__iframe-placeholder-status">
        <span class="terminal-ui__iframe-placeholder-dot"></span>
        <span class="terminal-ui__iframe-placeholder-text">Connecting to preview...</span>
    </div>
</div>
<iframe class="terminal-ui__iframe" src="" ...></iframe>
```

The placeholder overlays the iframe. It stays visible until dismissed.

### Sequence

1. **User opens Preview** → `openIframePane('preview')` → calls `setPreviewURL()`
2. **Placeholder shown** → `placeholder.classList.remove('hidden')`, `_previewWaiting = true`
3. **Probe starts** → `probeUntilReady(base + '/', { isReady: resp => resp.headers.has('X-Agent-Reverse-Proxy') })` — retries up to 10 times with exponential backoff (2s → 30s)
4. **Probe succeeds** (proxy is up) → `iframe.src = base + '/__agent-reverse-proxy-debug__/shell?path=...'`
5. **Placeholder dismissed** when either:
   - `iframe.onload` fires while `_previewWaiting` is true (fallback)
   - Debug WebSocket receives `urlchange` or `init` message from agent-reverse-proxy (primary)

The `swe-swe-preview` MCP tool (agent-reverse-proxy) serves a shell page at `/__agent-reverse-proxy-debug__/shell` and provides a UI observer WebSocket at `/__agent-reverse-proxy-debug__/ui` for URL bar updates and navigation state.

### What the user sees

```
[Traefik not ready]     → "Connecting to preview..." (placeholder)
[Proxy up, app not up]  → "Listening for app..." (proxy's 502 error page, shown inside iframe after probe succeeds)
[App up]                → Actual app content
```

## Agent Chat Tab Flow

### HTML structure (current, before fix)

```html
<div class="terminal-ui__agent-chat" style="display: none;">
    <iframe class="terminal-ui__agent-chat-iframe" src="about:blank" ...></iframe>
</div>
```

No placeholder element — the iframe loads directly.

### Sequence (current)

1. **Server broadcasts `agentChatPort`** in status message to all sessions (regardless of session mode)
2. **Client probes** `probeUntilReady(acUrl + '/', { isReady: resp => resp.headers.has('X-Agent-Reverse-Proxy') })` — same header check as preview
3. **On probe success**: reveals the Agent Chat tab button, and if `session=chat`, auto-activates the chat tab
4. **On tab activation** (`switchLeftPanelTab('chat')`): lazy-loads `chatIframe.src = buildAgentChatUrl(...) + '/'`

### What the user sees (current, broken)

- `session=chat`: Tab shown immediately (line 2932), but `switchLeftPanelTab('chat')` fires before probe completes → `_agentChatLoaded` is false, no agentChatPort yet → iframe stays `about:blank`. Then when probe succeeds, `switchLeftPanelTab('chat')` is called again but `tab === this.leftPanelTab` guard returns early. The iframe never gets its src set to the agent chat URL — or if it does, the backend may not be ready, showing the 502 error page with no retry.
- `session=terminal`: `AGENT_CHAT_DISABLE=1` prevents the MCP sidecar from starting, but the server still sends `agentChatPort`. The probe succeeds (our proxy responds with the header even on 502), so the tab appears when it shouldn't.

### Sequence (after fix)

The agent chat tab should follow the same placeholder pattern as preview:

1. **`session=chat`**: Tab shown immediately. Placeholder ("Connecting...") shown over iframe area.
2. **`session=terminal`**: Skip probe entirely — don't send `agentChatPort` to client.
3. **Probe** waits for `X-Agent-Reverse-Proxy` header (proxy is up).
4. **On probe success**: set `chatIframe.src` to the agent chat URL. On iframe load, dismiss placeholder.

What the user sees after fix:

```
[Traefik not ready]     → "Connecting..." (placeholder)
[Proxy up, app not up]  → proxy's 502 error page (real content from our proxy)
[App up]                → Agent chat UI
```

This matches the Preview tab pattern: the placeholder hides Traefik's 502, and once our proxy is serving, whatever it returns is shown directly.
