# Task: Always-Available Split-Pane UI with Tab Toggle

## High-Level Goal

Remove the `--basic-ui` flag and make the split-pane UI with iframe always available as a toggle, where:
- Terminal starts at 100% width (backward compatible)
- Status bar tabs (shell, vscode, preview, browser) toggle iframe pane open/closed
- Only "preview" tab shows a toolbar (home/refresh) and helpful loading screen
- Clicking active tab closes and destroys iframe to save memory
- Desktop: regular clicks open in iframe; cmd/ctrl+click opens new tab
- Mobile: all tabs open in new tabs (no iframe)

## Key Constants

- `MIN_PANEL_WIDTH = 360px`
- `RESIZER_WIDTH = 8px`
- Desktop breakpoint = 360 × 2 + 8 = **728px**

## Behavior Matrix

| Tab | Regular Click (Desktop) | Cmd/Ctrl+Click | Right Click | Mobile |
|-----|------------------------|----------------|-------------|--------|
| shell | → iframe | → new tab | → context menu | → new tab |
| vscode | → iframe | → new tab | → context menu | → new tab |
| preview | → iframe | → new tab | → context menu | → new tab |
| browser | → iframe | → new tab | → context menu | → new tab |

## Toggle Behavior

- Click inactive tab → open iframe with that content
- Click active tab → close iframe, destroy it, return to 100% terminal
- Switch tabs (click different tab while one is active) → reuse iframe, just change `src`, keep widths

## Resize Constraints

- Terminal minimum: 360px (cannot close via drag)
- Iframe minimum: 360px (cannot close via drag)
- To close right panel: click active tab
- Left panel (terminal) cannot be closed

---

# Phase 1: Refactor UI to Always-Available Split-Pane ✅ COMPLETE

## What Will Be Achieved

The split-pane HTML structure will always be present in the DOM, but the iframe pane starts hidden. Terminal occupies 100% width by default. This maintains backward compatibility while enabling the toggle behavior in later phases.

## Steps

1. **Remove `--basic-ui` flag from CLI parsing** (`cmd/swe-swe/main.go`)
   - Remove `basicUiEnabled` flag definition
   - Remove `basicUi` variable and related logic
   - Remove `BasicUi` field from `InitConfig` struct

2. **Remove `{{IF BASIC_UI}}` conditionals from templates**
   - `templates/host/docker-compose.yml` - keep the preview port/env vars unconditionally
   - `templates/host/traefik-dynamic.yml` - if any BASIC_UI conditionals exist

3. **Update `terminal-ui.js` HTML structure**
   - Split-pane structure always present (terminal + resizer + iframe-pane)
   - Iframe pane starts with `display: none` or a `.hidden` class
   - Remove `initBasicUi()` detection logic (no longer conditional)

4. **Update CSS for default hidden state**
   - Terminal at 100% width when no iframe visible
   - Resizer hidden by default (only visible when iframe pane is shown)
   - Iframe pane hidden by default
   - Replace `.basic-ui` class with `.iframe-visible` class that shows resizer + iframe pane together

5. **Update `main.go` template processing**
   - Remove `basicUI` parameter from `processSimpleTemplate()`
   - Remove BASIC_UI conditional handling

6. **Always generate `app-preview.md`**
   - Remove the conditional check for `basicUi != ""`
   - Always include `templates/container/.swe-swe/docs/app-preview.md` in container files
   - Preview is always available, so documentation should always be present

## Verification

**Before changes:**
- `swe-swe init` without `--basic-ui` → no split-pane structure in HTML
- `swe-swe init --basic-ui` → split-pane structure present

**After changes:**
- `swe-swe init` → split-pane structure always present
- `swe-swe init` → terminal at 100% width (iframe pane hidden)
- `swe-swe init` → no `--basic-ui` flag recognized

**Regression checks:**
- `make test` passes
- `make build golden-update` → verify golden diffs show only removal of basicUi-related content
- Manual (test container + MCP browser): terminal displays full width
- Manual: service links still work (open new tabs)

---

# Phase 2: Implement Tab Toggle Behavior ✅ COMPLETE

## What Will Be Achieved

Status bar service links become toggles that open/close the iframe pane. Clicking an inactive tab opens the iframe with that content. Clicking the active tab closes and destroys the iframe, returning to 100% terminal width.

## Steps

1. **Add state tracking for active tab**
   - Add `activeTab` property (null | 'shell' | 'vscode' | 'preview' | 'browser')
   - null = iframe hidden, terminal 100%

2. **Create `openIframePane(tab, url)` method**
   - If no iframe exists (closed state):
     - Create iframe element
     - Add `.iframe-visible` class to container
     - Show resizer
     - Apply saved width from localStorage (or default 50%)
   - If iframe already exists (switching tabs):
     - Just update `src` (reuse iframe)
     - Keep panel widths unchanged
   - Update `activeTab` state
   - Update visual indicator on status bar

3. **Create `closeIframePane()` method**
   - Set iframe `src = 'about:blank'` (stop content)
   - Remove iframe from DOM entirely
   - Remove `.iframe-visible` class
   - Hide resizer
   - Set terminal to 100% width
   - Clear `activeTab` state
   - Remove visual indicator from all tabs

4. **Update service link click handler**
   - If clicked tab === activeTab → call `closeIframePane()`
   - Else → call `openIframePane(tab, url)`
   - (For now, all clicks trigger this; Phase 3 adds desktop/mobile distinction)

5. **Add active tab visual indicator CSS**
   - Style for active/selected state (e.g., underline, highlight, dot)
   - Consistent with existing status bar styling

6. **Update `savePaneWidth()` and `loadSavedPaneWidth()`**
   - Rename localStorage key from `swe-swe-basic-ui-width` to `swe-swe-iframe-width`
   - Only apply saved width when opening iframe pane

## Verification

**Before changes:**
- Service links open new browser tabs
- No toggle behavior exists
- No active tab indicator

**After changes:**
- Click "vscode" → iframe opens with VS Code, tab shows active indicator
- Click "vscode" again → iframe closes and destroyed, terminal 100%
- Click "shell" → iframe opens with shell content
- Click "preview" → iframe switches to preview content
- Iframe destruction confirmed (check DOM, no lingering iframe when closed)

**Regression checks:**
- `make test` passes
- Resizer still works when iframe is visible
- Width preference saved/restored correctly
- Terminal still functions normally

---

# Phase 3: Click Hijacking for Desktop ✅ COMPLETE

## What Will Be Achieved

On desktop viewports, regular left-clicks on service tabs open content in the iframe. Modifier clicks (cmd/ctrl) and mobile always open in new browser tabs. This provides the best of both worlds - quick inline preview on desktop, but escape hatch to new tab always available.

## Steps

1. **Define minimum panel width constant**
   - `MIN_PANEL_WIDTH = 360`
   - `RESIZER_WIDTH = 8`
   - Used for both resize constraints AND desktop detection

2. **Create `canShowSplitPane()` helper**
   - Returns `window.innerWidth >= (MIN_PANEL_WIDTH * 2 + RESIZER_WIDTH)`
   - Single source of truth for "can we show split pane?"

3. **Create `isRegularClick(event)` helper**
   - Returns true if: `!e.metaKey && !e.ctrlKey && !e.shiftKey && e.button === 0`
   - False for any modifier key or non-left-click

4. **Update click handler for service links**
   ```
   handleTabClick(e, tab, url):
     canSplit = canShowSplitPane()
     isRegular = isRegularClick(e)

     if (canSplit && isRegular):
       e.preventDefault()
       if (tab === activeTab):
         closeIframePane()
       else:
         openIframePane(tab, url)
     else:
       // default behavior: open in new tab (don't preventDefault)
   ```

5. **Ensure links have proper `href` and `target="_blank"`**
   - Links must work as normal links when not intercepted
   - Keeps right-click → "Open in new tab" working
   - Keeps cmd/ctrl+click working

6. **Handle window resize edge case**
   - If iframe is open and window resizes below breakpoint:
     - Keep iframe open (user explicitly opened it)
     - Respect user's choice, don't auto-close

## Verification

**Before changes:**
- All clicks behave the same (Phase 2 behavior)
- No distinction between desktop/mobile
- No modifier key handling

**After changes:**
- Desktop + regular click → opens in iframe (or toggles if active)
- Desktop + cmd/ctrl+click → opens new browser tab
- Desktop + right-click → context menu works, "Open in new tab" works
- Mobile (narrow viewport) + any click → opens new browser tab
- Resize to mobile while iframe open → iframe stays open

**Regression checks:**
- `make test` passes
- All service links still have valid `href` attributes
- Links work without JavaScript (graceful degradation)
- Keyboard navigation still works (Enter on focused link)

---

# Phase 4: Preview Toolbar and Helpful Loading Screen ✅ COMPLETE (core)

## What Will Be Achieved

Only the "preview" tab shows a toolbar in the iframe pane (home/refresh buttons). When the preview is loading or fails to connect, a helpful screen appears with copy-paste friendly prompts to guide users on starting a dev server.

## Steps

1. **Add toolbar HTML structure to iframe pane**
   - Container div for toolbar (above iframe)
   - Home button (🏠) - navigates iframe to preview URL
   - Refresh button (↻) - reloads iframe
   - Port indicator/display (e.g., "localhost:3000")
   - Toolbar hidden by default, shown only when `activeTab === 'preview'`

2. **Update `openIframePane()` to show/hide toolbar**
   - If `tab === 'preview'` → show toolbar
   - Else → hide toolbar

3. **Create preview loading/error page**
   - This is an HTML page served by swe-swe-server
   - Route: `/preview-placeholder` or similar
   - Content:
     - "📡 Waiting for your app..."
     - "Preview is trying to connect to: http://localhost:3000"
     - Helpful prompts section with copy-paste examples
     - Retry button
     - Port selector/input

4. **Design copy-paste friendly prompt examples**
   ```
   Tell your agent to start a dev server:

   ┌─────────────────────────────────────────────┐
   │ Run `npx elm-live src/Main.elm --port=3000` │ 📋
   │ for a hello world Elm app                   │
   └─────────────────────────────────────────────┘

   Or for other frameworks:
   • npm run dev -- --port 3000
   • npx vite --port 3000
   • npx next dev --port 3000
   • python -m http.server 3000
   • go run main.go (listening on :3000)
   ```

5. **Add clipboard copy functionality**
   - Click on prompt box → copies text to clipboard
   - Visual feedback (checkmark, "Copied!")

6. **Implement preview connection logic**
   - Preview iframe initially loads the placeholder page
   - Placeholder page polls the target port (or use iframe onload/onerror)
   - When connection succeeds, redirect to actual preview URL
   - Or: toolbar "Retry" button manually attempts connection

7. **Add port configuration**
   - Default port: 3000
   - Save preferred port to localStorage (`swe-swe-preview-port`)
   - Port input/selector in toolbar or placeholder page
   - Update preview URL when port changes

8. **Style toolbar to match existing UI**
   - Dark theme consistent with terminal UI
   - Compact height (not too intrusive)
   - Buttons with hover states

## Verification

**Before changes:**
- No toolbar exists
- Preview iframe shows raw connection error
- No guidance for users on what to do

**After changes:**
- Click "preview" → toolbar appears with home/refresh/port
- Click "vscode" → toolbar hidden
- Preview with no server running → helpful placeholder page shown
- Click copy button → prompt copied to clipboard
- Change port → preview URL updates
- Server starts → preview shows app content
- Click refresh → iframe reloads
- Click home → iframe returns to preview URL (useful if user navigated within app)

**Regression checks:**
- `make test` passes
- `make build` succeeds (new route added to server)
- Other tabs (shell, vscode, browser) unaffected - no toolbar
- Toolbar doesn't interfere with iframe content

---

# Phase 5: Sync Settings Panel with Status Bar ✅ COMPLETE

## What Will Be Achieved

The settings panel navigation and content will be updated to match the new status bar behavior. Any previous inconsistencies between settings panel and status bar will be resolved.

## Steps

1. **Audit current settings panel structure**
   - Review existing navigation items in settings panel
   - Identify any service link references (shell, vscode, browser)
   - Check for any `--basic-ui` related UI elements that need removal

2. **Remove basic-ui specific elements from settings panel**
   - Remove any navigation items that were hidden/shown based on `.basic-ui` class
   - CSS rule `.terminal-ui.basic-ui .settings-panel__nav { display: none; }` - review and update

3. **Update settings panel navigation**
   - Ensure service links in settings panel match status bar
   - Same items: shell, vscode, preview, browser
   - Consistent naming and icons

4. **Apply same click behavior to settings panel links**
   - Reuse `handleTabClick()` logic from status bar
   - Desktop + regular click → iframe toggle
   - Cmd/ctrl+click → new tab
   - Keep settings panel and status bar in sync

5. **Add preview settings section (if appropriate)**
   - Port configuration option
   - Could duplicate toolbar port selector, or just reference "use toolbar"
   - Keep it simple - avoid duplication if toolbar handles it

6. **Visual consistency**
   - Active tab indicator should reflect in both status bar AND settings panel
   - If "vscode" is active, both locations show it as active
   - Closing iframe clears active state in both

7. **Test settings panel ↔ status bar sync**
   - Click tab in status bar → settings panel reflects state
   - Click tab in settings panel → status bar reflects state
   - Close from either location → both update

## Verification

**Before changes:**
- Settings panel may have stale/inconsistent navigation
- `.basic-ui` CSS rules may hide/show elements incorrectly
- Settings panel links may not match new iframe toggle behavior

**After changes:**
- Settings panel navigation matches status bar exactly
- Clicking link in settings panel → same behavior as status bar
- Active tab indicator synced between both locations
- No orphaned `.basic-ui` CSS rules
- Preview port setting accessible (if added)

**Regression checks:**
- `make test` passes
- Settings panel still opens/closes correctly
- All settings still functional (font size, colors, etc.)
- No visual glitches when toggling between tabs

---

# Phase 6: Cleanup and Golden Tests ✅ COMPLETE

## What Will Be Achieved

Remove all remaining `--basic-ui` related code paths, update golden tests to reflect the new always-available split-pane UI, and verify no regressions across the entire codebase.

## Steps

1. **Remove `--basic-ui` golden test variants**
   - Delete `cmd/swe-swe/testdata/golden/with-basic-ui/` directory
   - Delete `cmd/swe-swe/testdata/golden/with-basic-ui-custom-url/` directory
   - Remove test case entries from `cmd/swe-swe/main_test.go`

2. **Update Makefile**
   - Remove `_golden-variant` targets for `with-basic-ui` and `with-basic-ui-custom-url`
   - Line 123-124 in Makefile

3. **Remove `--basic-ui` from test-container script**
   - `scripts/test-container/01-init.sh` line 130 mentions `--basic-ui` in comments
   - Update or remove references

4. **Clean up task log files (optional)**
   - `tasks/2026-01-19-basic-ui-flag.md`
   - `tasks/2026-01-19-basic-ui-flag-phase1.log`
   - `tasks/2026-01-19-basic-ui-flag-phase2.log`
   - Keep for historical reference or remove - user preference

5. **Run `make build golden-update`**
   - Regenerate all golden test outputs
   - Verify changes are as expected:
     - `basicUi` field removed from `init.json` files
     - Split-pane HTML structure now in all variants
     - `app-preview.md` now generated in all variants
     - No `{{IF BASIC_UI}}` conditionals in output

6. **Review golden diffs**
   ```bash
   git add -A cmd/swe-swe/testdata/golden
   git diff --cached -- cmd/swe-swe/testdata/golden
   ```
   - Verify only expected changes
   - No unintended side effects

7. **Run full test suite**
   - `make test` - all tests pass
   - `make build` - builds successfully

8. **Final manual verification with test container**
   - Boot test container (docs/dev/test-container-workflow.md)
   - MCP browser: verify terminal loads at 100% width
   - MCP browser: click service tabs, verify iframe toggle works
   - MCP browser: verify preview toolbar appears only for preview
   - MCP browser: verify cmd+click opens new tab (if testable)
   - MCP browser: verify settings panel synced
   - Shutdown test container

9. **Search for any remaining references**
   ```bash
   grep -r "basic-ui\|basicUi\|BASIC_UI\|BasicUi" --include="*.go" --include="*.js" --include="*.yml" --include="*.md"
   ```
   - Should return nothing (except historical task logs if kept)
   - Clean up any stragglers

## Verification

**Before changes:**
- Golden tests include `with-basic-ui` variants
- `--basic-ui` flag still referenced in various places
- Some golden outputs have `basicUi: true` in init.json

**After changes:**
- No `with-basic-ui` golden test directories
- No `--basic-ui` references in code (except historical task logs if kept)
- All golden outputs have split-pane structure
- All golden outputs include `app-preview.md`
- `make test` passes
- `make build golden-update` produces clean output

**Regression checks:**
- Full `make test` passes
- All existing functionality preserved
- New split-pane toggle works correctly
- No JavaScript console errors
- No build warnings
