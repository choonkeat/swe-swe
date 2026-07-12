# Bug: Files tab stuck on "Connecting to files…" (no retry / no reconnect)

**Reported:** 2026-07-12 (mobile, 4G)
**Component:** swe-swe frontend Files pane (iframe pane-host)
**Severity:** medium — intermittent, self-inflicted dead-end (only a manual
reload clears it, and mobile users don't know to do that)

---

## Symptom

On mobile the Files tab sometimes sits forever on the
`● Connecting to files…` placeholder and never renders the listing. Other
times (same session, same device) it loads instantly. Reloading the page
clears it every time. Two screenshots from the reporter: one stuck on
"Connecting to files…", one showing the same session's README rendered fine.

## Evidence — the backend is NOT the problem

Checked the container while a tab was reportedly hanging:

- md-serve for the workspace was alive and answering: `GET http://localhost:9005/`
  → **HTTP 200 in ~6ms** (other session servers `:9000/:9001/:9003` too). All
  bound and `LISTEN`ing.
- So when the overlay is stuck, the file server behind it is up and instant.

Request path (from `2026-05-24-files-tab-md-serve.md`):

```
Browser (Files tab iframe)  -->  filesProxy :29000 (requireAuthCookie + cors)
tunnel: https://29000.{unique}.{tunnelhost}/  --tunneld demux--> 127.0.0.1:29000
        -->  reverse-proxy to :9000  -->  md-serve -dir <workDir> -addr :9000
```

The failure is in the **first hop** — the iframe's initial load through the
tunnel — not in md-serve.

## Root cause (hypothesis)

The Files pane is a **plain iframe** (Phase 5, `terminal-ui.js`; md-serve
renders full pages so there's no shell-page wrapper). The
`Connecting to files…` overlay is dismissed on the iframe's `load` event.

An `<iframe>` does **not** auto-retry a failed navigation. If that first GET
fails or is dropped — cold tunnel re-establishment, a backgrounded tab whose
connection was reaped, or a weak-signal timeout — the `load` event never
fires, the overlay never clears, and nothing re-attempts. There is no
`error`/timeout → retry, and no reload when the tab regains focus or
visibility. This is exactly why it's intermittent on mobile and always fixed
by a manual reload.

Preview/Agent-Chat panes share this iframe shape, so they likely have the
same latent issue; fix it in the shared pane-host if practical.

## Suggested fix

Give the Files iframe pane-host a small load-supervisor. Three parts:

1. **Load timeout → retry with capped backoff.** After setting `iframe.src`,
   start a watchdog. If `load` hasn't fired within ~4s, re-assign `src`
   (cache-busting query param so the browser actually re-requests) and try
   again. Also retry on the iframe `error` event.
   - **Retry fast, cap the backoff:** e.g. 1s, 2s, 4s, 8s → cap at ~15s, with
     small jitter. Keep retrying indefinitely (don't give up — the backend is
     usually fine and comes back), but never hammer faster than the cap.
   - Clear the watchdog and reset the backoff to the floor on a successful
     `load`.

2. **Reload on focus / tab switch / visibility.** When the pane becomes
   active again and it is **not** in the loaded state, kick an immediate retry
   (reset backoff to floor first, so a returning user waits ~0s, not ~15s):
   - `document.visibilitychange` → `document.visibilityState === 'visible'`
   - `window` `focus`
   - the app's own tab/pane-activated event (whatever fires when the user
     selects the Files pane)

3. **Overlay reflects state.** While retrying, keep/return the overlay to
   "Connecting to files…" (optionally "Reconnecting…"); hide it only on
   `load`. Consider a manual "Retry" affordance on the overlay as a belt-and-
   suspenders for mobile.

### Sketch

```js
// inside the files pane-host setup
let backoff = 1000;               // floor
const MAX = 15000;                // cap
let timer = null, done = false;

function attempt() {
  clearTimeout(timer);
  const bust = (iframe.src.includes('?') ? '&' : '?') + '_r=' + retryToken();
  iframe.src = filesUrl + bust;   // cache-busted so it truly reloads
  showOverlay('Connecting to files…');
  timer = setTimeout(retry, 4000); // load watchdog
}
function retry() {
  if (done) return;
  backoff = Math.min(backoff * 2, MAX);
  timer = setTimeout(attempt, backoff + jitter());
}
iframe.addEventListener('load',  () => { done = true; backoff = 1000; clearTimeout(timer); hideOverlay(); });
iframe.addEventListener('error', () => { done = false; retry(); });

// focus / visibility / pane-activate -> instant retry with reset backoff
function kick() { if (!done) { backoff = 1000; attempt(); } }
document.addEventListener('visibilitychange', () => document.visibilityState === 'visible' && kick());
window.addEventListener('focus', kick);
onPaneActivated('files', kick);
```

`retryToken()`/`jitter()`: derive from a monotonic counter — this repo's
codebase forbids `Date.now()`/`Math.random()` in some contexts; use whatever
the existing modules use for cache-busting.

## Anchors

- `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js`
  - Files pane-host / iframe setup added in Phase 5 (`PANES_IN_ORDER`,
    `PANE_LABELS`, the `files` iframe pane).
  - `filesProxyPort` stored from the Status message; `filesUrl` built via
    `buildSubdomainFilesUrl` / `buildPortBasedFilesUrl`
    (`static/modules/url-builder.js`).
- Placeholder string `Connecting to files…` — grep the static assets.
- Prior design: `tasks/2026-05-24-files-tab-md-serve.md` (Phase 5 +
  end-to-end flow diagram).

## Acceptance criteria

- Kill/pause md-serve (or block the tunnel) briefly, open the Files tab: the
  overlay shows, and the pane **auto-recovers within a few seconds** of the
  backend coming back — no manual page reload.
- Background the tab for a while, return to it: the Files pane reloads on
  becoming visible if it wasn't already loaded, near-instantly (backoff reset).
- Retries are rate-limited to the backoff cap (verify no tight reload loop in
  the network panel when the backend is truly down).
- Happy path unchanged: when md-serve is up, the tab loads once and the
  watchdog is cleared (no periodic reloads).

## Resolution (2026-07-12)

Implemented in two phases (scope: files/shell/browser — the shared
`setIframeUrl` path; preview deferred per its own `setPreviewURL` path +
manual ↻).

- **Phase 1** — new pure module
  `static/modules/iframe-load-supervisor.js`: `IframeLoadSupervisor` with a
  4s load watchdog, capped exponential backoff (1s→2s→4s→8s→15s, reusing
  `reconnect.js`), monotonic cache-bust tokens (no `Date.now`/`Math.random`),
  `error`-event retry, and `kick()` (immediate retry + backoff reset for
  focus/visibility/pane-activate). 15 unit tests (`node --test`).
- **Phase 2** — wired into the shared `setIframeUrl` so files/shell/browser
  panes are supervised; added `visibilitychange`/`window focus` +
  pane-activate kicks and `cleanup()`. Overlay shows `Reconnecting...` while
  retrying. Fixed a browser-only `Illegal invocation` in the default timer
  wiring (found via live e2e; injected-clock unit tests couldn't surface it).

All four acceptance criteria verified live (`make e2e-up-simple`, real
browser): auto-recover, reload-on-visible, rate-limited backoff (no tight
loop), and unchanged happy path. Logs: `*-phase1.log`, `*-phase2.log`.
