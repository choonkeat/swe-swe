# Preview Tab Enhancement Research

## Context

Each swe-swe session gets a per-session preview proxy (`5{PORT}`) with an injected debug channel. The debug channel provides a bidirectional WebSocket relay between the iframe (running the user's app) and the agent. Currently the debug channel supports console capture, error capture, fetch/XHR monitoring, and `query` (querySelector) commands.

The Playwright MCP browser is shared across all sessions in a deployment — it cannot be trivially scoped to a single session's preview iframe.

---

## 1. Back/Forward Buttons in Preview Toolbar

### Current toolbar layout (terminal-ui.js, line ~349)

```
⌂ Home | ↻ Refresh | [URL input] | → Go | ↗ Open External
```

### Proposed layout

```
⌂ Home | ◀ Back | ▶ Forward | ↻ Refresh | [URL input] | → Go | ↗ Open External
```

### Technical feasibility

The preview iframe and proxy are **same-origin** (content served through `5{PORT}` proxy), so the parent page can access `iframe.contentWindow.history`:

```js
iframe.contentWindow.history.back()    // ◀ Back
iframe.contentWindow.history.forward()  // ▶ Forward
```

### Caveats

- The History API does not expose whether there are entries to go back/forward to. `history.length` counts total entries, not position. There is no `history.index` or `history.canGoBack`.
- Buttons will always be enabled and silently no-op if there is nothing to navigate to.
- If the proxied app uses `pushState`/`replaceState` (SPA routing), back/forward will navigate within the SPA as expected.

### Verdict

Works. Clean to implement. Minimal code change.

---

## 2. Debug Channel `eval` Support

### Current command handling (debugInjectJS in main.go, line ~1441)

The injected JS handles incoming WebSocket messages from the agent:

```js
ws.onmessage = function(e) {
  var cmd = JSON.parse(e.data);
  if (cmd.t === 'query') {
    var el = document.querySelector(cmd.selector);
    send({ t: 'queryResult', id: cmd.id, found: !!el, text: el ? el.textContent : null, ... });
  }
};
```

### Proposed addition

```js
if (cmd.t === 'eval') {
  try {
    var result = (0, eval)(cmd.code);  // indirect eval = global scope
    send({ t: 'evalResult', id: cmd.id, result: serialize(result) });
  } catch (e) {
    send({ t: 'evalResult', id: cmd.id, error: e.message, stack: e.stack });
  }
}
```

Agent sends: `{ t: 'eval', id: 'unique-id', code: 'document.title' }`
Iframe responds: `{ t: 'evalResult', id: 'unique-id', result: 'My Page Title' }`

### Async eval

For promises, wrap with async support:

```js
if (cmd.t === 'eval') {
  try {
    var result = (0, eval)(cmd.code);
    Promise.resolve(result).then(function(val) {
      send({ t: 'evalResult', id: cmd.id, result: serialize(val) });
    }).catch(function(err) {
      send({ t: 'evalResult', id: cmd.id, error: err.message });
    });
  } catch (e) {
    send({ t: 'evalResult', id: cmd.id, error: e.message, stack: e.stack });
  }
}
```

### Security analysis

Per ADR-024, the debug channel is behind the same `forwardAuth` middleware as the preview itself. An agent that can connect to `/__swe-swe-debug__/agent` already has full access to the user's app. Eval does not meaningfully expand the attack surface — the app being debugged is the user's own app in their own container.

### Verdict

Trivial to add (~15 lines). Acceptable security posture (same trust boundary).

---

## 3. Preview Tab as "Mini Playwright" vs. CDP Multiplexing

### Option A: Multiplex shared Chrome via CDP

Playwright connects to Chrome via CDP (Chrome DevTools Protocol). Each `browser.newContext()` creates an isolated browser context.

**How it would work:**
- Each session gets its own `BrowserContext` within the shared Chrome
- A broker/multiplexer maps session ID to context
- CDP supports targeting specific pages/frames within contexts

**Problems:**
- Need to build a CDP multiplexer or session-aware Playwright server
- MCP Playwright tools operate on "the current page" — not designed for multi-session targeting
- Chrome resource usage scales with contexts (memory per context)
- Chrome crash = all sessions lose their browser
- Adds a hard dependency on Chrome being available in the host

### Option B: Enhance debug channel to be a "mini Playwright"

Extend the existing injected JS and WebSocket relay to support structured commands.

**Proposed command set:**

| Command | Agent sends | Iframe does |
|---------|------------|-------------|
| `eval` | `{ t: 'eval', id, code }` | `(0, eval)(code)`, return result |
| `click` | `{ t: 'click', id, selector }` | `querySelector(sel).click()` |
| `fill` | `{ t: 'fill', id, selector, value }` | Set `.value`, dispatch `input` + `change` events |
| `getText` | `{ t: 'getText', id, selector }` | Return `textContent` of matched element |
| `getAttribute` | `{ t: 'getAttribute', id, selector, attr }` | Return attribute value |
| `snapshot` | `{ t: 'snapshot', id }` | Serialize DOM into accessibility-like tree |
| `navigate` | `{ t: 'navigate', id, url }` | `window.location.href = url` |
| `back` | `{ t: 'back', id }` | `history.back()` |
| `forward` | `{ t: 'forward', id }` | `history.forward()` |
| `waitForSelector` | `{ t: 'waitFor', id, selector, timeout }` | Poll with MutationObserver, respond when found or timeout |
| `getUrl` | `{ t: 'getUrl', id }` | Return `window.location.href` |
| `getTitle` | `{ t: 'getTitle', id }` | Return `document.title` |

**Advantages over CDP multiplexing:**
- Already per-session isolated (no multiplexing needed)
- No shared Chrome dependency or crash blast radius
- Works through the same proxy/auth boundary
- Agent uses the existing `/__swe-swe-debug__/agent` WebSocket endpoint
- No new infrastructure — just extending injected JS

**Limitations vs. real Playwright:**
- No true screenshots (only DOM snapshots; html2canvas is lossy)
- No network interception (fetch/XHR are monitored but not interceptable)
- No cookie/storage manipulation beyond what eval provides
- No PDF generation
- No file download interception
- No geolocation/permission mocking

### Recommendation

**Option B is the pragmatic path.** The debug channel already provides 80% of the infrastructure. Adding structured commands gives agents useful browser control without CDP multiplexing complexity. Real Playwright via CDP remains better suited for the test-container workflow where there is a dedicated Chrome instance per test run.

### Architecture for Option B

```
Agent CLI / MCP tool
    │
    │  WebSocket: /__swe-swe-debug__/agent
    ▼
┌──────────┐
│ DebugHub │  (per-session, in swe-swe-server)
│  (relay)  │
└──────────┘
    │
    │  WebSocket: /__swe-swe-debug__/ws
    ▼
┌──────────────────┐
│ debugInjectJS    │  (injected into every proxied HTML page)
│ command handler  │
└──────────────────┘
    │
    ▼
  User's app DOM (querySelector, eval, history, etc.)
```

The DebugHub relay (Go) does not need to understand command types — it remains a dumb pipe. All command interpretation happens in the injected JS. This keeps the Go server simple and the command set extensible without recompilation.

---

## 4. Live URL Bar Updates (Reflecting In-Iframe Navigation)

### Current behavior

`updateIframeUrlDisplay()` (terminal-ui.js, line 3153) is called on the iframe `load` event. Two problems:

1. **Disabled for preview tab** — line 3155 has `if (this.activeTab === 'preview') return;` which short-circuits the function entirely when the preview tab is active.
2. **Only fires on full page loads** — SPA navigations (`pushState`, `replaceState`, hash changes) do not trigger the `load` event, so the URL bar goes stale.
3. **Shows proxy URL, not logical URL** — `iframe.contentWindow.location.href` returns the proxy URL (e.g. `https://host:53007/about`), not the logical `http://localhost:3007/about`.

### Proposed solution: URL change reporting via debug channel

Add URL change detection to `debugInjectJS`. The injected JS already runs inside every proxied page:

```js
// Report URL changes to parent via debug channel
var lastUrl = location.href;
function checkUrl() {
  if (location.href !== lastUrl) {
    lastUrl = location.href;
    send({ t: 'urlchange', url: location.href });
  }
}

// Catch all navigation types
window.addEventListener('popstate', function() { checkUrl(); });
window.addEventListener('hashchange', function() { checkUrl(); });

// Monkey-patch pushState/replaceState for SPA routing
var origPush = history.pushState;
var origReplace = history.replaceState;
history.pushState = function() { origPush.apply(this, arguments); checkUrl(); };
history.replaceState = function() { origReplace.apply(this, arguments); checkUrl(); };
```

On the parent side (terminal-ui.js), listen for `urlchange` messages on the debug WebSocket and reverse-map the proxy URL to the logical `localhost:{PORT}` URL for display.

### URL reverse-mapping

The proxy URL path maps directly to the logical URL path. Given:
- Proxy base: `https://host:53007`
- Logical base: `http://localhost:3007`
- Proxy URL reported: `https://host:53007/dashboard?tab=settings#section`

Display URL: `http://localhost:3007/dashboard?tab=settings#section`

This is a simple base-URL swap using the pathname + search + hash from the reported URL.

### Why debug channel is better than polling

| Approach | Catches pushState | Catches hashchange | Catches full loads | Overhead |
|----------|:-:|:-:|:-:|----------|
| `load` event only | No | No | Yes | Zero |
| Poll `contentWindow.location` | Yes | Yes | Yes | Timer overhead, same-origin required |
| Debug channel reporting | Yes | Yes | Yes | Zero (event-driven), works even if cross-origin in future |

### What needs to change

1. **`debugInjectJS` in main.go** — add ~12 lines for URL change detection + pushState/replaceState patching
2. **`terminal-ui.js`** — listen for `urlchange` messages on the debug WS, reverse-map proxy URL to display URL, update the URL input field
3. **Remove or fix the early return** on line 3155 (`if (this.activeTab === 'preview') return;`)

### Verdict

Feasible and clean. Fits naturally into the debug channel. Catches all navigation types including SPA routing and anchor changes.

---

## Related files

| File | Role |
|------|------|
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | DebugHub, debug injection proxy, WebSocket endpoints, `debugInjectJS` |
| `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js` | Preview toolbar UI, iframe navigation, setPreviewURL, refreshIframe |
| `cmd/swe-swe/templates/host/swe-swe-server/static/modules/url-builder.js` | URL construction utilities |
| `docs/adr/0024-debug-injection-proxy-security.md` | Security threat model |
| `docs/adr/0025-per-session-preview-ports.md` | Per-session port isolation architecture |
