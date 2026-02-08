# Light / Dark / System Theme Mode

## Goal

Add three-way theme mode toggle (Light / Dark / System) to the swe-swe web UI.

- **Dark mode** = current styles, pixel-for-pixel unchanged
- **Light mode** = new light palette for structural colors
- **System mode** = follows OS `prefers-color-scheme` (new default)
- Accent color picker continues working independently of mode
- Shell/agent terminals don't need to react to mid-session toggles (accepted tradeoff)

## Design Decisions

1. Toggle lives next to the color picker in settings panel (both pages)
2. New default is "System"
3. `theme-mode.js` is the shared JS module for mode management
4. Light mode overrides structural CSS variables; dark mode uses existing `:root` values untouched
5. xterm.js terminal canvas updates its theme object on mode change
6. Shared CSS extraction happens last (Phase 5) to avoid mixing refactor with feature work

## Key Files

| File | Role |
|------|------|
| `static/styles/terminal-ui.css` | Session page CSS, `:root` structural vars (lines 3-26) |
| `static/color-utils.js` | Accent color palette, `applyTheme()`, contrast calculation |
| `static/terminal-ui.js` | Web component, xterm init (line 428-437), settings panel, color picker |
| `static/selection.html` | Homepage, inline `<style>` with ~68 hardcoded dark hex values |
| `static/homepage-theme.js` | Homepage theme init, settings dialog |
| `static/session-theme.js` | Session page theme init |
| `static/index.html` | Session page HTML shell |
| `cmd/swe-swe/init.go` | Embedded file registration (hostFiles list) |

All paths relative to `cmd/swe-swe/templates/host/swe-swe-server/`.

## Dark Mode Baseline Values (for regression checking)

Capture these via `browser_evaluate` on `http://swe-swe:3000` and session page before/after changes.
Values must match exactly in dark mode after all phases complete.

### Homepage (`selection.html`)

| Element | Property | Expected Value |
|---------|----------|----------------|
| `document.body` | `backgroundColor` | `rgb(15, 23, 41)` — `#0f1729` |
| `document.body` | `color` | `rgb(248, 250, 252)` — `#f8fafc` |
| `.header` | `borderBottomColor` | `rgb(51, 65, 85)` — `#334155` |
| `.session-card` | `borderColor` | `rgb(30, 41, 59)` — `#1e293b` |
| `.section-header__title` | `color` | `rgb(248, 250, 252)` — `#f8fafc` |
| `.dialog` | `backgroundColor` | `rgb(30, 41, 59)` — `#1e293b` |
| `.dialog__input` | `backgroundColor` | `rgb(15, 23, 42)` — `#0f172a` |
| `.footer-link` | `color` | `rgb(148, 163, 184)` — `#94a3b8` |

### Session Page (`index.html` + `terminal-ui.css`)

| Element | Property | Expected Value |
|---------|----------|----------------|
| `document.body` | `backgroundColor` | `rgb(30, 30, 30)` — `#1e1e1e` |
| `.terminal-ui` | `background` | `rgb(15, 23, 42)` — `#0f172a` via `var(--bg-primary)` |
| `.settings-panel__content` | `background` | `rgb(30, 41, 59)` — `#1e293b` via `var(--bg-secondary)` |
| xterm.js theme | `background` | `#1e1e1e` |
| xterm.js theme | `foreground` | `#d4d4d4` |

---

## Phase 0: Baseline golden cleanup [DONE]

Committed stale golden files from commits `c88c697` and `e8c59e8`.
`make test` passes, tree is clean.

---

## Phase 1: Theme infrastructure

### What
Create `theme-mode.js` — the shared ES6 module that manages light/dark/system switching, localStorage persistence, and OS media query listening. Register it in `init.go`.

### Steps

1. **Create `static/theme-mode.js`**:
   - `THEME_MODES = { LIGHT: 'light', DARK: 'dark', SYSTEM: 'system' }`
   - `THEME_STORAGE_KEY = 'swe-swe-theme-mode'`
   - `getStoredMode()` — reads localStorage, defaults to `'system'`
   - `getResolvedMode(mode)` — resolves `'system'` → `'light'`/`'dark'` via `matchMedia('(prefers-color-scheme: dark)')`
   - `applyMode(mode)` — sets `data-theme="light"` or `data-theme="dark"` on `<html>`, sets structural CSS vars for light mode (or removes overrides for dark), dispatches `CustomEvent('theme-mode-changed', { detail: { mode, resolved } })`
   - `setThemeMode(mode)` — saves to localStorage + calls `applyMode`
   - `initThemeMode()` — reads stored pref, applies, sets up `matchMedia` change listener for system mode auto-switching
   - Light structural palette:
     ```
     --bg-primary: #ffffff
     --bg-secondary: #f1f5f9
     --bg-tertiary: #f8fafc
     --bg-elevated: #e2e8f0
     --border-primary: #cbd5e1
     --border-secondary: #94a3b8
     --text-primary: #0f172a
     --text-secondary: #475569
     --text-muted: #94a3b8
     ```
   - Light xterm theme: `{ background: '#ffffff', foreground: '#1e293b' }`
   - Export: `THEME_MODES`, `getStoredMode`, `getResolvedMode`, `initThemeMode`, `setThemeMode`, `LIGHT_XTERM_THEME`, `DARK_XTERM_THEME`

2. **Register in `init.go`**: Add `theme-mode.js` to the `hostFiles` slice.

### Verify
- `make build golden-update` — golden diff shows new `theme-mode.js` in every variant
- `make test` passes

### Regression risk
None. New file only, not imported by any page yet.

---

## Phase 2: Session page

### What
Wire theme mode into the session page. Add mode toggle to settings panel. Update xterm.js to react. Make `index.html` theme-aware.

### Steps

1. **`index.html`**:
   - Add `<script type="module" src="/theme-mode.js">` before `session-theme.js`
   - Change `background: #1e1e1e` to `background: var(--bg-terminal, #1e1e1e)` (introduce `--bg-terminal` so the body tracks mode)

2. **`session-theme.js`**:
   - Import and call `initThemeMode()` on load
   - Expose on `window.sweSweTheme`: `{ ..., initThemeMode, setThemeMode, getStoredMode, THEME_MODES }`

3. **`terminal-ui.js` — settings panel HTML** (~line 215):
   - Add "Appearance" field above "Theme Color" with three-button segmented toggle:
     ```html
     <div class="settings-panel__field">
         <label class="settings-panel__label">Appearance</label>
         <div class="settings-panel__theme-toggle" id="settings-theme-toggle">
             <button class="settings-panel__theme-btn" data-mode="light">Light</button>
             <button class="settings-panel__theme-btn" data-mode="dark">Dark</button>
             <button class="settings-panel__theme-btn selected" data-mode="system">System</button>
         </div>
     </div>
     ```

4. **`terminal-ui.js` — JS logic**:
   - Add `populateThemeToggle()` — reads current mode, marks correct button `.selected`
   - Add `setupThemeToggle()` — click handler calls `window.sweSweTheme.setThemeMode(mode)`, updates selection
   - On `theme-mode-changed` event: update xterm theme via `this.term.options.theme = resolved === 'light' ? LIGHT_THEME : DARK_THEME`
   - Call both methods from `connectedCallback` alongside existing color picker setup

5. **`terminal-ui.css`**:
   - Add styles for `.settings-panel__theme-toggle` and `.settings-panel__theme-btn` (segmented button group)
   - Add `--bg-terminal` to `:root` (dark: `#1e1e1e`) — light mode override in `theme-mode.js` sets it to `#ffffff`

### Verify
- `make build golden-update` — diff shows: index.html (script + background), session-theme.js (init), terminal-ui.js (toggle HTML + JS), terminal-ui.css (toggle styles + `--bg-terminal`)
- `make test` passes
- Dev server + MCP playwright on `http://swe-swe:3000/session/test123?assistant=claude&preview`:
  - Dark mode: all baseline values match table above
  - Light mode: backgrounds white, text dark, xterm area light
  - System mode: resolves correctly
  - Accent color picker works in both modes

### Regression risk
Low. Dark mode = existing `:root` values unchanged. `--bg-terminal` defaults to `#1e1e1e`.

---

## Phase 3: Homepage

### What
Wire theme mode into homepage. Convert ~68 hardcoded dark hex values in `selection.html` to CSS variables.

### Steps

1. **`selection.html` — `<style>` block**:
   - Add structural CSS variable definitions to `:root` (same values as `terminal-ui.css`):
     ```css
     --bg-primary: #0f172a;
     --bg-secondary: #1e293b;
     /* ... etc ... */
     ```
   - Replace hardcoded hex with `var(--...)`. Mapping:
     - `#0f1729` / `#0f172a` → `var(--bg-primary)` (body bg)
     - `#1e293b` → `var(--bg-secondary)` (header, cards, dialogs)
     - `rgba(30, 41, 59, ...)` → use `var(--bg-secondary)` with opacity wrapper or dedicated var
     - `#334155` → `var(--border-primary)`
     - `#475569` → `var(--border-secondary)`
     - `#f8fafc` → `var(--text-bright)` (brightest text — titles, inputs)
     - `#e2e8f0` → `var(--text-primary)`
     - `#94a3b8` → `var(--text-secondary)`
     - `#64748b` → `var(--text-muted)`
   - Semantic colors stay hardcoded: `#22c55e` (green), `#ef4444` (red), `#f59e0b` (amber), `#ec4899` (pink), agent badge colors

2. **`selection.html` — scripts**:
   - Add `<script type="module" src="/theme-mode.js">` before `homepage-theme.js`

3. **`homepage-theme.js`**:
   - Import and call `initThemeMode()`
   - Expose on `window.sweSweTheme`: add theme mode functions

4. **Homepage settings dialog** — add theme toggle:
   - Same segmented toggle HTML/JS as session page, inside the settings dialog above color picker
   - Wire to `setThemeMode()`

### Verify
- `make build golden-update` — diff shows selection.html (vars + toggle + script), homepage-theme.js (init)
- `make test` passes
- Dev server + MCP playwright on `http://swe-swe:3000`:
  - **Dark mode regression check**: all baseline values from table match exactly
  - Light mode: white backgrounds, dark text, accent colors pop, cards readable
  - Mode persists across homepage ↔ session navigation (same localStorage key)

### Regression risk
Medium — largest change. The hex-to-variable conversion must be exact. Verify by confirming every baseline value matches in dark mode.

---

## Phase 4: Visual verification

### What
Full end-to-end visual check of all three modes on both pages using MCP playwright.

### Steps

1. `make run` to start dev server
2. **Homepage** (`http://swe-swe:3000`):
   - Verify dark mode baseline values via `browser_evaluate` (compare against table above)
   - Screenshot dark mode
   - Switch to Light — screenshot, check readability
   - Switch to System — verify follows browser pref
3. **Session page** (`http://swe-swe:3000/session/test123?assistant=claude&preview`):
   - Same three-mode checks
   - Verify xterm area changes
   - Verify accent color picker works in light mode (pick color, check contrast text)
4. **Persistence**: reload pages, confirm mode + accent color remembered
5. `make stop`

### Verify
- All dark mode baseline values match table exactly
- Light mode screenshots look polished
- No console errors

---

## Phase 5: Extract shared theme CSS

### What
Deduplicate structural CSS variables into `styles/theme.css`.

### Steps

1. Create `styles/theme.css`:
   - `:root` block with dark structural vars
   - `[data-theme="light"]` block with light overrides
2. `terminal-ui.css` — remove structural vars from `:root` (keep accent vars)
3. `selection.html` — add `<link href="/styles/theme.css">`, remove inline structural vars
4. `index.html` — add `<link href="/styles/theme.css">`
5. `theme-mode.js` — remove JS-based CSS variable setting for light mode (now handled by CSS `[data-theme="light"]` selector)
6. Register `styles/theme.css` in `init.go`

### Verify
- `make build golden-update`
- `make test` passes
- Dev server smoke test: dark identical, light identical
- Dark mode baseline values still match table

### Regression risk
Low — pure mechanical refactor, no value changes.
