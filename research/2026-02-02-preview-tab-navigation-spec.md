# Preview Tab Navigation Spec

**Date**: 2026-02-02
**Status**: Draft

## Architecture

```
Parent page ({host}:1977)
│
├── iframe.tab-preview  (created on demand, persists across tab switches)
│   └── Shell page at {host}:5{PORT}/__swe-swe-shell__
│       ├── Monitoring script (always running, never destroyed by navigation)
│       ├── Debug WebSocket connection (persistent)
│       └── Inner iframe → user content at {host}:5{PORT}/path
│
├── iframe.tab-vscode   (created on demand, persists)
├── iframe.tab-shell    (created on demand, persists)
└── iframe.tab-browser  (created on demand, persists)
```

### Key properties

- **Parent ↔ shell page**: Cross-origin (different ports). Communicate via debug WebSocket.
- **Shell page ↔ inner iframe content**: Same-origin (both on `{host}:5{PORT}`). Shell can read `innerIframe.contentWindow.location.href`, call `innerIframe.contentWindow.history.back()`, etc.
- **Per-tab iframes**: Each tab gets its own iframe, created on first use, shown/hidden via CSS (`display: block` / `display: none`). Switching tabs does not destroy iframe state.

## Invariant

**The URL bar always shows `localhost:PORT/path?query#anchor`. Never the proxy URL.**

The proxy URL (`{host}:5{PORT}/...`) and the display URL (`localhost:PORT/...`) are a bidirectional mapping. Translation is a base-URL swap preserving path, query string, and anchor.

## Components

### 1. Shell page (`/__swe-swe-shell__`)

A minimal HTML page served by the proxy. Contains:

- An inner iframe (100% width/height, no border) that loads user content.
- A monitoring script that:
  - Connects to the debug WebSocket at `/__swe-swe-debug__/ws`.
  - On `innerIframe.onload`: reads `innerIframe.contentWindow.location.href` and sends `{ t: 'urlchange', url: ... }` over WebSocket. Works for all content types (HTML, JSON, images, etc.) because same-origin.
  - Receives commands from parent via WebSocket:
    - `{ t: 'navigate', action: 'back' }` → calls `innerIframe.contentWindow.history.back()`
    - `{ t: 'navigate', action: 'forward' }` → calls `innerIframe.contentWindow.history.forward()`
    - `{ t: 'navigate', url: '/path?q=1#hash' }` → sets `innerIframe.src = url`
    - `{ t: 'reload' }` → calls `innerIframe.contentWindow.location.reload()`
  - Sends to parent via WebSocket:
    - `{ t: 'urlchange', url: '...' }` — on every navigation (full page load or SPA)
    - `{ t: 'navstate', canGoBack: bool, canGoForward: bool }` — after every navigation (future: for button enable/disable)

### 2. Inject script (debugInjectJS, injected into HTML responses)

Still injected by the proxy into HTML responses inside the inner iframe. Responsibilities narrowed to:

- **SPA detection**: Hook `pushState`, `replaceState`, `popstate`, `hashchange`. On URL change, send `{ t: 'urlchange', url: location.href }` over WebSocket.
- **Console/error forwarding**: Unchanged.
- **DOM query**: Unchanged.

The inject script no longer needs to:
- Send `init` messages for URL tracking (shell handles this via `onload`).
- Handle `{ t: 'navigate' }` commands (shell handles this directly).

### 3. terminal-ui.js (parent page)

Connects to the debug WebSocket as a UI observer at `/__swe-swe-debug__/ui`. Receives `urlchange` and `navstate` messages. Sends commands by...

Actually — the parent needs to send commands (navigate, reload) but the UI observer WebSocket (`/ui`) is currently read-only. Two options:
- Make the `/ui` endpoint bidirectional (parent sends commands, hub routes to shell/iframe).
- Parent sends commands on the `/ui` WebSocket, hub forwards to iframe clients.

Use the existing `/ui` WebSocket bidirectionally. The hub forwards commands from UI observers to iframe clients (the shell page).

## URL bar update flow

| Trigger | Who acts | Flow |
|---------|----------|------|
| **Home** | Parent | Parent sends `{ t: 'navigate', url: '/' }` via WebSocket. Sets URL bar to `localhost:PORT/` immediately. Shell sets `innerIframe.src = '/'`. Shell's `onload` sends `urlchange` confirming. |
| **Go / Enter** | Parent | Parent parses path/query/anchor from URL bar. Sends `{ t: 'navigate', url: '/path?q#h' }` via WebSocket. URL bar already shows what user typed. Shell sets `innerIframe.src`. |
| **Reload** | Parent | Parent sends `{ t: 'reload' }` via WebSocket. URL bar unchanged. Shell calls `innerIframe.contentWindow.location.reload()`. |
| **Back** | Parent | Parent sends `{ t: 'navigate', action: 'back' }`. Shell calls `innerIframe.contentWindow.history.back()`. Shell's `onload` or inject script's `urlchange` updates URL bar. |
| **Forward** | Parent | Parent sends `{ t: 'navigate', action: 'forward' }`. Same as back but forward. |
| **Open External** | Parent | Parent reads last `urlchange` URL from WebSocket, opens proxy URL in new tab. |
| **User clicks link** | Inject/Shell | Inner iframe navigates. If full page load: shell's `onload` fires, sends `urlchange`. If SPA: inject script detects, sends `urlchange`. Parent updates URL bar. |

### URL bar: immediate vs. async updates

When the parent triggers navigation (Home, Go), it sets the URL bar **immediately** to the expected value. The WebSocket `urlchange` that arrives later writes the same value — no flash.

When navigation originates inside the iframe (user clicks, SPA routing, back/forward), the WebSocket `urlchange` is the sole source. There may be a brief moment where the URL bar shows the old value until the message arrives. This is acceptable.

## Per-tab iframes

### Current behavior (shared iframe)
One `<iframe>` element. Switching tabs swaps `iframe.src`. Preview state is destroyed.

### New behavior (per-tab iframes)
```html
<div class="terminal-ui__iframe-container">
  <!-- created on first use of each tab -->
  <iframe class="terminal-ui__iframe tab-preview" />
  <iframe class="terminal-ui__iframe tab-vscode" />
  <iframe class="terminal-ui__iframe tab-shell" />
  <iframe class="terminal-ui__iframe tab-browser" />
</div>
```

- **Create on demand**: Iframe is created and `src` set on first tab activation.
- **Show/hide**: Active tab's iframe gets `display: block`, others get `display: none`.
- **Persist**: Switching away does not change `src` or destroy content.
- **Toolbar**: The URL bar and nav buttons are only visible when preview tab is active. They bind to the preview iframe/WebSocket only.
- **Close pane**: Closing the right pane sets all iframes to `about:blank` and removes them (frees memory). Next open recreates on demand.

## App WebSocket proxy support

The proxy currently strips the `Upgrade` header and cannot handle WebSocket connections from the user's app.

### Fix

In `handleProxyRequest`, before the normal HTTP proxy flow:

1. Detect `Connection: Upgrade` + `Upgrade: websocket` headers.
2. Hijack the incoming connection (`http.Hijacker`).
3. Dial the backend at `localhost:PORT` with the same path.
4. Forward the upgrade request verbatim.
5. Read the backend's upgrade response, forward to client.
6. Pipe raw bytes bidirectionally until either side closes.

The `/__swe-swe-debug__/*` paths are matched first by the mux, so they are unaffected. Only the catch-all `/` handler needs upgrade detection.

No WebSocket framing knowledge needed — raw byte relay after the HTTP upgrade handshake.

## Initial state

1. User opens preview tab for the first time.
2. Preview iframe is created with `src = {host}:5{PORT}/__swe-swe-shell__`.
3. Shell page loads, creates inner iframe with `src = /`.
4. Shell connects to debug WebSocket.
5. If `localhost:PORT` is not reachable, proxy returns the "Listening for app..." error page. Inner iframe shows it. Shell's `onload` fires, sends `urlchange` with `/` path.
6. If reachable, user content loads. Inject script (for HTML) connects and sends SPA events. Shell's `onload` sends `urlchange`.
7. URL bar shows `localhost:PORT/`.

## Things to remove

| What | Where | Why |
|------|-------|-----|
| `/__swe-swe-debug__/target` API | `main.go` (GET, POST, OPTIONS handlers) | Proxy always targets `localhost:PORT`. No re-targeting needed. |
| `/__swe-swe-debug__/target` client calls | `terminal-ui.js` (Home button, Go button) | Replaced by WebSocket navigate commands to shell. |
| `updateIframeUrlDisplay()` | `terminal-ui.js` | Shell page handles URL reading. Parent never reads `iframe.contentWindow.location`. |
| `iframe.onload` → URL update | `terminal-ui.js` | Shell's `onload` on inner iframe is the source of truth. |
| `_lastProxyUrl` | `terminal-ui.js` | Not needed. Open External uses last `urlchange` URL from WebSocket. |
| `refreshIframe()` | `terminal-ui.js` | Replaced by sending `{ t: 'reload' }` to shell via WebSocket. |
| Shared iframe swapping logic | `terminal-ui.js` (`openIframePane`, `closeIframePane`) | Replaced by per-tab iframe create/show/hide. |
| `{ t: 'navigate' }` handling in inject script | `main.go` (debugInjectJS) | Shell handles navigation directly (same-origin). |

## Things to add

| What | Where | Why |
|------|-------|-----|
| Shell page endpoint (`/__swe-swe-shell__`) | `main.go` | Serves the shell HTML with monitoring script. |
| Shell monitoring script | `main.go` (inline in shell page) | `onload` URL reading, WebSocket communication, command handling. |
| WebSocket upgrade proxy | `main.go` (`handleProxyRequest`) | Support app WebSocket connections. |
| Per-tab iframe management | `terminal-ui.js` | Create on demand, show/hide, toolbar binding. |
| Bidirectional UI WebSocket | `main.go` (hub), `terminal-ui.js` | Parent sends commands, hub forwards to shell. |
| `{ t: 'navigate', url }` command | Shell script | Navigate inner iframe to specific path. |
| `{ t: 'reload' }` command | Shell script | Reload inner iframe in place. |

## Edge cases

### Port change mid-session
When `previewPort` changes, the preview iframe's shell page is on the old port. Parent should detect the change, set preview iframe `src` to the new shell URL, and reset the debug WebSocket connection.

### Non-HTML content
Shell's `onload` handler reads inner iframe's location regardless of content type. URL bar updates correctly for JSON, images, PDFs, etc. The inject script is not present for non-HTML, so SPA detection doesn't apply (non-HTML pages don't do pushState).

### Error page ("Listening for app...")
Served by proxy as a 502 HTML response. Loads inside inner iframe. Shell's `onload` fires, URL bar shows `localhost:PORT/`. Error page includes its own polling script to auto-reload when app becomes available. When app starts, inner iframe reloads and shell detects the new URL.

### Redirects
Proxy returns redirect responses directly (doesn't follow them). Browser follows the redirect inside the inner iframe. Shell's `onload` fires on the final page with the correct URL.

### iframe sandbox
The outer shared iframe (preview) has `sandbox="allow-same-origin allow-scripts allow-forms allow-popups allow-modals"`. The shell page's inner iframe inherits these restrictions. Consider adding `allow-downloads` if apps need it.

### Frame-busting apps
Apps that check `window.top === window` or `window.parent` will see the shell page frame. They are already inside an iframe (the preview iframe from the parent), so this was already broken before the double-iframe approach. No regression.
