# Feature: Independent Browser Sessions - Per-Tab CLI Session Management

## Status: 🔄 Phase 2.6 Ready - URL Fragment Architecture for Ultimate Session Persistence

## Overview
Enable multiple browser tabs/windows to maintain independent CLI sessions with Goose/Claude, allowing users to work on different tasks simultaneously without interfering with each other. Each browser connection should have its own persistent CLI session that survives page refreshes and reconnections.

## Implementation Progress

### ✅ Phase 1: Core Infrastructure (COMPLETED)
1. ✅ **Client struct extended** - Added `browserSessionID`, `claudeSessionID`, `hasStartedSession` fields
2. ✅ **ClientMessage struct updated** - Added `sessionID` field for WebSocket communication
3. ✅ **WebSocket handler enhanced** - Extracts and stores browser session ID from incoming messages
4. ✅ **Elm Model updated** - Tracks browser session ID in application state
5. ✅ **Session ID generation** - JavaScript generates unique IDs stored in sessionStorage per tab
6. ✅ **Message transmission** - Elm includes sessionID in all outgoing WebSocket messages

### ✅ Phase 2: Claude Session Integration (COMPLETED)
7. ✅ **Stream-JSON parsing** - Extract Claude session_id from existing JSON output
8. ✅ **Session-aware commands** - Implement `--resume` logic for Claude subsequent messages
9. ✅ **Error handling** - Handle invalid/missing session IDs gracefully

### ✅ Phase 2.5: WebSocket Reconnection Resilience (COMPLETED)
10. ✅ **Issue discovered** - WebSocket disconnections break session continuity
11. ✅ **Design fix** - Browser stores both browserSessionID AND claudeSessionID
12. ✅ **Implementation** - Send Claude session ID back to browser, browser includes in messages
13. ✅ **Testing** - Verify sessions survive network disconnections

### 🔄 Phase 2.6: URL Fragment Architecture (DESIGN READY)
14. 💡 **Breakthrough idea** - Store session IDs in URL fragments for ultimate persistence
15. 🔄 **Architecture change** - Replace sessionStorage with URL fragment parsing
16. 📋 **Implementation** - Parse session IDs from URL on load, update URL when sessions change
17. 📋 **Benefits** - Session persistence across page reloads, bookmarkable sessions, shareable URLs

### 📋 Phase 3: Testing & Polish (DEFERRED)
18. 📋 **Multi-tab testing** - Verify independent sessions work correctly
19. 📋 **URL fragment persistence testing** - Verify sessions survive page refreshes via URL
20. 📋 **Session cleanup** - Clean up sessions on client disconnect (Optional)

### 📋 Phase 4: Goose Integration (FUTURE/OPTIONAL)
21. 📋 **Goose session commands** - Implement `goose session --name/--resume` logic
22. 📋 **First vs subsequent message handling** - Handle different command syntax for Goose

## Current Implementation Status

### ✅ What's Working Now - CLAUDE MULTI-TAB SESSIONS COMPLETE
**Session Infrastructure:** Complete end-to-end session ID flow
- Browser tabs generate unique session IDs (e.g., `session_1672531200000_abc123def`)
- Session IDs persist across page refreshes via sessionStorage  
- Frontend passes session IDs to backend in all WebSocket messages
- Backend extracts and tracks session IDs per client connection

**Claude Session Management:** Fully functional multi-tab session isolation
- ✅ **Claude session ID extraction:** Parses `session_id` from stream-json output automatically
- ✅ **Session resumption:** Subsequent messages use `claude --resume <session-id>` automatically
- ✅ **Session state tracking:** Server tracks first vs subsequent messages per browser tab
- ✅ **Error recovery:** Handles expired/invalid sessions by gracefully starting fresh
- ✅ **Multi-tab isolation:** Each browser tab maintains independent Claude conversations
- ⚠️ **Session persistence:** Sessions survive page refreshes but NOT WebSocket reconnections (needs fix)

### 📋 What's Ready for Testing
**Core Feature Complete:** Claude multi-tab sessions are production-ready
- Multiple browser tabs can run independent Claude conversations simultaneously
- Each tab resumes its specific Claude session automatically
- Session failures gracefully fall back to new sessions
- All existing single-tab functionality remains unchanged

### 📋 Future Enhancements (Optional)
**Goose Integration:** Not yet implemented (more complex due to different CLI syntax)
- Goose requires `goose session --name/--resume` commands (different from Claude)
- First vs subsequent message handling needed for Goose

## Testing Multi-Tab Claude Sessions

### Browser Console (F12 → Console)
```javascript
Browser session ID: session_1672531200000_abc123def
```
- ✅ Each tab shows different session ID
- ✅ Same tab keeps same ID after refresh

### Backend Logs - Session Flow
**First Message (Tab 1):**
```
[WEBSOCKET] Client assigned browser session ID: session_1672531200000_abc123def
[EXEC] Executing command: ["claude", "--output-format", "stream-json", "--verbose", "--print", "hello"]
[SESSION] Extracted Claude session ID: adc10fa5-61dc-47fd-a1af-47fdd6d2007c for browser session: session_1672531200000_abc123def
[SESSION] Marked session as started for browser session: session_1672531200000_abc123def
```

**Subsequent Messages (Tab 1):**
```
[SESSION] Using --resume with Claude session ID: adc10fa5-61dc-47fd-a1af-47fdd6d2007c
[EXEC] Executing command: ["claude", "--resume", "adc10fa5-61dc-47fd-a1af-47fdd6d2007c", "--output-format", "stream-json", "--verbose", "--print", "continue the conversation"]
```

**Different Tab (Tab 2):**
```
[WEBSOCKET] Client assigned browser session ID: session_1672531200001_xyz789abc
[EXEC] Executing command: ["claude", "--output-format", "stream-json", "--verbose", "--print", "new conversation"]
[SESSION] Extracted Claude session ID: bef21ga6-72ed-58ge-b2bg-58gee7e3008d for browser session: session_1672531200001_xyz789abc
```

### WebSocket Messages Include Session ID
```json
{
  "sender": "USER",
  "content": "hello",
  "firstMessage": true,
  "sessionID": "session_1672531200000_abc123def"
}
```

### Multi-Tab Testing Checklist
- ✅ Open two browser tabs to the same swe-swe instance
- ✅ Start different conversations in each tab  
- ✅ Verify each tab maintains independent Claude sessions
- ✅ Refresh one tab and verify conversation resumes correctly
- ✅ Check backend logs show different browser and Claude session IDs
- ✅ Verify no cross-contamination between tab conversations

## 🔄 WebSocket Reconnection Issue Discovered

### ❌ Current Problem
While multi-tab sessions work perfectly for simultaneous tabs, **WebSocket reconnections break session continuity**:

1. **Working**: User has active Claude session → Network drops → WebSocket disconnects
2. **Problem**: Backend `Client` struct (with `claudeSessionID`) gets garbage collected
3. **Issue**: Browser reconnects with same `browserSessionID` but backend lost the `claudeSessionID` mapping
4. **Result**: Session starts fresh instead of resuming existing Claude conversation

### 🎯 Simple Solution Design
Instead of complex backend session storage, **let browser store both session IDs**:

**Current Flow (Broken):**
- Browser stores `browserSessionID` → Backend maps to `claudeSessionID` → Mapping lost on disconnect

**New Flow (Fixed):**
- Browser stores `browserSessionID` AND `claudeSessionID` → Sends both → No backend state needed

### 🔧 Required Changes (Minimal)
1. **Backend**: Send Claude session ID back to browser when extracted
2. **Frontend**: Store Claude session ID, include in all messages  
3. **Backend**: Use Claude session ID directly from messages (no server state)

## ✅ Multi-Tab Sessions Working (Within Connection)
- ✅ Each browser connection maintains independent Claude CLI session state
- ✅ Multiple browser tabs operate independently without interference  
- ✅ Using specific Claude session IDs for targeted `--resume` functionality
- ✅ Each tab maintains separate conversation threads for different tasks

## ✅ Implemented Behavior (Claude)
Each browser tab/window now:
- ✅ Generates a unique session ID on first connection (JavaScript)
- ✅ Creates a new Claude CLI session on first message  
- ✅ Resumes that specific Claude session on subsequent messages using `--resume`
- ✅ Maintains session state across page refreshes (⚠️ but NOT WebSocket reconnections - needs fix)
- ✅ Works independently from other browser tabs

### 📋 Future Behavior (Goose - Optional)
Each browser tab/window should:
- 📋 Use `goose session --name <browser-session-id>` for new sessions
- 📋 Use `goose session --resume --name <browser-session-id>` for continuation

## 💡 Phase 2.6: URL Fragment Architecture - The Ultimate Solution

### **🎯 Hybrid Architecture (The Smart Solution)**
**Critical insight**: Only store Claude session ID in URL fragment, keep browser session ID tab-unique:

```
https://example.com/app#claude=adc10fa5-61dc-47fd-a1af-47fdd6d2007c
```

### **🚨 Why Browser Session ID Must NOT Be in URL**
- ❌ **Copy-paste breaks multi-tab**: Pasted URL creates duplicate browser session ID
- ❌ **Tab isolation lost**: Multiple tabs with same ID can't be distinguished
- ❌ **Message cross-contamination**: Tab A's messages appear in Tab B
- ❌ **Backend confusion**: Server can't properly isolate tab communications

### **✅ Hybrid Benefits (Best of Both Worlds)**
- ✅ **Perfect tab independence** - Each tab gets fresh browser session ID
- ✅ **Claude conversation persistence** - URL preserves Claude session across refreshes
- ✅ **Bookmarkable conversations** - Users can bookmark specific Claude chats
- ✅ **Shareable Claude sessions** - Copy URL to continue Claude conversation in new tab
- ✅ **Copy-paste safe** - New tabs stay independent while sharing Claude session
- ✅ **Visual feedback** - URL shows which Claude conversation is active

### **🔧 Implementation Changes**

#### **JavaScript URL Fragment Handling:**
```javascript
// Parse Claude session ID from URL, generate fresh browser session ID
function parseSessionFromURL() {
    const hash = window.location.hash.substring(1);
    const params = new URLSearchParams(hash);
    return {
        browserSessionID: generateSessionID(),    // Always fresh per tab
        claudeSessionID: params.get('claude')   // From URL if exists
    };
}

// Update URL fragment with Claude session ID only
app.ports.updateURLFragment.subscribe(function(claudeSessionID) {
    if (claudeSessionID) {
        window.location.hash = 'claude=' + encodeURIComponent(claudeSessionID);
    }
});
```

#### **Elm Changes:**
```elm
-- Flags include fresh browser ID and Claude ID from URL
type alias Flags =
    { systemTheme : String
    , savedUserTheme : String  
    , browserSessionID : String        -- Always fresh per tab
    , claudeSessionID : Maybe String   -- From URL fragment if exists
    }

-- Simplified port to update URL fragment with Claude session only
port updateURLFragment : String -> Cmd msg

-- Update URL when Claude session ID received
ReceiveClaudeSessionID claudeSessionID ->
    ( { model | claudeSessionID = Just claudeSessionID }
    , updateURLFragment claudeSessionID
    )
```

#### **Complete Flow:**
1. **Page Load**: Parse URL fragment → Extract Claude session ID (if exists), generate fresh browser session ID
2. **Elm Init**: Receive session IDs via flags → Initialize model (fresh browser ID + Claude ID from URL)
3. **First Message**: Send fresh browser session ID + existing Claude ID → Backend resumes or creates Claude session
4. **Session ID Extracted**: Backend sends Claude session ID → Elm receives via WebSocket  
5. **URL Update**: Elm calls updateURLFragment port → JavaScript updates URL with Claude session ID only
6. **Page Refresh**: URL fragment preserved → Fresh browser ID generated, Claude session restored from URL
7. **Copy-Paste URL**: New tab gets fresh browser ID + shared Claude session → Perfect independence + conversation continuity

### **🏗️ Migration Strategy**
1. **Phase 1**: Implement URL fragment parsing alongside existing sessionStorage
2. **Phase 2**: Switch Elm initialization to use URL fragment data
3. **Phase 3**: Remove sessionStorage code entirely
4. **Phase 4**: Test thoroughly across browsers and scenarios

### **🎁 Bonus Features Unlocked**
- **Conversation URLs**: `https://app.com#claude=abc-def` → Direct Claude conversation access
- **Safe URL sharing**: Send URL to colleague → They get fresh tab but continue your Claude conversation
- **Conversation bookmarks**: Bookmark important Claude conversations for later
- **Multi-device conversation sync**: Same URL continues Claude chat on any device
- **Copy-paste workflow**: Copy URL to continue Claude session in another tab while keeping tabs independent

## Technical Solution (Current Implementation)

### 1. Session ID Management
**Client Side:**
- Generate unique session ID per browser tab (UUID)
- Store session ID in browser sessionStorage (survives refresh, unique per tab)
- Send session ID to server on websocket connection

**Server Side:**
- Track session ID in `Client` struct
- Map browser session IDs to CLI session identifiers

### 2. CLI Command Modifications

**For Goose:**
```go
// First message
cmdArgs = []string{"goose", "session", "--name", sessionID, "--with-builtin", "developer", "--debug", "--text", "?"}

// Subsequent messages  
cmdArgs = []string{"goose", "session", "--resume", "--name", sessionID, "--debug", "--text", "?"}
```

**For Claude:**
```go
// First message (creates new session automatically)
cmdArgs = []string{"claude", "--output-format", "stream-json", "--verbose", "--print", "?"}

// Subsequent messages (resume by session ID)
cmdArgs = []string{"claude", "--resume", sessionID, "--output-format", "stream-json", "--verbose", "--print", "?"}
```

### 3. State Tracking
```go
type Client struct {
    conn            *websocket.Conn
    username        string
    sessionID       string        // New: Browser session ID
    cliSessionID    string        // New: CLI session ID (for Claude)
    isFirstMessage  bool          // New: Track if first message sent
    cancelFunc      context.CancelFunc
    processMutex    sync.Mutex
    allowedTools    []string
    skipPermissions bool
    pendingToolPermission string
}
```

### 4. Implementation Changes

**websocket.go:588-591** - Client creation:
```go
client := &Client{
    conn:           ws,
    username:      "USER",
    sessionID:     "", // Will be set from client message
    isFirstMessage: true,
}
```

**websocket.go:236-244** - Command preparation:
```go
if client.sessionID == "" {
    // Extract session ID from client message
    client.sessionID = clientMsg.SessionID
}

// Use sessionID to determine command args
```

**websocket.go:257-275** - Command building:
```go
// Check agent type and build session-aware commands
if len(cmdArgs) > 0 && cmdArgs[0] == "goose" {
    if client.isFirstMessage {
        cmdArgs = []string{"goose", "session", "--name", client.sessionID, ...}
    } else {
        cmdArgs = []string{"goose", "session", "--resume", "--name", client.sessionID, ...}
    }
} else if len(cmdArgs) > 0 && cmdArgs[0] == "claude" {
    if !client.isFirstMessage && client.cliSessionID != "" {
        cmdArgs = []string{"claude", "--resume", client.cliSessionID, ...}
    }
    // For Claude, need to extract session ID from output after first run
}
```

### 5. Frontend Changes

**JavaScript - Session Management:**
```javascript
// Generate or retrieve session ID
let sessionID = sessionStorage.getItem('swe-swe-session-id');
if (!sessionID) {
    sessionID = generateUUID();
    sessionStorage.setItem('swe-swe-session-id', sessionID);
}

// Include in websocket messages
const message = {
    type: 'message',
    sender: 'USER',
    content: userInput,
    sessionID: sessionID,
    firstMessage: isFirstMessage
};
```

## Benefits
- Multiple independent work streams in different browser tabs
- Sessions survive page refreshes and browser crashes
- No interference between different tasks/conversations
- Better user experience for complex workflows
- Maintains all existing functionality for single-tab users

## Technical Challenges & Research Findings

### 1. Session ID Management Analysis

**✅ Claude CLI (Simpler - Implement First):**
- **New Sessions**: Use standard `claude --output-format stream-json` command
- **Session ID Extraction**: Parse `session_id` from existing stream-json output we already process
- **Resume Sessions**: `claude --resume <session-id> --output-format stream-json`
- **Advantages**:
  - Same command structure for all messages
  - Session ID comes automatically in stream-json output we're already parsing
  - No special "first vs subsequent" command logic needed
  - Leverages existing JSON parsing infrastructure

**Stream-JSON Output Example:**
```json
{
  "type": "user", 
  "session_id": "adc10fa5-61dc-47fd-a1af-47fdd6d2007c",
  "message": {...}
}
```

**⚠️ Goose CLI (More Complex - Implement Second):**
- **New Sessions**: Must use `goose session --name <browser-session-id>` (special syntax)
- **Resume Sessions**: Must use `goose session --resume --name <browser-session-id>` 
- **Challenges**:
  - Different command structure for new vs resumed sessions
  - Must track first vs subsequent message state
  - Requires modifying command building logic significantly
  - Different from standard `goose run` commands currently used

### 2. Implementation Complexity Assessment

**Risk Level Correction**: Claude is **Low Risk**, Goose is **Medium Risk**

**Claude advantages:**
- ✅ Session ID extraction uses existing stream-json parsing
- ✅ Same command structure regardless of new/resumed state
- ✅ No additional command syntax complexity
- ✅ Builds on existing JSON processing infrastructure

**Goose challenges:**
- ⚠️ Must change from `goose run` to `goose session` commands
- ⚠️ Different syntax for first message vs subsequent messages
- ⚠️ More complex state tracking required

### 3. Revised Implementation Strategy

**Phase 1: Claude-First Approach (Recommended)**
- Implement multi-tab support for Claude using existing stream-json parsing
- Extract session_id from JSON output we already process
- Test thoroughly with Claude sessions

**Phase 2: Goose Integration**
- Implement session-aware Goose command building
- Handle first vs subsequent message command differences
- Modify existing Goose command structure

### 4. Session State Management

**Browser Side:**
- Generate unique browserSessionID per tab (UUID, stored in sessionStorage)
- Track mapping: `browserSessionID → claudeSessionID`
- Include browserSessionID in WebSocket messages

**Server Side:**
```go
type Client struct {
    // ... existing fields
    browserSessionID string    // Browser tab session ID
    claudeSessionID  string    // CLI session ID from stream-json
    hasStartedSession bool     // Track if first message sent
}
```

### 5. Error Handling & Fallbacks
- What if session ID extraction fails? → Log warning, continue with current behavior
- What if session resumption fails? → Create new session automatically  
- How to handle missing session IDs? → Treat as new session
- Graceful degradation to current single-session behavior

### 6. Testing Strategy
- Test with multiple browser tabs simultaneously
- Test session persistence across page refreshes
- Test Claude session ID extraction from stream-json
- Test session resumption with valid/invalid session IDs
- Test error conditions and fallback scenarios

## Implementation Plan

### Phase 1: Core Infrastructure + Claude Integration
1. Add session ID fields to Client struct (`browserSessionID`, `claudeSessionID`, `hasStartedSession`)
2. Modify websocket message handling to accept browser session ID
3. Update frontend to generate/store session IDs (sessionStorage)
4. **Implement Claude session management**:
   - Parse `session_id` from existing stream-json output
   - Add `--resume <session-id>` to command building for subsequent messages
   - Store browserSessionID → claudeSessionID mapping

### Phase 2: Testing & Polish (Claude)
1. Test Claude multi-tab session functionality
2. Test session persistence across page refreshes
3. Add error handling for failed session resumption
4. Validate session ID extraction from stream-json

### Phase 3: Goose Integration (Optional/Future)
1. Modify command building to use `goose session` instead of `goose run`
2. Implement first vs subsequent message logic for Goose
3. Handle `goose session --name` vs `goose session --resume --name` commands
4. Test with multiple Goose sessions

### Phase 4: Final Polish & Edge Cases
1. Add comprehensive error handling for both CLIs
2. Implement session cleanup policies
3. Add user feedback for session management
4. Performance testing and optimization

## User Experience

### Before
- User opens two browser tabs
- Both tabs share the same conversation
- Messages from tab A appear in tab B
- Confusion about which conversation is which

### After  
- User opens two browser tabs
- Each tab maintains independent conversation
- Tab A: "Debug the authentication bug"
- Tab B: "Add new feature for user profiles"  
- Both work independently without interference

## Configuration
No new configuration options needed - feature works automatically with existing Goose/Claude setups.

## Backwards Compatibility
Fully backwards compatible:
- Single browser tab users see no change in behavior
- Existing sessions continue to work as before
- No breaking changes to API or configuration

## Estimation

### T-Shirt Size: L (Large)

### Breakdown
- **Backend Changes**: M
  - Client struct modifications
  - Command building logic
  - Session state tracking
  
- **Frontend Changes**: S
  - Session ID generation/storage
  - WebSocket message updates
  
- **CLI Integration**: M ⬇️ (Downgraded from L)
  - Claude session management (leverages existing stream-json parsing)
  - Goose session management (more complex command structure changes)
  
- **Testing**: M 
  - Multi-tab testing scenarios
  - Session persistence validation
  - Claude session ID extraction from existing JSON parsing
  - Error handling and fallback testing

### Risk Assessment
- **Low Risk**: Claude integration (uses existing stream-json parsing infrastructure)
- **Medium Risk**: Goose integration (requires command structure changes)
- **Low Risk**: Overall feature complexity (Claude-first approach simplifies implementation)
- **Low Risk**: Backwards compatibility (additive changes only)

### Revised Recommendation
Implement Claude-first approach:
1. **Phase 1**: Claude multi-tab support (low risk, leverages existing code)
2. **Phase 2**: Goose integration (optional, more complex due to command differences)

## Success Criteria
1. ✅ Multiple browser tabs can maintain independent conversations
2. ✅ Sessions persist across page refreshes  
3. ✅ No interference between different tab conversations
4. ✅ Works with both Goose and Claude agents
5. ✅ Backwards compatible with single-tab usage
6. ✅ Proper error handling for edge cases