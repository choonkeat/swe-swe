# Status Bar Redesign Task

## Objective
Rearrange the bottom status bar to display:
- **When connected**: `Connected as {name} with {agent}` and if >1 viewer: ` and {n-1} others`. Dimensions and duration on right.
- **When connecting/error/reconnecting**: Keep current behavior unchanged.

## Key Requirements
1. User name from chat feature (`currentUserName`); show "??" if not set
2. Clicking "??" or status prompts user for their name
3. Display format: `Connected as Joe with Claude and 2 others` (for 3 total viewers)
4. Dimensions (`120×40`) displayed on right side before duration timer
5. Only apply new format when WebSocket is OPEN/Connected

## Implementation Steps

### Step 1: Analyze and document current status bar behavior
**Status**: COMPLETED
- Read full status bar HTML structure (lines 432-441)
- Read full updateStatusInfo() and related display functions (lines 701-803)
- Document current click handlers (lines 1020-1042)
- Identified all state variables:
  - `this.viewers`: Number of connected clients
  - `this.assistantName`: AI assistant name from server
  - `this.currentUserName`: Current user's name (null or string)
  - `this.ptyCols` / `this.ptyRows`: Terminal dimensions
  - `this.ws.readyState`: WebSocket connection state
- Current display formats documented:
  - Connected: `[Icon] Connected | Claude | 1 viewer | 120×40    [Duration]`
  - With username: `[Icon] Connected | Joe | Claude | 1 viewer | 120×40    [Duration]`
  - Connecting/Error: Shows status text without info section
- Click handlers:
  - Status bar click (lines 1020-1026): Reconnect on error/reconnecting
  - Info element click (lines 1029-1042): Chat trigger or rename prompt
  - Info element shows "viewer" to trigger chat, shows username to trigger rename

### Step 2: Refactor HTML structure for status bar
**Status**: COMPLETED
- Added `status-dims` span for dimensions display (line 439)
- Added CSS rule for `status-dims` with opacity: 0.9 (lines 219-221)
- Kept `status-info` and `status-timer` in place for backward compatibility
- Build test: Successfully compiled without errors
- Commit: fc3b85d "feat: add status-dims span to status bar HTML structure"

### Step 3: Update updateStatusInfo() logic
**Status**: COMPLETED
- Modified updateStatusInfo() to build "Connected as..." message (lines 705-737)
  - Uses `currentUserName || '??'` for user display
  - Builds message: "Connected as {name} with {agent}"
  - Adds " and {n-1} others" suffix when viewers > 1
  - Dimensions moved to separate status-dims span
- Simplified updateUsernameDisplay() to call updateStatusInfo() (lines 808-811)
- Keeping old behavior for Connecting/Error states via updateStatus() function
- Build test: Successfully compiled without errors
- Commit: 14ba0af "feat: update status bar to show 'Connected as {name} with {agent}' format"

### Step 4: Implement click handler for name prompt
**Status**: COMPLETED
- Updated click handler for status-info element (lines 1036-1050)
  - If "??" in text: prompt for name via getUserName()
  - If "and" in text (multiple viewers): trigger chat
  - If currentUserName set (single viewer): allow rename via promptRenameUsername()
- Reuses existing validation and localStorage functionality
- Build test: Successfully compiled without errors
- Commit: 1824421 "feat: update click handler to prompt for name when ?? is shown"

### Step 5: Test all connection state transitions
**Status**: COMPLETED
- ✓ Connect → display "Connected as ?? with Claude"
- ✓ Click "??" → name prompt appears
- ✓ Set name to "Alice" → display updates to "Connected as Alice with Claude"
- ✓ Add second viewer → both show "and 1 others" suffix
- ✓ Add third viewer → all show "and 2 others" suffix
- ✓ Click status with multiple viewers → chat input opens (confirms "and" detection works)
- ✓ Server stops → display shows "Reconnecting in Xs..." (old format preserved for errors)
- ✓ Dimensions display in separate span works correctly (112×58)
- ✓ Duration timer continues updating in all states

### Step 6: Verify no regressions
**Status**: COMPLETED
- ✓ Terminal input works and displays correctly
- ✓ Chat messages display correctly from both viewers
- ✓ Status bar click triggers name prompt and opens chat
- ✓ Multi-viewer chat synchronization works
- ✓ Name persistence: localStorage correctly restores "Alice" across sessions
- ✓ Duration timer continues updating (verified 30s+ uptime)
- ✓ Dimensions display correctly in separate span (112×58)
- ✓ Console shows no errors
- ✓ Each client maintains independent username (tab 0 shows ??, tab 1 shows Alice)
- No regression fixes needed

### Step 7: Final verification and cleanup
**Status**: COMPLETED
- ✓ All connection states display correctly (Connected, Connecting, Reconnecting, Errors)
- ✓ Responsive layout tested on mobile (375×667) - dimensions correctly shown as 42×36
- ✓ CSS is properly scoped with `.terminal-ui__` prefix
- ✓ All user interactions work: name prompt, chat trigger, chat messages
- ✓ Desktop layout verified (1280×800) - status bar shows correctly formatted message
- ✓ Screenshot captured showing final result
- ✓ No new commits needed - all functionality complete

## Technical Details

### Current Code Locations
- HTML structure: `cmd/swe-swe-server/static/terminal-ui.js` lines 432-441
- updateStatusInfo(): lines 701-713
- Status display update calls: scattered throughout (line 513, 521, 544, etc.)
- Click handlers: lines 1028-1042
- currentUserName state: line 21 (initialization)

### Current Display Format (when connected)
```
[Icon] Connected | Claude | 1 viewer | 120×40    [Duration]
```

### Target Display Format (when connected)
```
[Icon] Connected as Joe with Claude and 2 others    120×40 [Duration]
```

### State Variables to Consider
- `this.viewers` - number of connected clients
- `this.assistantName` - display name of AI assistant
- `this.currentUserName` - current user's name (or null/"??")
- `this.ptyCols` / `this.ptyRows` - terminal dimensions
- `this.ws.readyState` - WebSocket connection state

## Summary of Changes

### What was implemented
The bottom status bar has been successfully redesigned to display:
- **New format**: `Connected as {name} with {agent}` + viewer suffix for multiple viewers
- **Old format preserved**: "Connecting...", "Connection error", "Reconnecting in Xs..." for non-connected states
- **Dimensions moved**: Displayed in separate span before duration timer
- **Name management**: Shows "??" if not set, clicking prompts for name, localStorage persistence
- **Multiple viewers**: Suffix automatically shows " and {n-1} others" when viewers > 1

### Commits created
1. fc3b85d: feat: add status-dims span to status bar HTML structure
2. 14ba0af: feat: update status bar to show 'Connected as {name} with {agent}' format
3. 1824421: feat: update click handler to prompt for name when ?? is shown
4. 0d4544f: fix: load username from localStorage on page init and simplify name prompt

### Testing completed
- ✓ localStorage name auto-loads on page init (no need to click)
- ✓ Single viewer display: "Connected as Alice with Claude"
- ✓ Multiple viewers: "and 1 others" / "and 2 others" suffix works
- ✓ Chat integration: Multiple viewers can chat via clicking status bar
- ✓ Error/reconnecting states: Old format preserved
- ✓ Mobile responsive: Works correctly at 375×667
- ✓ Desktop layout: Works correctly at 1280×800
- ✓ Name prompt simplified to just "Your name"
- ✓ Status bar displays: "{icon} {status text} {dims} {duration}"
- ✓ No regressions: All existing features work

## Notes
- Use small, verifiable steps
- Each step has before/after test verification
- Only commit relevant files per step
- Document progress in this file as we go
- All requirements met per specification
