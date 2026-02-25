# Proxy Architecture: Preview & Agent Chat

How the Preview tab and Agent Chat tab work end-to-end.

## Port Layout

Each session gets a **preview port** (e.g., 3000), a derived **agent chat port** (+1000, e.g., 4000), and a **public port** (e.g., 5000). All are container-internal only.

The preview and agent chat proxies are reachable via **two paths** — the browser automatically picks whichever works:

```
Port-based (preferred, per-origin isolation):
  Preview:     User's app  ←  swe-swe-server :23000  ←  Traefik :23000 (forwardauth)  ←  Browser
                 :3000

  Agent Chat:  MCP sidecar ←  swe-swe-server :24000  ←  Traefik :24000 (forwardauth)  ←  Browser
                 :4000

  Public:      User's app  ←  swe-swe-server :25000  ←  Traefik :25000 (NO auth)      ←  Anyone
                 :5000

Path-based (fallback, when ports are unreachable):
  Preview:     User's app  ←  swe-swe-server /proxy/{uuid}/preview/    ←  Traefik :1977  ←  Browser
                 :3000          :9898

  Agent Chat:  MCP sidecar ←  swe-swe-server /proxy/{uuid}/agentchat/  ←  Traefik :1977  ←  Browser
                 :4000          :9898
```

The **public port** differs from preview/agent chat: its Traefik router omits the `forwardauth@file` middleware, making it accessible without login. Use it for webhooks, public APIs, or shareable URLs.

Both paths reach the **same proxy instance** — the embedded `agent-reverse-proxy` Go library inside swe-swe-server. The per-port listeners are thin wrappers that delegate to the same handler.

### Derived ports

- `agentChatPort(previewPort) = previewPort + 1000`
- `publicPort(previewPort) = previewPort + 2000` (e.g., 3000 → 5000)
- `previewProxyPort(previewPort) = previewPort + proxyPortOffset` (default offset: 20000)
- `agentChatProxyPort(acPort) = acPort + proxyPortOffset`
- `publicProxyPort(publicPort) = publicPort + proxyPortOffset`

### Environment variables

swe-swe-server passes these to the container environment:
- `PORT={previewPort}` — tells the user's app which port to bind (e.g., 3000)
- `AGENT_CHAT_PORT={agentChatPort}` — tells the MCP sidecar which port to bind (e.g., 4000)
- `PUBLIC_PORT={publicPort}` — no-auth port for webhooks/public APIs (e.g., 5000)
- `SESSION_UUID={uuid}` — used by the stdio bridge and open shim to address the correct proxy instance

### Port reservation

`findAvailablePortPair()` reserves three app ports (preview, agent chat, public) by bind-probing. Additionally, swe-swe-server starts per-port listeners on the derived proxy ports (e.g., :23000, :24000, :25000) when each session is created.

### Agent bridge

AI agents always communicate via the internal path-based URL:

```
npx @choonkeat/agent-reverse-proxy --bridge http://swe-swe:3000/proxy/$SESSION_UUID/preview/mcp
```

This is container-internal (never goes through Traefik), so it works regardless of which mode the browser uses.

## Proxy Mode Discovery (Browser)

The browser discovers which mode to use via a two-phase probe:

1. **Path probe** — `probeUntilReady(pathBasedUrl)`. Checks if the proxy handler is active (target app may or may not be up). Retries with exponential backoff. If this fails, neither mode works yet — keep retrying.

2. **Port probe** — once the path probe succeeds, make a single quick fetch to the port-based URL (e.g., `https://hostname:23000/`). If reachable (proxy header present), use port-based URLs. If not (blocked port, timeout), stay on path-based.

The decided mode is stored for the session. All subsequent URL construction (iframe src, debug WebSocket, agent chat) follows the chosen mode consistently.

### Why path-first

The path-based URL is always reachable (it's on the main server port). By probing it first, we know whether the proxy handler and target app exist at all. The port probe is then a quick "can I reach it on the dedicated port?" check — not a "is the app up?" check.

## Agent Chat Proxy (swe-swe-server)

The agent chat proxy uses `agentChatProxyHandler()` which:

1. Sets `X-Agent-Reverse-Proxy: 1` header on **every response** (before any body writing)
2. Detects WebSocket upgrades → raw byte relay via `handleWebSocketRelay()`
3. For HTTP: forwards the request to `http://localhost:{targetPort}`, copies response via `processProxyResponse()`
4. On connection error: returns 502 with `agentChatWaitingPage` ("Waiting for Agent Chat..." with auto-poll)

`processProxyResponse()` copies headers (stripping cookie Domain/Secure attributes) then streams the body. No modification of response content.

In port-based mode, the agent chat proxy is also reachable on its dedicated port (e.g., :24000) via the per-port listener. In path-based mode, it's at `/proxy/{uuid}/agentchat/` on the main port.

## The `X-Agent-Reverse-Proxy` Header

This header serves one purpose: **let the browser-side probe detect that our proxy handler is active**.

- swe-swe-server agent chat proxy sets it to `"1"` on every response (including 502 error pages)
- The preview proxy (hosted in swe-swe-server) also sets this header (with its version string) on every response
- If the session doesn't exist, swe-swe-server returns 404 (no header)

The probe uses `resp.headers.has('X-Agent-Reverse-Proxy')` — it doesn't care about the status code. A 502 *with* the header means "our proxy is running but the backend app isn't up yet."

In port-based mode, this header is readable because it's same-origin (dedicated port). In path-based mode, it's readable because it's same-origin (same main port). No CORS needed in either case.

## Preview Tab Flow

The Preview tab uses a **placeholder → probe → iframe load** pattern. The preview proxy is hosted inside swe-swe-server as an embedded Go library (`github.com/choonkeat/agent-reverse-proxy`), with each session getting a proxy instance.

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
3. **Path probe** → `probeUntilReady(pathBasedUrl + '/', ...)` — retries until proxy header detected. Uses GET to avoid iOS Safari preflight bug.
4. **Port probe** → quick fetch to `portBasedUrl + '/'`. If header present → port-based mode. Otherwise → path-based mode.
5. **Iframe loaded** → `iframe.src = chosenBaseUrl + '/__agent-reverse-proxy-debug__/shell?path=...'`
6. **Placeholder dismissed** when either:
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
3. **Path probe** then **port probe** — same two-phase discovery as preview.
4. **On probe success**: set `chatIframe.src` to the chosen URL (port-based or path-based). On iframe load, dismiss placeholder.

### What the user sees

```
[Session not ready]     → "Connecting to chat..." (placeholder)
[Proxy up, app not up]  → "Waiting for Agent Chat..." (swe-swe-server's 502 page with auto-poll)
[App up]                → Agent chat UI
```
