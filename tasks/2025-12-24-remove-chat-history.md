# Remove Server-Side Chat History Storage

## Objective
Remove chat history storage from server since chat UI is ephemeral (auto-disappears after 5 seconds). This aligns the implementation with the design philosophy.

## Context
- Chat messages currently stored on server (last 10 messages per session)
- Sent to new clients via `SendChatHistory()` when they join
- Client-side UI auto-fades messages after 5 seconds
- Architecture is inconsistent: UI is ephemeral but server keeps history

## Changes Summary
- Delete `ChatMessage` struct
- Remove `chatMessages` and `chatMutex` from Session
- Delete `AddChatMessage()`, `GetChatHistory()`, `SendChatHistory()` functions
- Modify chat handler to broadcast directly without storing
- Remove `chat_history` message handler from client
- Affected files: `cmd/swe-swe-server/main.go`, `cmd/swe-swe-server/static/terminal-ui.js`

## Implementation Plan

### Step 1: Create helper function for broadcasting chat without storage
**Files**: `cmd/swe-swe-server/main.go`

**Changes**:
- Add new function `BroadcastChatMessage(userName, text string)` that creates a ChatMessage on-the-fly and broadcasts it
- Does NOT store to history
- Can be called directly from WebSocket handler

**Why this step first**:
- Ensures we have a clear migration path
- Makes the next steps straightforward
- Minimizes risk of chat breaking

**Test plan**:
1. Open test session with 2+ clients
2. Send chat message from one client
3. Verify all clients receive the message
4. Verify message fades after 5 seconds on all clients
5. Verify no errors in server logs

**Git commit**: `refactor: add BroadcastChatMessage helper function`

---

### Step 2: Refactor chat message handler to use new function
**Files**: `cmd/swe-swe-server/main.go`

**Changes**:
- Update WebSocket handler "chat" case (line 901-906)
- Change from: `sess.AddChatMessage(msg.UserName, msg.Text)`
- Change to: `sess.BroadcastChatMessage(msg.UserName, msg.Text)`
- Remove logging that referenced AddChatMessage

**Test plan**:
1. Repeat Step 1 tests
2. Send multiple chat messages in sequence
3. Verify each message is broadcast to all clients
4. Verify no storing/buffering behavior

**Git commit**: `refactor: use BroadcastChatMessage in WebSocket handler`

---

### Step 3: Remove SendChatHistory call from AddClient
**Files**: `cmd/swe-swe-server/main.go`

**Changes**:
- Remove line 122: `go s.SendChatHistory(conn)`
- Remove comment on line 121: `// Send chat history to new client`
- Only BroadcastStatus remains

**Test plan**:
1. Open session with 1 client
2. Send chat message (confirm visible)
3. Open new client (2nd client)
4. Verify 2nd client does NOT see the previous message (since we removed SendChatHistory)
5. Send new message from first client
6. Verify both clients receive the new message
7. Verify no "chat_history" message is sent (check network tab or server logs)

**Git commit**: `refactor: remove SendChatHistory call from AddClient`

---

### Step 4: Delete unused functions from server
**Files**: `cmd/swe-swe-server/main.go`

**Changes**:
- Delete `AddChatMessage()` function (lines 267-287)
- Delete `GetChatHistory()` function (lines 318-327)
- Delete `SendChatHistory()` function (lines 329-359)

**Test plan**:
1. Verify code compiles: `make build`
2. Repeat Step 3 tests
3. Check no references remain to deleted functions (grep for function names)
4. Verify logs show no errors

**Git commit**: `refactor: remove AddChatMessage, GetChatHistory, SendChatHistory functions`

---

### Step 5: Remove ChatMessage struct and session fields
**Files**: `cmd/swe-swe-server/main.go`

**Changes**:
- Delete `ChatMessage` struct (lines 54-59)
- Delete `chatMessages []ChatMessage` field from Session (line 109)
- Delete `chatMutex sync.RWMutex` field from Session (line 110)

**Test plan**:
1. Verify code compiles: `make build`
2. Repeat all chat messaging tests
3. Verify no unused field warnings
4. Run full test suite if available

**Git commit**: `refactor: remove ChatMessage struct and unused session fields`

---

### Step 6: Remove chat_history handler from client
**Files**: `cmd/swe-swe-server/static/terminal-ui.js`

**Changes**:
- Delete `case 'chat_history':` block (lines 688-697)
- Keep `case 'chat':` handler intact (lines 681-686)

**Test plan**:
1. Open devtools console (check for errors)
2. Repeat messaging tests with multiple clients
3. Verify no reference to chat_history in code
4. Check network requests - should not see chat_history type messages

**Git commit**: `refactor: remove chat_history message handler from client`

---

### Step 7: Verify no regressions
**Files**: All relevant files

**Test plan - Comprehensive**:
1. Start fresh server: `make run`
2. Open session, connect 3+ clients
3. Send messages from different clients, verify all see them
4. Verify messages fade after 5 seconds
5. New client joins - should NOT see old messages
6. New client sends message - all see it
7. One client disconnects - rest continue chatting normally
8. Check server logs - no errors
9. Check browser console - no JS errors
10. Test chat input/output UI works smoothly

**Acceptance criteria**:
- ✅ Chat works real-time between connected clients
- ✅ Messages fade after 5 seconds
- ✅ No chat history sent to new clients
- ✅ No errors in logs or console
- ✅ Code compiles cleanly
- ✅ No unused imports or variables

**Git commit**: `test: verify no regressions after removing chat history`

---

## Files Modified

| File | Lines | Change |
|------|-------|--------|
| `cmd/swe-swe-server/main.go` | 54-110, 121-122, 267-359, 901-906 | Delete struct, fields, functions; refactor handler |
| `cmd/swe-swe-server/static/terminal-ui.js` | 688-697 | Delete chat_history handler |

## Rollback Plan
If issues occur:
1. `git reset --hard HEAD~7` to undo all changes (or cherry-pick reverting commits)
2. Restart server: `make run`
3. Test basic chat to confirm rollback worked

## Progress Tracking

- [x] Step 1: BroadcastChatMessage helper - DONE (commit: 8d85411)
- [x] Step 2: Refactor handler - DONE (commit: 40f566a)
- [x] Step 3: Remove SendChatHistory call - DONE (commit: 45a587a)
- [x] Step 4: Delete functions - DONE (commit: 4013a95)
- [x] Step 5: Remove struct and fields - DONE (commit: d20e609)
- [x] Step 6: Remove client handler - DONE (commit: 34c0391)
- [x] Step 7: Full regression testing - DONE

**Status**: ✅ ALL STEPS COMPLETE

## Verification Results

**Build**: ✅ Clean build with no errors
**Server logs**: ✅ No chat-related errors
**Removed code**: ✅ No references to ChatMessage, GetChatHistory, SendChatHistory, AddChatMessage, or chat_history
**Retained code**: ✅ Client-side UI methods intact, BroadcastChatMessage working correctly
**Chat functionality**: ✅ Real-time messaging between connected clients working
**Network**: ✅ No chat_history messages being sent (as expected)
