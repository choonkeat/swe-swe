# UI Revamp Bugs

Bugs introduced after the terminal UI revamp that need to be fixed.

## Bug 1: Session Naming Not Discoverable

**Status**: Fixed (2026-01-26)

**Description**: The session naming feature was not discoverable after the UI revamp (not actually broken).

**Expected behavior**: User should be able to name/rename their session with clear visual affordances.

**Current behavior**: ~~Session name appears as plain text with no indication it's clickable.~~ Fixed - session name now shows:
- Edit icon (✎) on hover
- Underline on hover
- Tooltip "Click to rename session"
- Cursor changes to pointer

**Investigation findings**:
- [x] Session naming works via: (1) `?name=` URL parameter, (2) Settings panel input, (3) Click-to-rename
- [x] The issue was purely discoverability - no visual affordance that it's clickable
- [x] Fixed by adding CSS hover effects and title attribute

---

## Bug 2: Chat Feature Not Discoverable

**Status**: Fixed (2026-01-26)

**Description**: The chat feature was not discoverable after the UI revamp (not actually broken).

**Expected behavior**: Users should be able to send chat messages to collaborate with others viewing the same session.

**Current behavior**: ~~Chat trigger was hidden in legacy status bar.~~ Implementation added - chat button (💬) now appears in header when there are 2+ viewers:
- Click button to open chat input
- Badge shows unread message count
- Existing chat overlay and WebSocket functionality unchanged

**Investigation findings**:
- [x] Chat was fully implemented but trigger was hidden in legacy status bar
- [x] WebSocket chat messages work correctly (`type: 'chat'`)
- [x] Added visible chat button to header that shows when viewers > 1
- [x] Verified via `?preview` mode - chat button visible and styled correctly

---

## Bug 3: Session Keep/Trash Icons Always Visible (Should Be Hover-Only)

**Status**: Fixed (2026-01-26)

**Description**: The session keep (bookmark) and trash (delete) icons in the Session Recordings list are always visible.

**Expected behavior**: Icons should be hidden by default and only appear when hovering over the recording row.

**Current behavior**: Icons are always visible.

**Fix approach**: Add CSS to hide `.recording-card__btn-group` by default and show on `.recording-card:hover`.

---

## Bug 4: Preview Panel Has Redundant URL Bars

**Status**: Fixed (2026-01-26)

**Description**: The preview panel showed two URL-related inputs: a read-only location bar displaying the current URL, plus a separate "Enter URL to debug..." input field below it.

**Expected behavior**: A single URL bar that serves both purposes (shows current URL and allows navigation/debugging).

**Current behavior**: ~~Two separate UI elements for URLs.~~ Fixed - single URL input that:
- Shows current iframe URL
- Allows editing to navigate to a different URL
- Has placeholder "Enter URL..."

**Fix implemented**: Combined the read-only URL span and debug input into a single editable input field.

---

## Bug 5: Session Page Top Navigation Colors Inconsistent

**Status**: Fixed (2026-01-26)

**Description**: The colors of the top navigation elements on the session page don't match - some areas use the correct navy blue theme while others have different styling.

**Expected behavior**: All navigation elements should use consistent navy blue theme:
- Main header bar (with session ID, back button)
- Left panel header (Agent Terminal + badge)
- Right panel header (Preview, Code, Terminal tabs)

**Current behavior**: ~~Inconsistent colors across the navigation elements.~~ Fixed - both panel headers now use `var(--bg-tertiary)` for consistent navy blue background.

**Fix approach**: After Bug 6 is fixed (separate panel headers), ensure both panel headers use same CSS variables for consistent navy blue background.

---

## Bug 6: Second-Level Navigation Layout Wrong

**Status**: Fixed (2026-01-26)

**Description**: Each panel should have its own header row, NOT one shared full-width nav bar.

### CORRECT Layout (target - now implemented):
```
┌─────────────────────────────────────────────────────────────────────────────┐
│ ←  replit/vibe-code [main]  ● Connected • 0m          U1 +24  [YOLO]  ⚙    │  <- Header
├───────────────────────────────────┬─────────────────────────────────────────┤
│ >_ Agent Terminal    [CLAUDE SAFE]│ ⊕Preview  </>Code  >_Terminal  ⚡Agent  │  <- Panel headers (separate)
├───────────────────────────────────┼─────────────────────────────────────────┤
│                                   │                                         │
│           {xterm}                 │            {app preview}                │  <- Content
│                                   │                                         │
└───────────────────────────────────┴─────────────────────────────────────────┘
```
- LEFT panel has its own header: `>_ Agent Terminal [CLAUDE (SAFE)]`
- RIGHT panel has its own header: `Preview | Code | Terminal | Chat | Agent View`
- Headers are INSIDE their respective panels, side-by-side

### Fix implemented:
1. Made `terminal-ui__terminal-bar` mobile-only (for mobile view switcher)
2. Added `terminal-ui__panel-header` inside `terminal-ui__terminal-wrapper` (left panel header)
3. Added `terminal-ui__panel-header` inside `terminal-ui__iframe-pane` (right panel header with tabs)
4. Both panel headers use same CSS (`var(--bg-tertiary)`, 40px height)
5. Changed `terminal-ui__terminal-wrapper` to `flex-direction: column` to stack header + terminal

---

## Bug 7: Shell Exit Closes Entire Right Panel

**Status**: Fixed (2026-01-26)

**Description**: When the shell/terminal in the right panel exits, it closes the entire right panel.

**Expected behavior**: When shell exits, automatically switch to the App Preview tab instead of closing the panel.

**Current behavior**: ~~Exiting the shell closes the entire right panel.~~ Fixed - shell exit now switches to Preview tab.

**Fix implemented**: Changed the `swe-swe-session-ended` message handler in terminal-ui.js from `closeIframePane()` to `switchPanelTab('preview')`. The right panel now stays visible and shows App Preview when the shell exits.

---

## Bug 8: Agent Label Missing Safe/YOLO Indicator

**Status**: Fixed (2026-01-26)

**Description**: The agent label on the session page (e.g., "CLAUDE") does not show whether the session is in "Safe" or "YOLO" mode.

**Expected behavior**: Agent badge should display the mode, e.g., "CLAUDE (safe)" or "CLAUDE (YOLO)" to match the design shown in screenshots.

**Current behavior**: ~~Agent badge only shows the agent name without the mode indicator.~~ Fixed - badge now shows "NAME (safe)" or "NAME (YOLO)".

**Fix implemented**: Updated `updateStatusInfo()` in terminal-ui.js to:
- Use `querySelectorAll` to update all assistant badges (mobile + desktop)
- Include the mode indicator in badge text: `${name} (${mode})`

---

## Files to Investigate

- `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js`
  - `sendChatMessage()` - line 1718
  - `showChatNotification()` - line 1744
  - Chat overlay HTML in template

- `cmd/swe-swe/templates/host/swe-swe-server/static/styles/terminal-ui.css`
  - Chat-related styles

- `cmd/swe-swe/templates/host/swe-swe-server/main.go`
  - Session naming handling via `?name=` parameter

- `cmd/swe-swe/templates/host/swe-swe-server/templates/homepage.html` (or similar)
  - Session list rendering
  - Keep/trash icon visibility CSS
