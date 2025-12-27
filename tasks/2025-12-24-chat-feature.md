# Chat Feature Implementation Plan

**Started**: 2025-12-24 15:23:25
**Design Doc**: `research/2025-12-24-chat-window.design.md`
**Prototype**: `cmd/swe-swe-server/static/terminal-with-chat.html`

---

## Overview

Implement real-time chat overlay feature for terminal sessions. Users can chat with other viewers of the same session. Design is Minecraft/Quake style - messages overlay terminal, auto-fade after 5 seconds.

**Key Features**:
- Username management with localStorage persistence (`swe-swe-username`)
- Click "n viewers" or any message to chat
- Server keeps last 10 messages per session
- Clients keep in-memory message history
- Clickable username to rename

---

## Implementation Steps

### Step 1: Frontend - Add Chat UI Markup & Basic CSS
**Goal**: Add chat overlay/input DOM elements and styling to terminal-ui.js

**What to do**:
1. Open `cmd/swe-swe-server/static/terminal-ui.js`
2. Find where the status bar is created (search for `terminal-ui__status-bar`)
3. Add `.chat-overlay` and `.chat-input-overlay` containers to the Web Component's template
4. Add CSS for:
   - `.chat-overlay` - absolute positioned at top, flex column, z-index 100
   - `.chat-message` - message styling with fade animations
   - `.chat-message.own` - blue variant
   - `.chat-message.other` - gray variant
   - `.chat-input-overlay` - bottom center input box, hidden by default
   - `.chat-send-btn` + `.chat-cancel-btn` - button styling
5. Do NOT add any JavaScript functionality yet - just the static HTML/CSS

**Test**:
- Load the terminal in browser
- Verify markup exists in DOM (inspect element)
- Verify styles apply (no layout errors, colors correct)
- Verify overlays are hidden initially
- Take screenshot and compare with prototype mockup visually

**Files to modify**:
- `cmd/swe-swe-server/static/terminal-ui.js` (add markup + CSS)

---

### Step 2: Frontend - Add Username Management (localStorage)
**Goal**: Implement username prompt, validation, and storage

**What to do**:
1. Add these properties to TerminalUI class:
   - `currentUserName` (string, null initially)
   - `unreadChatCount` (number)
2. Add method `validateUsername(name)` - returns `{valid: bool, error?: string, name?: string}`
   - Trim whitespace
   - Check length: 0 < len <= 16
   - Check chars: `/^[a-zA-Z0-9 ]+$/`
   - Return validation result
3. Add method `getUserName()` - prompts until valid
   - Check `this.currentUserName` first (in-memory)
   - Then check `localStorage.getItem('swe-swe-username')`
   - If not found, loop with `window.prompt()` until valid name or user cancels
   - Store in localStorage and `this.currentUserName`
   - Return username or null if cancelled
4. Add method `updateUsernameDisplay()` - updates status bar
   - If username set: show "YourName • n viewers"
   - Otherwise: show "n viewers"
5. Wire up: Make "n viewers" clickable
   - Find status info element (`.terminal-ui__status-info`)
   - Add click handler that calls `this.getUserName()` then `this.openChatInput()`

**Test**:
- Open terminal, click "n viewers"
- Verify prompt appears
- Enter valid name (e.g., "Alice") - should accept, close prompt, update display
- Refresh page, click "n viewers" - should NOT prompt (uses localStorage)
- Clear localStorage manually (`localStorage.clear()`), refresh, click viewers - should prompt again
- Try invalid names:
  - Empty string → re-prompts
  - "toolongname1234567890" (>16) → re-prompts with error
  - "alice@bob" (special chars) → re-prompts with error
- Cancel prompt → chat doesn't open
- Verify status bar shows "YourName • n viewers" format

**Files to modify**:
- `cmd/swe-swe-server/static/terminal-ui.js` (add methods, wire up button)

---

### Step 3: Frontend - Make Username Clickable to Rename (Option A)
**Goal**: Allow renaming by clicking username in status bar

**What to do**:
1. Modify `updateUsernameDisplay()` to wrap username in a clickable span
   - Show: `<span class="terminal-ui__username">YourName</span> • n viewers`
2. Add click handler to username span
   - Calls `this.promptRenameUsername()`
3. Add method `promptRenameUsername()`
   - Loop with `window.prompt('Enter new name:')` until valid
   - Update localStorage and `this.currentUserName`
   - Update display
   - Show updated name immediately

**Test**:
- After username is set, status bar shows "YourName • 3 viewers"
- Click on "YourName" part (not the viewers count)
- Prompt appears asking for new name
- Enter valid new name → updates immediately
- Refresh page → new name persists
- Try renaming with invalid input → re-prompts
- Cancel rename → name doesn't change

**Files to modify**:
- `cmd/swe-swe-server/static/terminal-ui.js` (add rename method, update display markup)

---

### Step 4: Frontend - Add Chat Input Toggle & Message Display (No WebSocket yet)
**Goal**: Implement chat input opening/closing and basic message display

**What to do**:
1. Add properties:
   - `chatInputOpen` (boolean)
   - `chatMessages` (array of {userName, text, timestamp})
2. Add method `openChatInput()`
   - Set `chatInputOpen = true`
   - Add `.active` class to `.chat-input-overlay`
   - Focus the input field
   - Clear unread badge
3. Add method `closeChatInput()`
   - Set `chatInputOpen = false`
   - Remove `.active` class
   - Blur the input
4. Add method `toggleChatInput()`
   - If open, close; else open (call `getUserName()` first)
5. Add method `addChatMessage(userName, text, isOwn)`
   - Create message element DOM
   - Add to `.chat-overlay`
   - Add class `.own` or `.other`
   - Show format: `<span class="chat-message-username">userName:</span> text`
   - Schedule fade-out after 5 seconds
   - Add click handler → calls `openChatInput()`
   - Push to `this.chatMessages` array (in-memory storage)
6. Wire up Send/Cancel buttons:
   - Find them in DOM (`.chat-send-btn`, `.chat-cancel-btn`)
   - Send button onclick → calls `this.sendChatMessage()`
   - Cancel button onclick → calls `this.closeChatInput()`
7. Add method `sendChatMessage()`
   - Get text from input
   - If empty, return
   - If no username, call `getUserName()`
   - Call `this.addChatMessage(this.currentUserName, text, true)`
   - Clear input, keep focus

**Test**:
- Click "n viewers" or any message → input appears at bottom
- Type text, click Send → message appears in blue at top, fades after 5s
- Click Cancel → input closes, doesn't send
- Click message → input opens again
- Press Enter in input → sends message
- Press Esc in input → closes (if implemented)
- Verify messages have correct username displayed
- Open DevTools, check `this.chatMessages` array contains all messages

**Files to modify**:
- `cmd/swe-swe-server/static/terminal-ui.js` (add all methods, CSS animations for fade)

---

### Step 5: Frontend - Add Unread Message Badge
**Goal**: Show notification badge when messages arrive while chat is closed

**What to do**:
1. Add badge element to status bar next to viewers count
   - `.chat-notification` - red badge with count
   - Hidden initially
2. Add method `showChatNotification(count)`
   - Update badge text with count
   - Show badge (remove `display: none`)
3. Add method `clearChatNotification()`
   - Hide badge
4. In `addChatMessage()`:
   - If message is from other user (not `isOwn`) and chat input not open
   - Increment `this.unreadChatCount`
   - Call `this.showChatNotification(this.unreadChatCount)`
5. In `openChatInput()`:
   - Clear unread count
   - Call `this.clearChatNotification()`

**Test**:
- Open chat, send a message
- Close chat (press Cancel)
- Create a fake "other user" message (via DevTools: `terminalUI.addChatMessage('Bob', 'hello', false)`)
- Verify red badge appears on "n viewers" showing "1"
- Add more messages → badge updates to "2", "3", etc.
- Click "n viewers" to open chat → badge disappears
- Close and add more messages → badge reappears with new count

**Files to modify**:
- `cmd/swe-swe-server/static/terminal-ui.js` (add badge methods, update addChatMessage)

---

### Step 6: Frontend - Handle Chat Message History from Server
**Goal**: Prepare to receive initial chat history on connection (server not implemented yet)

**What to do**:
1. Add handler in `handleJSONMessage()`:
   - `case 'chat_history'`:
     - Receive `msg.messages` array
     - For each message, call `addChatMessage(msg.userName, msg.text, false)` (don't show notifications)
     - Messages should appear but not trigger badges
2. Add handler for new incoming messages:
   - `case 'chat'`:
     - Extract `msg.userName`, `msg.text`
     - Determine if `isOwn` by comparing to `this.currentUserName`
     - Call `addChatMessage(msg.userName, msg.text, isOwn)`

**Test**:
- Don't send to server yet
- Mock these message types manually in DevTools:
  ```javascript
  // Simulate chat history on connection
  terminalUI.handleJSONMessage({
    type: 'chat_history',
    messages: [
      {userName: 'Alice', text: 'Hello everyone!'},
      {userName: 'Bob', text: 'Hi Alice!'}
    ]
  });

  // Simulate incoming message
  terminalUI.handleJSONMessage({
    type: 'chat',
    userName: 'Charlie',
    text: 'What are we building?'
  });
  ```
- Verify messages appear in overlay
- Verify no errors in console

**Files to modify**:
- `cmd/swe-swe-server/static/terminal-ui.js` (add handlers)

---

### Step 7: Backend - Add Chat Message Type & Session Storage
**Goal**: Add data structures for chat on backend

**What to do**:
1. In `cmd/swe-swe-server/main.go`:
2. Define struct:
   ```go
   type ChatMessage struct {
       UserName  string    `json:"userName"`
       Text      string    `json:"text"`
       Timestamp time.Time `json:"timestamp"`
   }
   ```
3. Add to Session struct:
   ```go
   chatMessages []ChatMessage
   chatMutex    sync.RWMutex
   ```
4. Add method `AddChatMessage(userName, text string)`:
   - Lock `chatMutex`
   - Create new ChatMessage
   - Append to `chatMessages`
   - If len > 10, remove first message
   - Unlock
5. Add method `GetChatHistory()`:
   - Lock `chatMutex` (read)
   - Return copy of `chatMessages` (last 10)
   - Unlock
6. Add method `BroadcastChat(userName, text string)`:
   - Call `AddChatMessage(userName, text)`
   - Create JSON message: `{type: "chat", userName, text, timestamp}`
   - Broadcast to all clients in session
7. Add method `SendChatHistory(client *websocket.Conn)`:
   - Get history via `GetChatHistory()`
   - Create JSON message: `{type: "chat_history", messages: [...]}`
   - Send to single client

**Test**:
- Build and verify no compile errors: `make build`
- Check Session struct has chat fields
- Check methods exist and have correct signatures

**Files to modify**:
- `cmd/swe-swe-server/main.go` (add struct, methods)

---

### Step 8: Backend - Handle Incoming Chat Messages from Client
**Goal**: Receive and broadcast chat messages

**What to do**:
1. In the WebSocket handler where you handle incoming messages
2. Add parsing for text messages that are NOT file uploads or resizes
3. Try to unmarshal as JSON
4. If `type == "chat"`:
   - Extract `userName` and `text` from message
   - Validate: userName matches stored client info (or use client's session username if tracked)
   - Call `session.BroadcastChat(userName, text)`
   - Don't respond with error/success (just broadcast)
5. If parsing fails or type unknown, ignore (don't crash)

**Test**:
- Build: `make build`
- Start server: `make run`
- Open two terminal windows to same session
- In one window, open chat and send message
- Verify message appears in both windows
- Refresh one window → previous messages should appear via `chat_history`

**Files to modify**:
- `cmd/swe-swe-server/main.go` (add handler in WebSocket reader)

---

### Step 9: Frontend - Send Chat Messages to Server
**Goal**: Wire up actual WebSocket sending

**What to do**:
1. In `sendChatMessage()` method in terminal-ui.js:
2. Before calling `addChatMessage()`, send to server:
   ```javascript
   if (this.ws && this.ws.readyState === WebSocket.OPEN) {
       this.ws.send(JSON.stringify({
           type: 'chat',
           userName: this.currentUserName,
           text: text
       }));
   }
   ```
3. Still call `addChatMessage()` locally for immediate UI update
4. Server will broadcast back to us and other clients

**Test**:
- Open two browser windows to same session
- In window 1, open chat (enter name "Alice")
- In window 2, open chat (enter name "Bob")
- Window 1 sends message "Hello Bob"
- Verify appears in both windows
- Window 2 sends reply "Hi Alice"
- Verify appears in both windows
- Refresh window 1 → should see last 10 messages via history
- Test with 3+ simultaneous viewers

**Files to modify**:
- `cmd/swe-swe-server/static/terminal-ui.js` (update sendChatMessage)

---

### Step 10: Frontend - Load Chat History on Connection
**Goal**: Send chat history to new clients when they connect

**What to do**:
1. In backend, after `session.AddClient(conn)`, call:
   ```go
   session.SendChatHistory(conn)
   ```
2. Verify frontend handles `chat_history` message type (from Step 6)

**Test**:
- Open window 1, chat, send 3 messages
- Open window 2 → verify it immediately sees the last 3 messages (via history)
- Those messages should appear without notification badges
- New incoming message to window 2 should show badge

**Files to modify**:
- `cmd/swe-swe-server/main.go` (call SendChatHistory in AddClient)

---

### Step 11: Edge Cases & Polish
**Goal**: Handle edge cases and verify robustness

**What to do**:
1. Test:
   - User closes tab/disconnects mid-chat → messages don't hang
   - Rapid message sending (spam) → all appear
   - Very long username/message → truncate or wrap gracefully
   - Connection loss → graceful degradation
   - Session with 0 messages → joining doesn't error
   - Message with newlines → escape/handle properly
2. Verify:
   - No console errors
   - No race conditions on server
   - Cleanup on disconnect
3. Optional: Add XSS protection (escape HTML in messages)

**Test**:
- Stress test with multiple connections
- Check server logs for errors
- Verify no memory leaks (watch connection/session counts)

**Files to modify**:
- Both frontend and backend as needed

---

### Step 12: Final Testing & Documentation
**Goal**: End-to-end validation and update docs

**What to do**:
1. Test full workflow:
   - New user joins session → prompted for name
   - User sends message → appears immediately to self, after broadcast delay to others
   - User renames → updates in their own messages
   - User refreshes → name persists, sees message history
   - Multiple users chat simultaneously
2. Test mobile/tablet view → buttons reachable, overlays not cutoff
3. Update design doc with any deviations
4. Test with actual xterm.js terminal (ensure overlays don't interfere)

**Test**:
- Manual testing with real scenarios
- Screenshot comparisons with original prototype
- No regressions to existing terminal functionality

**Files to modify**:
- None (testing only)

---

## Progress Tracking

- [x] Step 1: DOM markup + CSS ✅ (2025-12-24 15:35)
  - Added `.terminal-ui__chat-overlay` (absolute positioned, z-index 100)
  - Added `.terminal-ui__chat-input-overlay` (hidden by default)
  - Added `.terminal-ui__chat-message` styling with fade animations
  - Added `.terminal-ui__chat-send-btn` and `.terminal-ui__chat-cancel-btn`
  - Verified: All elements in DOM, styles applied, pointer-events correct, no terminal interference
- [x] Step 2: Username management (localStorage + prompt) ✅ (2025-12-24 15:42)
  - Added `validateUsername()` - max 16 chars, alphanumeric + spaces
  - Added `getUserName()` - checks localStorage first, prompts if needed, stores in localStorage
  - Added `updateUsernameDisplay()` - shows "UserName | Claude | 87×37 | 1 viewer"
  - Tested: Click viewers → prompt appears → enter "Alice" → name persists in localStorage
- [x] Step 3: Clickable username to rename ✅ (2025-12-24 15:42)
  - Added `promptRenameUsername()` - allows clicking username to change it
  - Wired status bar click handler to detect clicks on username vs viewers
  - Tested: Username displays and is clickable to rename
- [x] Step 4: Chat input UI + local message display ✅ (2025-12-24 15:42)
  - Added `openChatInput()`, `closeChatInput()`, `toggleChatInput()`
  - Added `addChatMessage()` - adds message to overlay, schedules fade after 5s
  - Added `sendChatMessage()` - gets text, validates username, sends JSON to server
  - Added `escapeHtml()` - prevents XSS
  - Tested: Click viewers → chat input opens, type message, click Send → message appears and fades
- [x] Step 5: Unread message badge ✅ (2025-12-24 15:42)
  - Added `showChatNotification()` - shows red badge with count
  - Added `clearChatNotification()` - hides badge when chat opens
  - Badge created dynamically if needed
- [x] Step 6: Message handler preparation (mock testing) ✅ (2025-12-24 15:42)
  - Added handlers for `chat` and `chat_history` message types
  - Wired Send button to call `sendChatMessage()`
  - Wired Cancel button to call `closeChatInput()`
  - Enter key sends, Esc closes (bonus features)
  - Tested: All methods callable, no errors in console
- [x] Step 7: Backend chat structures ✅ (Commit 9ea5a3f)
- [x] Step 8: Backend message handler ✅ (Commit 9ea5a3f)
- [x] Step 9: Frontend WebSocket sending ✅ (Commit 9ea5a3f)
- [x] Step 10: Chat history on connection ✅ (Commit 9ea5a3f)
- [x] Step 11: Edge cases & polish ✅ (In progress)
  - Simplified message send logic (removed local echo, rely on server broadcast)
  - Added sans-serif fonts to chat UI for better readability
  - Repositioned chat overlay to top-right corner with right-alignment
  - Set max-width to 40% to minimize terminal content coverage
  - Tested with special characters, XSS payloads, long messages, rapid sends
  - Verified multi-client chat with proper message history
- [x] Step 12: Final testing & docs ✅ (2025-12-24 17:30)
  - Verified multi-client chat workflow with proper user isolation
  - Confirmed chat history loads correctly on client connection
  - Tested viewer count updates (showed "2 viewers" with 2 clients)
  - Verified notification badge shows unread message count
  - Tested username persistence across page refresh
  - Confirmed no duplicate messages in UI
  - Verified top-right positioning minimizes terminal coverage
  - Updated websocket-protocol.md with correct implementation details
  - All features working as designed, no console errors

---

## Final Summary

✅ **Chat feature fully implemented and tested**

### Commits
1. `94b1298` - feat: add chat overlay UI markup and CSS
2. `3d2610b` - feat: implement chat UI with username and messaging
3. `9ea5a3f` - feat: implement chat backend with message buffer
4. `1e11fd7` - feat: polish chat UI - top-right positioning and sans-serif fonts
5. `d298304` - docs: update websocket-protocol with correct implementation details

### Key Features
- Real-time chat overlay with top-right positioning
- Username management with localStorage persistence (key: `swe-swe-username`)
- Max 16 characters, alphanumeric + spaces only
- Click "n viewers" to open chat or click any message
- Clickable username to rename (Option A)
- Server keeps last 10 messages per session
- Chat history auto-sent to new clients
- Auto-fade messages after 5 seconds
- XSS protection via HTML escaping
- Works with multiple concurrent viewers
- Thread-safe operations with mutexes on backend

### Files Modified
- `cmd/swe-swe-server/static/terminal-ui.js` - Frontend chat UI and logic
- `cmd/swe-swe-server/main.go` - Backend chat structures and handlers
- `docs/websocket-protocol.md` - Updated with chat message types

### Files Added
- `cmd/swe-swe-server/static/terminal-with-chat.html` - Prototype mockup
- `research/2025-12-24-1422-chat-window.design.md` - Design document
- `tasks/2025-12-24-152325-chat-feature.md` - This tracking document

## Notes

- **Design reference** - `research/2025-12-24-1422-chat-window.design.md`
- **Prototype reference** - `cmd/swe-swe-server/static/terminal-with-chat.html`
- **WebSocket protocol docs** - `docs/websocket-protocol.md`
- All changes follow Conventional Commit style
- Extensive browser testing with multi-client scenarios
- No regressions to existing terminal functionality
