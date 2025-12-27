# Implementation Plan: Status Bar Clickability and File Upload Loading UX

**Status:** ✅ COMPLETED

**Summary:** Successfully implemented status bar clickability UX and file upload overlay with queue management. All tests passed with no regressions.

---

## Overview

Implement two connected UX features:
1. **Status bar clickability:** Make clickable status bar elements look and behave like hyperlinks
2. **File upload overlay:** Show blocking overlay during file transfers with queue awareness

---

## Implementation Steps

### Phase 1: Prepare and Understand Current State

- [x] Step 1.1: Explore existing status bar component structure
  - Find status bar component file
  - Identify existing styles and event handlers
  - Document current clickable elements
  - **Test:** Visual inspection that component renders correctly
  - **Progress:** ✓ Found in `cmd/swe-swe-server/static/terminal-ui.js`, documented all clickable elements and existing styles

- [x] Step 1.2: Explore existing file upload handling
  - Find file drag-drop implementation
  - Identify file upload logic and state management
  - Document current upload flow
  - **Test:** Drag a file and confirm upload proceeds
  - **Progress:** ✓ Found client-side drag-drop (lines 1130-1170), server-side handling (lines 849-897), current feedback is temporary status bar message

### Phase 2: Status Bar Clickability (Non-Breaking)

- [x] Step 2.1: Add hover styles to clickable status bar elements
  - Add dotted underline and brightness filter on hover
  - Apply `cursor: pointer` to clickable elements
  - Scope styles within project-specific class
  - **Test:** ✓ Manual hover verification shows dotted underline applied correctly
  - **Progress:** ✓ Commit: 5622d9d
  - **Commit:** ✓ style: add clickable status bar hover states

- [x] Step 2.2: Ensure existing click handlers still work
  - Verify all status bar clicks trigger expected actions
  - **Test:** ✓ Clicked status text and rename dialog opened as expected
  - **Progress:** ✓ Click handlers working, no regression
  - **Commit:** ✓ (no changes needed)

### Phase 3: File Upload Loading Overlay

- [x] Step 3.1: Create overlay component
  - Build overlay component with queue display
  - Implement fade-in/fade-out animations
  - **Test:** ✓ Overlay renders without errors, CSS applies correctly, spinner animates
  - **Progress:** ✓ Commit: c54b2ee
  - **Commit:** ✓ feat: add file upload overlay component

- [x] Step 3.2: Add file upload queue state management
  - Track queued files (count and details)
  - Track upload progress state
  - **Test:** ✓ All queue operations tested and working correctly
  - **Progress:** ✓ Commit: 01985cb
  - **Commit:** ✓ feat: add file upload queue state tracking

- [x] Step 3.3: Integrate overlay with file upload flow
  - Show overlay when file upload starts
  - Update overlay with queue count
  - Auto-dismiss overlay after agent processing begins
  - Block terminal input while overlay visible
  - **Test:** ✓ Overlay displays with spinner, filename, and queue count. Overlay hides correctly after processing
  - **Progress:** ✓ Commit: 674553e
  - **Commit:** ✓ feat: integrate overlay with file upload

- [x] Step 3.4: Handle queued files
  - Accept multiple drag-drop files
  - Queue subsequent files during active upload
  - Process queued files sequentially
  - **Test:** ✓ Already implemented - drop handler loops through all files and queues them, processUploadQueue handles sequentially
  - **Progress:** ✓ Implemented in commit 674553e
  - **Commit:** ✓ (included in previous commit)

### Phase 4: Refinements and Edge Cases

- [x] Step 4.1: Add size threshold for overlay visibility
  - Only show overlay for uploads > 1s
  - **Test:** ✓ Quick uploads don't show overlay, delay tested
  - **Progress:** ✓ Commit: 3307e4c
  - **Commit:** ✓ feat: add upload duration threshold for overlay visibility

- [x] Step 4.2: Make overlay dismissible for long uploads
  - Add close button or ESC key handling
  - **Test:** ✓ ESC key successfully dismisses overlay
  - **Progress:** ✓ Commit: 77ace7e
  - **Commit:** ✓ feat: make upload overlay dismissible with ESC key

- [x] Step 4.3: Test full regression
  - Verify all existing features still work
  - Terminal input/output not affected outside overlay
  - Status bar interaction works as before
  - **Test:** ✓ All regression tests passed:
    - Status bar hover styles working (cursor, underline, transition)
    - All queue methods exist and callable
    - Upload overlay component exists
    - Drop overlay still functional
    - Terminal input still works
  - **Progress:** ✓ All tests passed, no regressions found
  - **Commit:** ✓ (no changes needed)

---

## Dependencies and Context

### Status Bar Component (Discovered Step 1.1)
- **File:** `cmd/swe-swe-server/static/terminal-ui.js`
- **Component:** `TerminalUI extends HTMLElement` class
- **Clickable Elements:**
  1. Entire status bar in error/reconnecting state - calls `this.connect()` to reconnect
  2. Status text (username) - shows user info, opens chat, or prompts rename
  3. Status links - markdown-style `[text](url)` that open in new tabs
- **Current styling:** Classes use `terminal-ui__` prefix
- **No existing test framework discovered** - manual testing required for now

### File Upload Handler (Discovered Step 1.2)
- **Client-side file:** `cmd/swe-swe-server/static/terminal-ui.js`
  - `setupFileDrop()`: lines 1130-1170 (drag-drop handlers)
  - `handleFile()`: lines 1172-1216 (upload logic)
  - `showTemporaryStatus()`: lines 744-759 (feedback display)
  - Drop overlay: lines 227-250 (CSS), toggles `.visible` class
- **Server-side file:** `cmd/swe-swe-server/main.go`
  - File upload handling: lines 849-897
  - Session state management: lines 92-142
- **Current feedback:** Temporary status message in status bar (3 second auto-hide)
- **Current overlay:** Drop overlay visible only during drag (not during upload)
- **No blocking overlay during upload** - just temporary status text

### CSS Scoping Approach
- Use `terminal-ui__` prefix for all new classes (matches existing pattern)
- Styles embedded in `terminal-ui.js` component

---

## Notes

- Keep changes small and atomic
- Each step should be independently testable
- Commit only relevant files, not entire directory
- Update this file after each completed step
