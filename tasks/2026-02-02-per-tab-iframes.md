# Per-Tab Iframes

**Spec**: `research/2026-02-02-preview-tab-navigation-spec.md` (section "Per-tab iframes")

## Goal

Replace the single shared `<iframe>` (which gets its `src` swapped on every tab switch, destroying state) with **per-tab iframes** created on demand and shown/hidden via CSS. This preserves each tab's state across switches.

## Key Files

- `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js` — Main UI logic
- `cmd/swe-swe/templates/host/swe-swe-server/static/styles/terminal-ui.css` — Layout and styling

## Testing Approach

Use dev server (`make run`) + `?preview` mode for iteration. See `docs/dev/swe-swe-server-workflow.md`.

---

## Progress

- [ ] Phase 1: Per-tab iframe creation and show/hide
- [ ] Phase 2: Toolbar binding to preview iframe only
- [ ] Phase 3: Close pane cleanup, mobile nav, and shell exit behavior
- [ ] Phase 4: Golden update and final regression verification

---

## Phase 1: Per-tab iframe creation and show/hide

### What will be achieved

Replace the single `<iframe class="terminal-ui__iframe">` with per-tab iframes created on first use. Switching tabs toggles `display: block/none` instead of swapping `src`. Each tab's iframe preserves its state across switches.

### Steps

1. **Add CSS for per-tab iframe visibility** — Add a `.terminal-ui__iframe.active` rule (`display: block`) and default iframes to `display: none`. Remove any existing single-iframe display logic that conflicts.

2. **Refactor `openIframePane(tab, url)`** — Instead of setting `src` on a shared iframe, check if a per-tab iframe already exists (keyed by tab name, e.g. `tab-preview`, `tab-vscode`). If not, create one, append it to `terminal-ui__iframe-container`, and set its `src`. If it exists, reuse it. Then set `.active` class on the target iframe and remove `.active` from all others.

3. **Refactor `switchPanelTab(tab)`** — Same logic: show the target tab's iframe, hide others. Don't re-set `src` if the iframe already exists.

4. **Refactor `setPreviewURL()` and `setIframeUrl()`** — These should target the correct per-tab iframe instead of a single shared `this.iframe`. `setPreviewURL` targets the preview iframe; `setIframeUrl` targets the iframe for the given tab.

5. **Refactor `closeIframePane()`** — Iterate all per-tab iframes, set each to `about:blank`, remove them from the DOM, and clear the internal tracking map.

6. **Update placeholder logic** — The loading placeholder should be per-tab or at least only visible when the active tab's iframe hasn't loaded yet.

7. **Store per-tab iframes in a map** — Add `this._tabIframes = {}` keyed by tab name, so we can look up existing iframes.

### Verification

1. **Golden tests**: `make build golden-update` — verify diff only shows structural changes (no extra iframes in initial HTML since they're created on demand via JS).

2. **Manual browser test via dev server**:
   - `make run`, navigate to `http://swe-swe:3000/session/test123?assistant=claude&preview`
   - Open preview tab → iframe created, content loads
   - Switch to vscode tab → preview iframe hidden (not destroyed), vscode iframe created
   - Switch back to preview → preview iframe shown immediately (no reload)
   - Verify via browser DevTools: multiple `<iframe>` elements in `terminal-ui__iframe-container`, only one with `display: block`
   - Close pane → all iframes removed from DOM

3. **Regression check**: Opening a single tab works identically to before (no visual diff, URL bar works, nav buttons work for preview).

---

## Phase 2: Toolbar binding to preview iframe only

### What will be achieved

Ensure the URL bar, nav buttons (back/forward/reload/home), and debug WebSocket communication remain correctly wired to the **preview tab's iframe only**, regardless of which tab is active. When switching away from preview and back, the toolbar state (URL, button enabled/disabled) is preserved.

### Steps

1. **Toolbar visibility already handled** — The existing code adds/removes `show-toolbar` class based on whether `activeTab === 'preview'`. Verify this still works with the per-tab iframe refactor and no changes are needed.

2. **Debug WebSocket targets preview iframe** — Verify that `this._debugWs` message handlers (`urlchange`, `navstate`) update the URL bar and button state regardless of which tab is visible. These should write to the toolbar state unconditionally since the toolbar is only shown for preview anyway.

3. **Navigation commands target preview** — Verify that Home, Back, Forward, Reload, Go all send WebSocket commands that reach the shell page inside the preview iframe. Since the shell page maintains its own WebSocket connection, this should work without changes — but confirm the commands aren't accidentally routed to a different iframe.

4. **Preserve toolbar state across tab switches** — When switching away from preview and back, the URL bar should still show the last known URL and back/forward buttons should retain their enabled/disabled state. No re-initialization should happen. Verify the `urlchange` and `navstate` handlers store state in properties (not just DOM) so the toolbar can be repopulated if needed.

5. **`refreshIframe()` targets preview** — The refresh fallback path (when no debug WS) currently does `this.iframe.src = this.iframe.src`. Update this to target the preview tab's iframe specifically from the `_tabIframes` map.

### Verification

1. **Manual browser test**:
   - Open preview tab, navigate to a few pages, confirm URL bar updates
   - Switch to vscode tab — toolbar hidden
   - Switch back to preview — toolbar visible, URL bar shows last URL, back/forward buttons in correct enabled/disabled state
   - Click Back — navigates correctly in the preview iframe (not the vscode iframe)
   - Click Reload — reloads preview content, not whatever other tab was last active

2. **Regression check**: All nav buttons (home, back, forward, reload, go, open-external) work identically to current behavior when only the preview tab is used.

3. **Golden tests**: `make build golden-update` — no unexpected diffs since toolbar HTML is unchanged, only JS behavior differs.

---

## Phase 3: Close pane cleanup, mobile nav, and shell exit behavior

### What will be achieved

Ensure that closing the right pane properly cleans up all per-tab iframes (freeing memory), that mobile navigation works correctly with per-tab iframes, and that exiting the shell tab returns to the preview tab with its state preserved.

### Steps

1. **`closeIframePane()` destroys all iframes** — Iterate `this._tabIframes`, set each iframe `src = 'about:blank'`, remove from DOM, then clear the map (`this._tabIframes = {}`). This ensures no background iframes consume memory or maintain WebSocket connections when the pane is closed.

2. **Re-opening after close creates fresh iframes** — After closing and re-opening a tab, the iframe should be re-created from scratch (new `src` set, new WebSocket connection from shell page for preview). Verify `openIframePane` handles the "map is empty" case correctly.

3. **Shell exit switches to preview** — When the shell tab's iframe session ends (user types `exit` in the shell), detect the session close and switch back to the preview tab. The preview iframe should still be alive in `_tabIframes` with its full state. The shell tab's iframe should be cleaned up (removed from map, set to `about:blank`).

4. **Mobile `switchMobileNav()` uses per-tab iframes** — The mobile dropdown calls `switchPanelTab()` internally. Verify this path works with per-tab iframes — creating on first use, showing/hiding on switch.

5. **Mobile view class toggling** — `mobile-view-workspace` and `mobile-view-terminal` classes control which panel is visible on mobile. Verify these interact correctly with the per-tab iframe visibility.

### Verification

1. **Manual browser test — shell exit flow**:
   - Open preview tab, navigate to a page (confirm URL bar shows a non-root path)
   - Switch to shell tab — shell iframe created, preview iframe hidden but alive
   - Click into the shell terminal, type `e`, `x`, `i`, `t`, `Enter` character by character
   - Shell session ends → UI switches back to preview tab
   - Preview iframe is shown immediately with its previous state (same URL, same page content, no reload)
   - Shell iframe is cleaned up from the DOM

2. **Manual browser test — close pane**:
   - Open preview, switch to vscode, then close pane
   - Inspect DOM: no iframes remain in `terminal-ui__iframe-container`
   - Re-open preview: fresh iframe created, shell page loads, URL bar shows `localhost:PORT/`
   - Confirm no stale WebSocket connections

3. **Manual browser test — mobile viewport**:
   - Resize browser to <768px or use DevTools responsive mode
   - Use dropdown to switch between terminal, preview, vscode, shell, browser
   - Verify each tab loads correctly on first use
   - Switch away and back — content preserved (not reloaded)
   - Switch to terminal view — workspace iframes hidden but not destroyed

4. **Regression check**: Close/reopen cycle works identically to current behavior.

5. **Golden tests**: `make build golden-update` — verify no unexpected diffs.

---

## Phase 4: Golden update and final regression verification

### What will be achieved

Ensure all golden test snapshots are updated, all tests pass, and the full feature is verified end-to-end via browser.

### Steps

1. **Run `make test`** — Confirm all existing unit tests pass with the code changes.

2. **Run `make build golden-update`** — Rebuild the binary with embedded templates, regenerate golden snapshots.

3. **Inspect golden diff** — `git diff --cached -- cmd/swe-swe/testdata/golden` to verify diffs are limited to:
   - CSS changes (per-tab iframe visibility rules)
   - JS changes (per-tab iframe creation/management, no single shared iframe)
   - No unexpected changes to HTML structure (iframes are created dynamically, not in initial HTML)

4. **Full browser verification via dev server** — Start `make run`, navigate to preview session page, run through the complete test matrix:
   - Open preview → works, URL bar updates
   - Switch to vscode → preview preserved, vscode loads
   - Switch to shell → both preview and vscode preserved, shell loads
   - Switch back to preview → immediate, no reload, URL bar correct, nav buttons correct state
   - Type `exit` in shell → returns to preview, shell cleaned up
   - Close pane → all iframes destroyed
   - Reopen preview → fresh iframe, loads from scratch
   - Mobile viewport → dropdown switching works, state preserved

5. **Stop dev server** — `make stop`

### Verification

This phase **is** the verification. Green CI (`make test` passes) + golden diffs look correct + manual browser walkthrough confirms no regressions.
