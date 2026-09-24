# Agent View tab: explain why it is unavailable instead of hiding it

**Date**: 2026-09-24
**Status**: PLANNED
**Sketch**: `mockups/lo-fi/2026-09/24-agent-view-unavailable-tab-page.html`
(committed 1173afbf7), example image `mockups/lo-fi/2026-09/24-agent-view-example.png`.

## Goal

Today, when Agent View cannot work, the tab is removed
(`setAgentViewTabVisible(false)`, `agentViewKnown()` in
`static/modules/pane-availability.js`). People who skipped the browser question
on the setup page never learn what they gave up. Instead, keep the tab and show
a page that says:

1. What Agent View is (one line).
2. An example screenshot.
3. Why it is missing HERE (one of the cases below).
4. That the agent cannot use a browser either.
5. How to get it: install here / use another machine / leave it off.
6. A "Hide this tab" button.

## Cases

| Case | Server knows it by | Headline |
|---|---|---|
| A. switched off | `agentViewBackend == "off"` | You switched it off during setup |
| B. programs missing | local mode, `browserStackAvailable()` false | These programs are missing: [list] |
| C. other machine unreachable | remote mode, `startRemoteAgentView` errors | Can't reach the browser machine at [address] |

Case C is broken today in a different way: the tab shows and sits on
"Starting browser..." forever.

## Phase 1: server reports the reason (baseline, no UI change)

1. `browser_backend.go`: split `browserStackAvailable()` into
   `missingBrowserPrograms() []string` (returns e.g. `["chromium","x11vnc"]`,
   treating chromium/chromium-browser as one) and keep
   `browserStackAvailable()` as `len(missing)==0`.
2. Add `agentViewUnavailableReason() string` -> `""` / `"off"` / `"missing"`.
3. `main.go` status payload (~line 1213): add `agentViewReason` and
   `agentViewMissing` next to `agentViewAvailable`. Keep `agentViewAvailable`
   unchanged so old frontends still behave.
4. Tests in `browser_backend_test.go` (fake `lookPath`, as the existing tests do)
   and `single_port_test.go` key list.

Verify: `make test`; status frame carries the new keys.

## Phase 2: the page (cases A and B)

1. New `static/agent-view-unavailable.html` (+ its CSS inline, theme from the
   `swe-swe-theme` cookie like `vnc_lite.html`). Reads `?reason=off|missing&missing=a,b`.
   Content follows the sketch. Install line = the setup page's `APT_LINE`
   (`www/index.html:838`); "Use another machine" links to
   `https://swe-swe.netlify.app/` browser section.
2. Example image: copy the cropped PNG to `static/images/agent-view-example.png`
   (shrink to ~40 KB, e.g. 800px wide, reduced palette). It is embedded in the
   binary via `//go:embed all:static`.
3. `pane-availability.js`: `agentViewKnown` becomes true whenever `uuid` is known
   and the status frame has arrived (reason or available), not only when available.
   Update `pane-availability.test.js`.
4. `terminal-ui.js`: `getBrowserViewUrl()` returns the placeholder URL when not
   available. Audit its three callers (services list, panel switch,
   `panePopoutUrl`) -- they treat `null` as "no such service"; decide per caller.
   Do NOT auto-start the browser (`browser/start`) for an unavailable pane.
5. Tab label: keep "Agent View"; no auto-focus of this tab on session start.

Verify: `make test`, `make build golden-update` (template change), then test
container + MCP browser: session with `SWE_AGENT_VIEW=off` shows case A; a
container with chromium removed shows case B with the right list.

## Phase 3: "Hide this tab"

1. Button posts a message to the parent frame; parent stores
   `swe-swe-hide-agent-view=1` in localStorage (try/catch) and removes the tab.
2. Way back: a "Show Agent View tab" toggle in Session Settings.

## Phase 4 (optional): case C, other machine unreachable

1. Where `startSessionAgentView` returns an error in remote mode, record it on
   the session and push `agentViewReason: "unreachable"` + the address.
2. Replace the endless "Starting browser..." with the page's case C.

## Estimates

Phase 1: 1.5 h. Phase 2: 2.5 h. Phase 3: 1 h. Phase 4: 2 h.

## Open questions

- Should the page also appear in the mobile pane dropdown? (Assumed yes.)
- Non-Debian hosts (macOS dockerless): the apt line is wrong there. Show it only
  when the server reports a Debian-like OS, else link to the setup page.
