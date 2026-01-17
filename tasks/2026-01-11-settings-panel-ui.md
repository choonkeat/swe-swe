# Settings Panel UI

**Branch:** `settings-ui`
**Goal:** Add a settings panel UI to the status bar that provides runtime customization (username, session name, primary color), consolidated navigation (homepage, VSCode, browser), and mobile-responsive design.

## Design Decisions

- **CSS-driven theming:** Use CSS custom properties with `oklch` auto-contrast and `color-mix()` derived shades — no JS color calculation needed
- **Mobile-first responsive:** Full-width bottom sheet on mobile, centered modal on desktop
- **Reuse existing code:** Extract helpers from existing username/session/link logic rather than rewriting
- **Terminal color links:** xterm.js linkifier makes CSS colors clickable to set status bar color
- **localStorage persistence:** Settings persist per-browser, template defaults used as fallback

---

## Phase 1: CSS Variable Foundation [DONE]

### What will be achieved
- Replace static template placeholders (`{{STATUS_BAR_COLOR}}`) with CSS custom properties
- Implement auto-contrast text color using `oklch` relative color syntax (no JS needed)
- Derive border shades using `color-mix()` for top/bottom borders
- Keep existing CLI flags working — they set the initial CSS variable values

### Small steps

1. **Modify `terminal-ui.js` CSS section:**
   - Add `:root` block with `--status-bar-color: {{STATUS_BAR_COLOR}};`
   - Change `.terminal-ui__status-bar` to use `var(--status-bar-color)` for background
   - Add `oklch` formula for auto-contrast text color
   - Add `color-mix()` for border shades

2. **Simplify `main.go` template processing:**
   - Remove `{{STATUS_BAR_TEXT_COLOR}}` replacement (CSS handles it now)
   - Keep `{{STATUS_BAR_COLOR}}` replacement (sets the CSS variable default)

3. **Update `color.go`:**
   - Keep `ContrastingTextColor()` for ANSI preview in `--status-bar-color=list`
   - No longer needed for template processing

### Verification

1. Boot test container per `docs/dev/test-container-workflow.md`
2. Use MCP browser to verify:
   - Test with light color (`--status-bar-color=yellow`) → expect dark text
   - Test with dark color (`--status-bar-color=navy`) → expect light text
   - Verify all connection states still look correct (`.connected`, `.error`, `.reconnecting`)
3. Shutdown test container

### Regression check
- Run `make test`
- Run `make build golden-update` and verify only expected changes
- Commit golden files

---

## Phase 2: Settings Panel Markup & Styling [DONE]

### What will be achieved
- HTML structure for the settings panel (hidden by default)
- Responsive CSS: centered modal on desktop, full-width bottom sheet on mobile
- Proper accessibility (focus trap, ARIA attributes)
- Visual design consistent with existing terminal UI

### Small steps

1. **Add panel HTML to `terminal-ui.js`:**
   ```html
   <div class="settings-panel" hidden>
     <div class="settings-panel__backdrop"></div>
     <div class="settings-panel__content">
       <header>Session Settings <button class="settings-panel__close">×</button></header>
       <section class="settings-panel__fields">
         <!-- username, session, color inputs -->
       </section>
       <nav class="settings-panel__nav">
         <!-- homepage, vscode, browser buttons -->
       </nav>
     </div>
   </div>
   ```

2. **Add responsive CSS:**
   - Desktop (min-width: 640px): centered modal, max-width ~400px, rounded corners, backdrop blur
   - Mobile: full-width, docked to bottom, no rounded bottom corners, slides up
   - Backdrop: semi-transparent overlay, closes panel on click

3. **Add CSS for form elements:**
   - Text inputs for username/session name
   - Color input with swatches + text input for CSS colors
   - Navigation buttons with icons (large tap targets, min 44px)

4. **Use CSS variables for theming:**
   - Panel uses `var(--status-bar-color)` for header/accents
   - Derived shades for borders/shadows via `color-mix()`

### Verification (test container + MCP browser)

1. Boot test container
2. Use MCP browser:
   - Verify panel exists in DOM but is hidden initially
   - Resize browser window, verify smooth transition between mobile/desktop layouts
   - Check panel doesn't overflow on small screens (320px width)
   - Verify color theming works (panel matches status bar color)
3. Shutdown test container

### Regression check
- Existing status bar functionality unchanged
- `make test` passes
- `make build golden-update`, commit golden files

---

## Phase 3: Panel Toggle & Interaction [DONE]

### What will be achieved
- Clicking status bar opens the settings panel
- Multiple close mechanisms: click backdrop, press Escape, click × button
- Smooth open/close animations
- Focus management (trap focus in panel when open, restore on close)

### Small steps

1. **Add click handler to status bar:**
   - `statusBar.addEventListener('click', openSettingsPanel)`
   - Prevent opening when clicking existing interactive elements (if any remain)

2. **Implement `openSettingsPanel()`:**
   - Remove `hidden` attribute from panel
   - Add `open` class for CSS animations
   - Set `aria-expanded="true"` on status bar
   - Focus first interactive element in panel

3. **Implement `closeSettingsPanel()`:**
   - Add `hidden` attribute back
   - Remove `open` class
   - Set `aria-expanded="false"`
   - Restore focus to status bar

4. **Add close triggers:**
   - × button click → `closeSettingsPanel()`
   - Backdrop click → `closeSettingsPanel()`
   - Escape key (when panel open) → `closeSettingsPanel()`

5. **Add CSS transitions:**
   - Desktop: fade in + scale from 95% to 100%
   - Mobile: slide up from bottom

### Verification (test container + MCP browser)

1. Boot test container
2. Use MCP browser:
   - `browser_snapshot` to get initial state (panel not visible)
   - `browser_click` on status bar → panel opens
   - `browser_snapshot` to verify panel is visible
   - `browser_press_key` Escape → panel closes
   - `browser_click` status bar, then click backdrop → panel closes
   - `browser_resize` to 375×667 (mobile), repeat tests
3. Shutdown test container

### Regression check
- Status bar still shows connection state correctly
- `make test` passes
- `make build golden-update`, commit golden files

---

## Phase 4: Settings Functionality [DONE]

### What will be achieved
- Username input syncs with existing username logic
- Session name input syncs with existing session name logic
- Color picker updates `--status-bar-color` CSS variable in real-time
- All settings persist to localStorage
- On page load, restore settings from localStorage (or fall back to template defaults)

### Small steps

1. **Extract existing helpers:**
   - Find current username/session update code (WebSocket message handlers)
   - Extract into reusable functions: `setUsername(name)`, `setSessionName(name)`
   - Ensure old code paths call these same helpers (no duplication)

2. **Wire username input:**
   - On input change, call extracted `setUsername()` helper
   - Save to `localStorage.setItem('settings:username', value)`
   - On page load, restore from localStorage and populate input

3. **Wire session name input:**
   - On input change, call extracted `setSessionName()` helper
   - Save to `localStorage.setItem('settings:sessionName', value)`
   - On page load, restore from localStorage and populate input

4. **Implement color picker:**
   - Add preset color swatches (6-8 common colors)
   - Add text input for any CSS color (hex, rgb, rgba, hsl, hsla, oklch, named)
   - Add native `<input type="color">` for visual picker
   - On color change:
     - Update `document.documentElement.style.setProperty('--status-bar-color', color)`
     - Save to `localStorage.setItem('settings:statusBarColor', color)`
   - On page load, restore from localStorage (CSS variable default from template used if no localStorage)

5. **Add visual feedback:**
   - Show current username/session in inputs on panel open
   - Highlight active color swatch

### Verification (test container + MCP browser)

1. Boot test container
2. Test username: type new value, verify status bar updates, refresh, verify persists
3. Test session name: same pattern
4. Test color: click swatch, verify change; enter custom CSS color, verify; refresh, verify persists
5. Test fallback: clear localStorage, refresh, verify template defaults apply
6. Shutdown test container

### Regression check
- Existing username/session display still works
- WebSocket sync still works
- `make test` passes
- `make build golden-update`, commit golden files

---

## Phase 4b: Terminal Color Links [DONE]

### What will be achieved
- xterm.js recognizes CSS colors as clickable links
- Clicking a color link sets the status bar color
- Tooltip on hover: "Click to set status bar color"
- Setup workflow updated to ask permission and output detected colors

### Small steps

1. **Add color link detection to xterm.js config:**
   - Register link provider for hex colors (`#[0-9a-fA-F]{3,6}`)
   - Register link provider for functional colors (`rgb(...)`, `rgba(...)`, `hsl(...)`, `hsla(...)`, `oklch(...)`)
   - Add hover callback showing "Click to set status bar color"
   - On click, call `setStatusBarColor(color)`

2. **Implement `setStatusBarColor(color)` (if not already from Phase 4):**
   - Update CSS variable
   - Save to localStorage
   - Show brief visual feedback (flash/pulse status bar)

3. **Update `swe-swe/setup` prompt:**
   - Add step: "Can I look for your project's primary color?"
   - If yes, search common files (tailwind.config.js, theme.ts, variables.css, etc.)
   - If found, output the color(s) as plain text (xterm makes them clickable)

### Verification (test container + MCP browser)

1. Boot test container
2. Have agent output a hex color in terminal (e.g., `echo "#ff5500"`)
3. Verify it's displayed as a link with tooltip
4. Click it, verify status bar color changes
5. Test with `rgb(255, 0, 0)` format as well
6. Shutdown test container

### Regression check
- Existing terminal link behavior unchanged
- `make test` passes
- `make build golden-update`, commit golden files

---

## Phase 5: Navigation Links [DONE]

### What will be achieved
- Navigation buttons in settings panel: Homepage, VSCode, Browser
- Reuse existing link generation logic (especially VSCode worktree detection)
- Large tap targets (min 44px) with icons
- Remove or deprecate old small links from status bar

### Small steps

1. **Find and extract existing link logic:**
   - Locate current homepage/VSCode/browser link generation code
   - Identify VSCode worktree detection logic (critical to preserve)
   - Extract into reusable helpers: `getHomepageUrl()`, `getVSCodeUrl()`, `getBrowserUrl()`
   - Ensure old code paths use these same helpers

2. **Add navigation buttons to panel:**
   - Three buttons in `<nav class="settings-panel__nav">`
   - Each button calls the extracted helper for its URL
   - Use `window.open(url, '_blank')` or appropriate navigation

3. **Style navigation buttons:**
   - Large tap targets (min 44×44px)
   - Icons (emoji or simple SVG)
   - Consistent with panel theming (uses CSS variables)
   - Hover/active states

4. **Clean up old links:**
   - Remove old small links from status bar
   - Ensure no dead code left behind

5. **Handle edge cases:**
   - VSCode link: preserve worktree logic exactly
   - Browser link: handle gracefully if not available
   - Homepage: should always work

### Verification (test container + MCP browser)

1. Boot test container
2. Open panel, verify all three nav buttons present
3. Test each button opens correct URL
4. Verify VSCode URL includes worktree path correctly
5. `browser_resize` to mobile, verify buttons are large enough (44px)
6. Verify old small links removed
7. Shutdown test container

### Regression check
- VSCode worktree detection unchanged
- `make test` passes
- `make build golden-update`, commit golden files

---

## Phase 6: Integration Testing

### What will be achieved
- End-to-end verification of all features working together
- Cross-viewport testing (desktop + mobile)
- Performance check (no jank on panel open/close)
- Final regression sweep

### Small steps

1. **Full flow test on desktop:**
   - Boot test container
   - Open terminal UI
   - Click status bar → panel opens
   - Change username → verify status bar updates
   - Change session name → verify updates
   - Change color via picker → verify status bar + panel theme updates
   - Change color via text input (CSS color) → verify
   - Click VSCode → verify correct URL with worktree
   - Click Homepage → verify correct URL
   - Click Browser → verify correct URL
   - Close panel via each method (×, backdrop, Escape)
   - Refresh page → verify all settings persisted

2. **Full flow test on mobile:**
   - `browser_resize` to 375×667
   - Repeat all tests from step 1
   - Verify panel is full-width bottom sheet
   - Verify tap targets are adequate

3. **Test terminal color links:**
   - Have agent output color values
   - Click to set status bar color
   - Verify it works and persists

4. **Test with different init flags:**
   - Test with `--status-bar-color=red`
   - Test with `--status-bar-color=yellow` (light color, needs dark text)
   - Verify CSS auto-contrast works correctly
   - Clear localStorage, verify flag defaults apply

5. **Connection state testing:**
   - Verify `.connected`, `.error`, `.reconnecting`, `.blurred` states still work
   - Verify color theming applies correctly in all states

6. **Performance check:**
   - Panel open/close should be smooth
   - Color changes should be instant

7. **Shutdown test container**

### Final steps
```bash
make test
make build golden-update
git add -A
git commit -m "phase 6: integration testing complete"
```

---

## Summary

| Phase | Description | Key Files |
|-------|-------------|-----------|
| 1 | CSS Variable Foundation | `terminal-ui.js`, `main.go`, `color.go` |
| 2 | Panel Markup & Styling | `terminal-ui.js` |
| 3 | Panel Toggle & Interaction | `terminal-ui.js` |
| 4 | Settings Functionality | `terminal-ui.js` (extract helpers) |
| 4b | Terminal Color Links | `terminal-ui.js`, `swe-swe/setup` |
| 5 | Navigation Links | `terminal-ui.js` (extract helpers) |
| 6 | Integration Testing | All |

Each phase ends with:
```bash
make build golden-update
git add -A cmd/swe-swe/testdata/golden
git diff --cached -- cmd/swe-swe/testdata/golden  # verify
git commit -m "phase N: description"
```
