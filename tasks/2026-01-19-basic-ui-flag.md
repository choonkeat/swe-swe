# Task: Add --basic-ui flag to swe-swe init

## Summary

Add a `--basic-ui` flag that creates a split-pane UI with xterm on the left and an iframe on the right for viewing the user's webapp.

## Design

### Layout
- Horizontal split: xterm (left) | resizer | iframe (right)
- Default 50/50 split, resizable
- Min width 150px per pane (never fully hidden)
- Double-click resizer resets to 50/50
- Mobile/narrow (<768px): fullwidth xterm only, iframe hidden

### iframe Panel
- Top "location bar" showing current URL + refresh button
- Main area: iframe content
- Loading/error placeholder with helpful message

### Flag Syntax
- `swe-swe init --basic-ui` → uses default URL (https://elm-lang.org)
- `swe-swe init --basic-ui --basic-ui-url http://localhost:3000` → uses specified URL

### Hidden Elements (when basic-ui enabled)
- Status bar: hide "shell | vscode | browser" links
- Settings dialog: hide navigation buttons

### Dynamic URL via OSC
- Agent sends `\x1b]7337;BasicUiUrl=<url>\x07` to change iframe URL
- JS validates URL with `new URL()` before accepting

### Agent Documentation
- Generate `.swe-swe/docs/app-preview.md` when `--basic-ui` is used
- Documents how to update the preview URL

## Phases

- [x] [Phase 1](#phase-1-baseline-commit-1): Add flag parsing + golden tests (no functional UI change)
- [ ] [Phase 2](#phase-2-implementation-commit-2): Implement the split-pane UI with all features

---

## Phase 1: Baseline (Commit 1)

Add the `--basic-ui` flag infrastructure without functional UI changes.

### Steps

- [x] 1.1. Add `BasicUiUrl` field to `InitConfig` struct (`cmd/swe-swe/main.go`)
- [x] 1.2. Add `--basic-ui` and `--basic-ui-url` flags to `handleInit()`
- [x] 1.3. Add `{{BASIC_UI_URL}}` template variable processing in `processTerminalUITemplate()`
- [x] 1.4. Add template placeholder comment in `terminal-ui.js`
- [x] 1.5. Wire `BasicUiUrl` into `InitConfig` saving/loading
- [x] 1.6. Add golden test variants in `main_test.go` and `Makefile`
- [x] 1.7. Make test container scripts flexible with `SWE_SWE_INIT_FLAGS` env var
- [x] 1.8. Run `make build golden-update` and verify golden diff
- [x] 1.9. Run `make test` to verify all tests pass

### Verification

1. Golden diff shows:
   - `with-basic-ui/init.json` has `basicUiUrl: "https://elm-lang.org"`
   - `with-basic-ui-custom-url/init.json` has `basicUiUrl: "http://localhost:3000"`
   - `terminal-ui.js` has `// BASIC_UI_URL: <url>` comment
2. All existing golden tests unchanged
3. `make test` passes
4. Test container accepts `SWE_SWE_INIT_FLAGS` env var

---

## Phase 2: Implementation (Commit 2)

Implement the full split-pane UI.

### Steps

#### CSS Layout
- [ ] 2.1. Add CSS variables for basic-ui mode
- [ ] 2.2. Add split-pane container CSS (`.terminal-ui__split-pane`, etc.)
- [ ] 2.3. Add location bar CSS
- [ ] 2.4. Add iframe and placeholder CSS
- [ ] 2.5. Add responsive CSS for mobile (<768px)
- [ ] 2.6. Add CSS to hide service links when in basic-ui mode

#### HTML Structure
- [ ] 2.7. Add split-pane HTML structure (conditional on basicUiUrl)
- [ ] 2.8. Add `basic-ui` class to root element when enabled

#### JavaScript: Resizer
- [ ] 2.9. Add instance variables (`basicUiUrl`, `iframePaneWidth`)
- [ ] 2.10. Add `setupResizer()` method with drag logic
- [ ] 2.11. Add localStorage persistence for pane width
- [ ] 2.12. Add double-click to reset to 50/50

#### JavaScript: iframe Management
- [ ] 2.13. Add `initBasicUi()` method
- [ ] 2.14. Add `setIframeUrl(url)` method
- [ ] 2.15. Add `refreshIframe()` method
- [ ] 2.16. Add iframe load/error event handlers

#### JavaScript: OSC Handler
- [ ] 2.17. Add OSC 7337 parser for `BasicUiUrl=<url>`
- [ ] 2.18. Validate URL with `new URL()` before accepting

#### JavaScript: Hide Navigation
- [ ] 2.19. Modify `renderServiceLinks()` to skip when basicUiUrl set

#### Agent Documentation
- [ ] 2.20. Create template `templates/container/.swe-swe/docs/app-preview.md`
- [ ] 2.21. Conditionally copy file in `handleInit()` when basicUiUrl set

#### Golden Tests & Verification
- [ ] 2.22. Run `make build golden-update` and verify diff
- [ ] 2.23. Run `make test`

### Browser Testing Checklist

Using test container with `SWE_SWE_INIT_FLAGS="--basic-ui"`:

- [ ] Split pane layout visible (terminal left, iframe right)
- [ ] elm-lang.org loads in iframe by default
- [ ] Resizer drag works (respects 150px minimum)
- [ ] Double-click resizer resets to 50/50
- [ ] Pane width persists after page reload
- [ ] Location bar shows current URL
- [ ] Refresh button reloads iframe
- [ ] Mobile simulation hides iframe (<768px)
- [ ] Status bar has NO shell|vscode|browser links
- [ ] Settings panel has NO navigation buttons
- [ ] OSC sequence changes iframe URL: `printf '\e]7337;BasicUiUrl=https://example.com\a'`
- [ ] Invalid URL in OSC is rejected (check console)
- [ ] `.swe-swe/docs/app-preview.md` exists in container

### Regression Testing

- [ ] Without `--basic-ui`: normal full-width terminal
- [ ] Without `--basic-ui`: service links present in status bar
- [ ] Without `--basic-ui`: navigation buttons in settings
- [ ] `--previous-init-flags=reuse` restores basic-ui URL
