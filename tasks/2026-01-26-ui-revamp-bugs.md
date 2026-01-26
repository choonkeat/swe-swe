# UI Revamp Bugs

Bugs introduced after the terminal UI revamp that need to be fixed.

## Bug 1: Session Naming Broken

**Status**: Open

**Description**: The ability to name a session is broken after the UI revamp.

**Expected behavior**: User should be able to name/rename their session.

**Current behavior**: TBD - need to investigate how this was previously exposed and what broke.

**Investigation needed**:
- [ ] Find where session naming UI was previously located
- [ ] Check if the `?name=` URL parameter still works
- [ ] Determine if this needs a UI element (input field, edit button, etc.)

---

## Bug 2: Chat Feature Not Discoverable/Broken

**Status**: Open

**Description**: The chat feature is not accessible or not obvious how to use after the UI revamp.

**Expected behavior**: Users should be able to send chat messages to collaborate with others viewing the same session.

**Current behavior**: TBD - need to investigate if chat UI is hidden, removed, or just not discoverable.

**Investigation needed**:
- [ ] Find where chat UI was previously located (likely in status bar or overlay)
- [ ] Check if chat WebSocket messages still work (`type: 'chat'`)
- [ ] Determine if chat input toggle is missing from the new UI
- [ ] Check `terminal-ui__chat-overlay` and `terminal-ui__chat-input` elements

---

## Bug 3: Session Keep/Trash Icons Always Visible (Should Be Hover-Only)

**Status**: Fixed (2026-01-26)

**Description**: The session keep (bookmark) and trash (delete) icons in the Session Recordings list are always visible.

**Expected behavior**: Icons should be hidden by default and only appear when hovering over the recording row.

**Current behavior**: Icons are always visible.

**Fix approach**: Add CSS to hide `.recording-card__btn-group` by default and show on `.recording-card:hover`.

---

## Bug 4: Preview Panel Has Redundant URL Bars

**Status**: Open

**Description**: The preview panel shows two URL-related inputs: a read-only location bar displaying the current URL, plus a separate "Enter URL to debug..." input field below it.

**Expected behavior**: A single URL bar that serves both purposes (shows current URL and allows navigation/debugging).

**Current behavior**: Two separate UI elements for URLs which is confusing and wastes vertical space.

**Screenshot reference**: See annotated screenshot showing the preview area with both URL elements.

**Fix options**:
1. Combine into a single editable URL bar (recommended)
2. Remove the debug URL input if redundant
3. Clarify the purpose of each if they serve different functions

---

## Bug 5: Session Page Top Navigation Colors Inconsistent

**Status**: NEEDS REDO (depends on Bug 6 fix)

**Description**: The colors of the top navigation elements on the session page don't match - some areas use the correct navy blue theme while others have different styling.

**Expected behavior**: All navigation elements should use consistent navy blue theme:
- Main header bar (with session ID, back button)
- Left panel header (Agent Terminal + badge)
- Right panel header (Preview, Code, Terminal tabs)

**Current behavior**: Inconsistent colors across the navigation elements.

**Fix approach**: After Bug 6 is fixed (separate panel headers), ensure both panel headers use same CSS variables for consistent navy blue background.

---

## Bug 6: Second-Level Navigation Layout Wrong

**Status**: NEEDS REDO (previous fix was wrong)

**Description**: Each panel should have its own header row, NOT one shared full-width nav bar.

### CORRECT Layout (target):
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

### WRONG Layout (current - our broken fix):
```
┌─────────────────────────────────────────────────────────────────────────────┐
│ ←  Session  ● Connecting...                                            ⚙   │  <- Header
├─────────────────────────────────────────────────────────────────────────────┤
│ >_ Agent Terminal [CLAUDE] | Preview  Code  Terminal  Agent View            │  <- WRONG: one full-width bar
├───────────────────────────────────┬─────────────────────────────────────────┤
│                                   │  ⌂ ↻ http://...  [URL input] →          │  <- URL bar in wrong place
│           {xterm}                 ├─────────────────────────────────────────┤
│                                   │            {app preview}                │
└───────────────────────────────────┴─────────────────────────────────────────┘
```
- WRONG: All tabs merged into one full-width bar
- WRONG: Panel tabs (Preview, Code, etc.) not associated with right panel

### Fix approach:
1. Remove panel tabs from terminal-bar (undo previous change)
2. Put panel tabs back inside `.terminal-ui__iframe-pane` as header
3. Each panel should have its own header row that spans only that panel's width
4. The two panel headers sit side-by-side at the same vertical position

---

## Bug 7: Shell Exit Closes Entire Right Panel

**Status**: Open

**Description**: When the shell/terminal in the right panel exits, it closes the entire right panel.

**Expected behavior**: When shell exits, automatically switch to the App Preview tab instead of closing the panel.

**Current behavior**: Exiting the shell closes the entire right panel, leaving only the Agent Terminal visible.

**Problem**: The new design doesn't have a layout without the right panel - it should always be visible. Closing the panel breaks the intended layout.

**Fix approach**: On shell exit event, switch the active tab to "Preview" (App Preview) instead of hiding/closing the panel.

---

## Bug 8: Agent Label Missing Safe/YOLO Indicator

**Status**: Open

**Description**: The agent label on the session page (e.g., "CLAUDE") does not show whether the session is in "Safe" or "YOLO" mode.

**Expected behavior**: Agent badge should display the mode, e.g., "CLAUDE (safe)" or "CLAUDE (YOLO)" to match the design shown in screenshots.

**Current behavior**: Agent badge only shows the agent name without the mode indicator.

**Fix approach**: Update the agent badge rendering to include the current autoApprove mode (safe/YOLO) from session state.

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
