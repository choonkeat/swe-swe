# Task: Auth Name Field + Remove Status Bar Color

## Overview

Two related UI cleanup tasks:
1. Make auth login name field editable (uses localStorage)
2. Remove unused status bar color setting and old status bar code

## Part 1: Auth Login Name Field

### Current State
- `cmd/swe-swe/templates/host/auth/main.go` line ~195 has:
  ```html
  <input type="text" name="username" value="admin" autocomplete="username" readonly>
  ```
- Field is readonly with hardcoded "admin" value

### Target State
- Editable text field with placeholder "Your name"
- On page load: prefill from `localStorage.getItem('swe-swe-username')` if exists
- On form submit: if name empty, `localStorage.removeItem('swe-swe-username')`; otherwise save to localStorage
- After redirect, `terminal-ui.js` handles username (already reads from localStorage, auto-generates if missing)

### Files to Modify
- `cmd/swe-swe/templates/host/auth/main.go`
  - Change input from readonly to editable
  - Add `<script>` block for localStorage read/write

## Part 2: Remove Status Bar Color

### What Gets Removed

| File | Changes |
|------|---------|
| `terminal-ui.css` | Remove `--status-bar-color`, `--status-bar-text-color`, `--status-bar-border-*` variables; Remove `.terminal-ui__status-bar` and related selectors |
| `terminal-ui.js` | Remove status bar HTML; Remove color picker in settings panel; Remove `setStatusBarColor()`, `restoreStatusBarColor()`, color functions |
| `init.go` | Remove `--status-bar-color` flag and related code |
| `templates.go` | Remove `{{STATUS_BAR_COLOR}}` replacement |
| `InitConfig` struct | Remove `StatusBarColor` field |

### What Changes (not removed)
- `.settings-panel__header` background: change from `var(--status-bar-color)` to `var(--accent-primary)`
- `.settings-panel__input:focus` border: change to `var(--accent-primary)`
- Any other `--status-bar-color` references: replace with `--accent-primary`

### Files to Modify
1. `cmd/swe-swe/templates/host/swe-swe-server/static/styles/terminal-ui.css`
2. `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js`
3. `cmd/swe-swe/init.go`
4. `cmd/swe-swe/templates.go`

## Testing Plan

Using `docs/dev/swe-swe-server-workflow.md`:

```bash
# Start dev server
cd /workspace
make run > /tmp/server.log 2>&1 &

# Test auth page (need to check if auth is part of dev server or separate)
# Test session settings panel - verify color picker removed
# Test that accent-primary color is applied to settings header

# View in MCP browser
http://swe-swe:3000/session/test123?assistant=claude&preview

make stop
```

## Implementation Order

1. [ ] Part 2 first (remove status bar color) - larger change, isolated
2. [ ] Part 1 second (auth name field) - smaller change
3. [ ] Run `make build golden-update` after both
4. [ ] Verify golden file diffs
5. [ ] Test with dev server

## Risks

- Golden files will change significantly (init.json loses statusBarColor field)
- Need to verify no runtime errors from missing CSS variables
- Auth page is separate service - may need to test differently
