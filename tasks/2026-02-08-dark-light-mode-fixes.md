# Dark/Light Mode Fixes

Fix 6 dark/light mode issues in the swe-swe web UI to ensure consistent, flicker-free theme support across all pages and improved visual polish.

## Phase 1: Cookie-based theme persistence (eliminate FOUC) ✅

**Goal:** When you set System|Light and reload, the page renders in light mode from the very first paint. No flash of dark mode.

### Steps

**1a. Set a cookie when theme changes**
- File: `cmd/swe-swe/templates/host/swe-swe-server/static/theme-mode.js`
- In `setThemeMode()` and `applyMode()`, also set cookie `swe-swe-theme=light|dark` (the *resolved* mode) with `path=/; max-age=31536000; SameSite=Lax`.
- When mode is "system", cookie stores the resolved value. When OS preference changes (the `DARK_MEDIA` listener), update the cookie too.

**1b. Add inline `<script>` to HTML pages that reads cookie before rendering**
- Files: `cmd/swe-swe/templates/host/swe-swe-server/static/index.html` (session page), `cmd/swe-swe/templates/host/swe-swe-server/static/selection.html` (homepage)
- Add a tiny inline `<script>` in `<head>` *before* any CSS link that reads the `swe-swe-theme` cookie and sets `document.documentElement.setAttribute('data-theme', value)`.
- Runs synchronously before browser paints. ~5 lines, no external dependency.

**1c. No server-side Go changes needed**
- Cookie approach is entirely client-side.

### Verification
- Set theme to Light, reload — no dark flash.
- Set theme to Dark, reload — no light flash.
- Set theme to System, toggle OS dark mode, reload — correct mode.
- Existing theme toggle still works on both pages.

---

## Phase 2: Preview URL bar contrast ✅

**Goal:** The URL bar in Agent View has good contrast in both dark and light modes.

### Steps

**2a. Read theme cookie in Agent View page**
- File: `cmd/swe-swe/templates/host/chrome-screencast/static/index.html`
- Add same inline `<script>` in `<head>` that reads `swe-swe-theme` cookie and sets `data-theme`.

**2b. Replace hardcoded URL bar colors with theme-aware CSS**
- Current hardcoded dark: `#2d2d2d` bg, `#1e1e1e` input bg, `#404040` border, `#ccc` text.
- Add CSS variables on `:root` (dark defaults) and `[data-theme="light"]` overrides:
  - Light: `#f1f5f9` bg, `#ffffff` input bg, `#cbd5e1` border, `#1e293b` text.
- Update all `#urlbar` selectors to use these variables.

### Verification
- Navigate to `/chrome/`, screenshot in dark — same as before.
- Set cookie to light, reload, screenshot — light bg, dark text, good contrast.
- Hover URL bar in both modes — opacity transition works, text readable.

---

## Phase 3: "Listening for app..." page theming

**Goal:** Preview waiting page respects dark/light mode.

### Steps

**3a. Add cookie-reading inline script**
- File: `cmd/swe-swe/templates/host/swe-swe-server/main.go` (the `previewProxyErrorPage` const)
- Same inline `<script>` in `<head>`.

**3b. Replace hardcoded colors with theme-aware CSS**
- Current dark: `background: #1e1e1e`, `color: #9ca3af`, heading `#e5e7eb`, instruction bg `#262626`, instruction text `#d1d5db`, port `#60a5fa`, status `#6b7280`.
- Add `:root` dark defaults, then `[data-theme="light"]` overrides:
  - Light: `background: #ffffff`, `color: #64748b`, heading `#1e293b`, instruction bg `#f1f5f9`, instruction text `#334155`, port `#2563eb`, status `#94a3b8`.
- Note: Go const uses `%%` for CSS `%` — be careful with formatting.

### Verification
- Navigate to preview URL (shows waiting page since no app running).
- Screenshot dark — same as before.
- Set cookie to light, reload, screenshot — light bg, dark text, good contrast.

---

## Phase 4: "Connected, waiting for frames..." page theming

**Goal:** Agent View overlay (spinner + status) respects dark/light mode.

### Steps

**4a. Cookie script already added in Phase 2**
- `data-theme` attribute is already set on `<html>` in `chrome-screencast/static/index.html`.

**4b. Replace hardcoded overlay/body colors with theme-aware CSS**
- File: `cmd/swe-swe/templates/host/chrome-screencast/static/index.html`
- Current: body bg `#1e1e1e`, overlay bg `rgba(30, 30, 30, 0.9)`, text `#ccc`, spinner border `#333`, spinner top `#007acc`.
- Add `:root` variables (dark defaults), then `[data-theme="light"]` overrides:
  - Light: body bg `#f8f9fb`, overlay bg `rgba(255, 255, 255, 0.9)`, text `#334155`, spinner border `#e2e8f0`, spinner top `#2563eb`.

### Verification
- Navigate to `/chrome/`, screenshot — dark overlay same as before.
- Set cookie to light, reload, screenshot — light overlay, dark text, blue spinner.
- When frames arrive, overlay hides — no regression.

---

## Phase 5: CLAUDE badge contrast in light mode

**Goal:** "CLAUDE" agent label is readable in light mode.

### Steps

**5a. Change badge color to theme variable**
- File: `cmd/swe-swe/templates/host/swe-swe-server/static/styles/terminal-ui.css` line 1522
- Change `color: #e0e0e0` to `color: var(--text-secondary)`.
- `--text-secondary` is `#94a3b8` dark / `#475569` light — good contrast in both.
- Single-line CSS change.

### Verification
- Navigate to session page with `?preview`, screenshot dark — badge visible.
- Toggle light, screenshot — badge text is dark gray, clearly readable.
- Check with `&yolo` — badge-toggle still works in both modes.

---

## Phase 6: Remove settings nav buttons

**Goal:** Remove Home/Shell/VSCode/App Preview/Agent View navigation from session settings panel.

### Steps

**6a. Remove nav HTML**
- File: `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js`
- Delete the `<nav class="settings-panel__nav">...</nav>` block (lines 238-258).

**6b. Remove nav CSS**
- File: `cmd/swe-swe/templates/host/swe-swe-server/static/styles/terminal-ui.css`
- Delete `.settings-panel__nav`, `.settings-panel__nav-btn`, `.settings-panel__nav-btn:hover`, `.settings-panel__nav-btn:active`, `.settings-panel__nav-icon` rules.
- Delete light mode override `[data-theme="light"] .settings-panel__nav-btn:hover`.

**6c. Remove dead JS**
- Search for `settings-panel__nav` references in JS. Remove any logic populating shell/preview URLs for nav links.

### Verification
- Navigate to session page with `?preview`, open settings panel, screenshot.
- Only Username, Session Name, Appearance, Theme Color visible. No nav buttons.
- Settings panel still opens/closes, theme toggle works, color picker works.
