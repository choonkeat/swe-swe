# Task: Auth Name Field + Remove Status Bar Color

## Status: COMPLETED

Implemented in commit `7b6c8919f`.

## Overview

Two related UI cleanup tasks:
1. Make auth login name field editable (uses localStorage)
2. Remove unused status bar color setting and old status bar code

## Part 1: Auth Login Name Field

### Changes Made
- `cmd/swe-swe/templates/host/auth/main.go`
  - Changed readonly `admin` field to editable text field with placeholder "Your name"
  - Added `<script>` block to:
    - On load: prefill from `localStorage.getItem('swe-swe-username')`
    - On submit: save to localStorage (or clear if empty)
  - Removed readonly CSS styles

## Part 2: Remove Status Bar Color

### Files Modified

| File | Changes |
|------|---------|
| `terminal-ui.css` | Removed `--status-bar-color`, `--status-bar-text-color`, `--status-bar-border-*` variables; Removed `.terminal-ui__status-bar` and related selectors (~120 lines removed) |
| `terminal-ui.js` | Removed color picker field from settings panel HTML; Removed `setStatusBarColor()`, `restoreStatusBarColor()`, `normalizeColorForPicker()`, `updateActiveSwatches()` functions; Removed color picker event handlers |
| `init.go` | Removed `--status-bar-color` flag; Removed `StatusBarColor` from `InitConfig` struct |
| `templates.go` | Removed `statusBarColor` parameter from `processTerminalUITemplate()` |
| `main_test.go` | Removed `with-status-bar-color` and `with-all-ui-options` test variants |

### CSS Replacements
- `.settings-panel__header` background: `var(--status-bar-color)` → `var(--accent-primary)`
- `.settings-panel__input:focus` border: `var(--status-bar-color)` → `var(--accent-primary)`
- `.settings-panel__nav-btn.active` border: `var(--status-bar-bg-color)` → `var(--accent-primary)`

## Testing Completed

1. `make build` - passed
2. `make golden-update` - updated 262 files
3. `make test` - all tests pass
4. Dev server visual test - settings panel shows purple header, no color picker

## Summary

- 262 files changed, 1,209 insertions(+), 40,359 deletions(-)
- Removed 2 test variant directories (`with-status-bar-color`, `with-all-ui-options`)
- Settings panel now uses `--accent-primary` (#7c3aed purple) for header
