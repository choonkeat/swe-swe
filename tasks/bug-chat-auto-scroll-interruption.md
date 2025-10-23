# Bug: Chat Auto-Scroll Interrupts User Reading

## Problem Description
When a user scrolls up to read chat history and new messages arrive, the chat window automatically scrolls to the bottom, interrupting the user's reading. The expected behavior is that auto-scroll should only occur if the user was already at the bottom when new messages arrive.

## Current Behavior
- Every new message triggers `scrollToBottom()` port call in Elm
- JavaScript always scrolls to bottom regardless of current scroll position
- Users get forcibly moved away from what they're reading

## Root Cause Analysis

### Files Involved
1. **Elm Frontend** (`/workspace/elm/src/Main.elm`):
   - Lines 394, 402, 409, 430, 438, 445, 452, 481, 488, 496, 504, 584, 651, 682, 709: All call `scrollToBottom()` when receiving new messages
   - No logic to check if user was already at bottom before scrolling

2. **JavaScript Handler** (`/workspace/cmd/swe-swe/index.html.tmpl`):
   - Lines 168-179: `scrollToBottom` port handler always scrolls to bottom
   - Uses `window.scrollTo()` with smooth behavior but no condition checks

### Technical Details
- Elm sends `scrollToBottom ()` command for every new message type (ChatUser, ChatBot, ChatContent, etc.)
- JavaScript port handler executes unconditionally: `window.scrollTo({ top: document.body.scrollHeight, behavior: 'smooth' })`
- No state tracking of user's scroll position or intent

## Solution Plan

### Phase 1: Add Scroll Position Tracking
1. **JavaScript Changes**:
   - Track if user is "at bottom" before new messages arrive
   - Define "at bottom" as within ~50px of bottom (accounting for smooth scroll)
   - Monitor scroll events to update "at bottom" state
   - Only auto-scroll if user was already at bottom

2. **Implementation Details**:
   - Add scroll event listener to track current position
   - Modify `scrollToBottom` port to conditionally scroll
   - Add debouncing to avoid excessive scroll position checks

### Phase 2: User Experience Enhancements  
1. **Visual Indicators**:
   - Show subtle notification when new messages arrive while scrolled up
   - Add "scroll to bottom" button/indicator with message count
   - Make indicator disappear when user manually scrolls to bottom

2. **Smart Scroll Behavior**:
   - Preserve exact scroll position when new messages arrive
   - Optionally: Auto-scroll for user's own messages even when scrolled up

### Phase 3: Advanced Features (Optional)
1. **Scroll Memory**:
   - Remember scroll position across page reloads
   - Restore position when reconnecting after disconnect

2. **Configuration**:
   - Allow users to toggle auto-scroll behavior
   - Provide accessibility options for scroll behavior

## Files to Modify

### Critical Changes:
1. **`/workspace/cmd/swe-swe/index.html.tmpl`** (lines 168-179):
   - Replace unconditional scroll with conditional logic
   - Add scroll position tracking
   - Add new message indicator

2. **`/workspace/elm/src/Main.elm`** (multiple lines):
   - Add new port for conditional scrolling
   - Optionally add model state to track scroll behavior preferences

### Optional Enhancements:
1. **`/workspace/cmd/swe-swe/static/css/styles.css`**:
   - Add styles for new message indicators
   - Style scroll-to-bottom button

## Testing Strategy
1. **Manual Testing**:
   - Scroll up in chat history
   - Send new message from another tab/session  
   - Verify scroll position is preserved
   - Verify indicator appears for new messages
   - Verify auto-scroll works when already at bottom

2. **Edge Cases**:
   - Very fast message streams
   - Browser window resize during scroll
   - Mobile/touch scrolling behavior
   - Keyboard navigation

## Risk Assessment
- **Low Risk**: Changes are isolated to scroll behavior
- **No Breaking Changes**: Fallback behavior maintains current UX
- **Performance**: Minimal impact from scroll event monitoring

## Acceptance Criteria
- ✅ Auto-scroll only occurs when user is already at bottom
- ✅ Scroll position preserved when scrolled up  
- ✅ Visual indicator for new messages when scrolled up
- ✅ Smooth user experience with no jarring interruptions
- ✅ Backwards compatibility maintained