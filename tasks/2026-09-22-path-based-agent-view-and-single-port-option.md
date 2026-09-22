# Path-based Agent View + a "one fixed port, nothing else" answer on the setup page

**Date**: 2026-09-22
**Status**: DONE -- all 3 phases shipped 2026-09-22 (7602a43be, e48eafdcf,
aa1a8cb4f), unpushed.

Deviations from the plan, all deliberate:

- Phase 2 gained a second e2e assertion in `e2e/tests/ports.spec.js` ("Agent
  View prefers its own port when that port is reachable"). Without it the
  corsWrapper probe fix had no positive coverage, and a regression there would
  silently cost every box its per-origin isolation rather than break anything
  visible.
- Phase 3's test is `www/setup-page.test.mjs` (node --test + Playwright,
  `make test-www`, wired into `make test`) rather than a spec under `e2e/`:
  the page is static and the e2e harness needs a running container. It skips
  itself when Playwright or chromium is missing.
- Phase 3 keeps the version note visible for the new answer and makes its text
  per-answer ("Needs swe-swe 2.39.0 or newer"). The plan said the script must
  match `anyport`'s byte for byte, which it does -- but `ordinary()` also hides
  that note, and the fallback needs a version newer than 2.38.0. **2.39.0 is an
  assumption about the next release number; correct it at release time if the
  bump differs.**
- The `swe-swe-tunnel` answer's note was reworded: it used to open "For a
  machine reachable on one fixed port and nothing else", which now describes
  the new answer as well.

## Goal

A box reachable on exactly ONE port, with no wildcard DNS and no tunnel, runs a
usable swe-swe: terminal, Agent Chat, Files, App Preview AND the live Agent View
all ride the same origin that served the page. The setup page
(`www/index.html`) grows the matching answer so that box is a supported choice
rather than an undocumented degradation.

### Why now

Preview, Agent Chat and Files already resolve subdomain -> port -> same-origin
path (`static/modules/proxy-base.js`, ADR-0033), and
`e2e/tests/proxy-fallback.spec.js` proves it with every non-main port refused at
the browser. Agent View is the ONE pane with no path form:
`getBrowserViewUrl()` in `static/terminal-ui.js` emits either
`{vncProxyPort}.{publicHostname}` or `{hostname}:{vncProxyPort}` and nothing
else, so on a single-port box the pane sits on the browser's own "refused to
connect" page.

For comparison, OpenHands avoids the extra port by not having a live view at
all: `BrowserObservation` events carry a base64 PNG over the existing
conversation websocket and `src/components/features/browser/browser.tsx` renders
an `<img>` -- no interaction, no motion, updated only when the agent acts.
Serving noVNC over a path keeps the live, clickable view AND costs no port, so
it is strictly better than matching them.

### What is still NOT available on such a box

- The public app port (5000-5019 band) -- a second, login-free origin for the
  session's app is a separate origin by definition.
- Per-origin isolation: on the path form every pane is same-origin with the
  terminal page, so a page rendered inside the Files pane can reach the parent
  document. Port/subdomain mode keeps its isolation and stays the default when
  reachable.

## Phases

1. Server: `/proxy/{uuid}/vnc/` same-origin route for noVNC.
2. Frontend: Agent View resolves subdomain -> port -> path, plus the probe fix
   that makes the port form detectable.
3. `www/index.html`: a fifth "My browser can reach it by" answer,
   "one fixed port, nothing else", with its consequence box -- and the page's
   first automated test.

---

## Phase 1 -- Server: same-origin `/proxy/{uuid}/vnc/`

**Achieves**: the main listener serves the noVNC page, its static assets and the
`/websockify` WebSocket upgrade for a session, behind the same auth and
session-scope rules the per-port VNC listener uses, including the remote
browser-backend case (`sess.RemoteVNCTarget`).

### Steps

1. **RED** -- `cmd/swe-swe/templates/host/swe-swe-server/proxy_listener_test.go`,
   alongside the existing `TestVNCReverseProxyDirectorRewritesHost` /
   `TestVNCAuthWrap*`. Stand up an `httptest` websockify double serving
   `/vnc_lite.html` and echoing on `/websockify`, then assert:
   - a. `GET /proxy/{uuid}/vnc/vnc_lite.html` -> 200 and the double's body.
   - b. a WebSocket upgrade at `/proxy/{uuid}/vnc/websockify` echoes.
   - c. no auth cookie -> refused (both a and b).
   - d. a scoped cookie for a DIFFERENT session -> refused (cf.
     `session_scope_enforce_test.go`).
   All four fail: the route does not exist.
2. **GREEN** -- `main.go`, in the per-session block that already registers
   `/proxy/{uuid}/preview/`, `/agentchat/` and `/files/` on `sessMux`. Hoist the
   `vncTarget` / `vncReverseProxy` construction above `sessMux` and register:

   ```go
   sessMux.Handle("/proxy/"+sess.UUID+"/vnc/", http.StripPrefix(
       "/proxy/"+sess.UUID+"/vnc",
       remoteBrowserVNCKeepalive(sess, vncReverseProxy)))
   ```

   One proxy instance, shared with the per-port listener, so the Director's
   `RemoteVNCTarget` branch and the keepalive apply to both forms.
   `vnc_lite.html` references its assets relatively (`./core/...`), so
   `StripPrefix` needs no body rewriting -- verify in step 4, and fall back to
   an `agentproxy.New` with `BasePath` + `NoInject` if any root-absolute
   reference turns up.
3. **REFACTOR** -- no behaviour change; keep the per-port listener wiring
   untouched.
4. `make test`.

### Non-regression

- `TestVNCReverseProxyDirectorRewritesHost`,
  `TestVNCAuthWrapBlocksWebSocketUpgradeWithoutCookie`,
  `TestVNCAuthWrapAllowsWebSocketUpgradeWithCookie` stay green unchanged.
- The per-port listener is untouched: boxes that reach `:27000+` behave exactly
  as today.
- No frontend consumes the new route yet.

**Estimate**: ~1.5h.

---

## Phase 2 -- Frontend: Agent View picks the reachable form

**Achieves**: Agent View uses the shared resolver
(`proxyCandidates` + `resolveProxyBase`) like every other pane, so it lands on
the path form when neither subdomain nor port is reachable.

### Steps

1. **Probe fix (RED first)** -- the per-port VNC handler is NOT wrapped in
   `corsWrapper`, so `GET :{vncProxyPort}/__probe__` never returns the
   `X-Agent-Reverse-Proxy` marker and the port candidate would ALWAYS lose.
   Go test asserting the marker on the VNC listener, then wrap `vncHandler` in
   `corsWrapper` in `main.go` exactly as preview/agentchat/files are.
2. **RED** -- `static/modules/url-builder.test.js`: a path builder for the VNC
   pane. Cases: subdomain reachable; port reachable; neither -- expect
   `/proxy/{uuid}/vnc/vnc_lite.html?path=proxy/{uuid}/vnc/websockify&...`
   (noVNC's `path=` query param is how it is told where its WebSocket lives).
3. **GREEN** -- `static/terminal-ui.js`: resolve once per session and cache
   (`_browserViewResolvedBase`), mirroring the Files pane's
   `_filesResolvedBase` / `_filesBaseResolving` pattern at ~line 6045. Candidate
   order stays subdomain -> port -> path (trusted).
4. Update the two synchronous callers of `getBrowserViewUrl()` -- the services
   list (~line 1441, "open in new tab") and the panel switch (~line 4220) -- to
   use the cached base, falling back to the path form rather than `null`.
5. **E2E RED->GREEN** -- extend `e2e/tests/proxy-fallback.spec.js` (which already
   refuses every non-main port at the browser) with an Agent View case: open the
   pane, wait for the noVNC iframe to report a connected RFB session.
6. `make test`, then `make e2e-up-simple && make e2e-test && make e2e-down`.

### Non-regression

- `e2e/tests/agent-browser.spec.js` (ordinary box) and
  `agent-view-remote.spec.js` (remote browser-backend) must stay green: with a
  reachable port, the resolver picks the port form, which is today's URL.
- `e2e/tests/public-hostname.spec.js` covers the subdomain form.
- The same-origin `vnc-ready` readiness probe (ADR-0040) is untouched; only the
  iframe URL selection changes.

**Estimate**: ~2.5h.

---

## Phase 3 -- Setup page: "one fixed port, nothing else"

**Achieves**: `www/index.html` offers a fifth answer to "My browser can reach it
by", with a consequence box that states plainly what works and what does not.

### Steps

1. Add the radio after `reach-anyport`, before `reach-tailscale`:
   `value="oneport"`, label "one fixed port, nothing else", note naming the two
   real cases (a VPS with one open port; a corporate network that blocks the
   rest).
2. Consequence box (`#oneport-yes`, rendered when `state.reach === 'oneport'`):
   - works: terminal, Agent Chat, Files, App Preview and the live Agent View,
     all on the one address;
   - not available: the login-free public app port;
   - caveat: panes share the page's origin, so a page opened in Files can reach
     the parent document.
3. No script change: `envPairs()` gains nothing, and the generated script must
   be byte-identical to the `anyport` one. Assert that in the test.
4. A `<details class="how">` "how do I check?" matching the other answers: if a
   second port on this machine does not open from your browser, pick this.
5. **Tests (new)** -- the page has none today. Add a Playwright spec that serves
   it with `node www/serve.cjs` and, for each of the five answers, asserts the
   generated script and which consequence blocks are visible. Written
   new-answer-first so it fails before step 1.
6. Before/after screenshots via the local chromium + `e2e/node_modules/playwright`
   (the MCP browser cannot reach this container's ports).

### Non-regression

- The new spec covers the four existing answers, which are currently untested.
- `wildcardRoute()` / `needsTunnel()` / `ordinary()` keep their current
  meanings; `oneport` is none of them, so the tunnel steps and the
  `SWE_PUBLIC_HOSTNAME` line stay hidden.

**Estimate**: ~1.5h. Total: ~5.5h.

---

## Deliberately out of scope

- Forcing Agent Chat / Files onto the path form always. Discussed and deferred:
  the probe already picks the path when the port is unreachable, and always-path
  would give up Files' origin isolation, which matters because Files can render
  project HTML.
- A "skip the probes" switch for boxes that DROP rather than refuse (each pane
  waits up to `PROBE_TIMEOUT` = 5s). Worth revisiting as its own change.
- Screenshot-style Agent View (the OpenHands model). More work, less capability.
