# Feature: Independent Browser Sessions - Per-Tab CLI Session Management

## Status: 🔄 In Progress (Phase 1 Complete, Phase 2 Pending)

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

### 📋 Phase 2: Claude Session Integration (PENDING)
7. 📋 **Stream-JSON parsing** - Extract Claude session_id from existing JSON output
8. 📋 **Session-aware commands** - Implement `--resume` logic for Claude subsequent messages

### 📋 Phase 3: Testing & Polish (PENDING)
9. 📋 **Error handling** - Handle invalid/missing session IDs gracefully
10. 📋 **Multi-tab testing** - Verify independent sessions work correctly
11. 📋 **Persistence testing** - Verify sessions survive page refreshes
12. 📋 **Session cleanup** - Clean up sessions on client disconnect

## Current Implementation Status

### ✅ What's Working Now
**Session Infrastructure:** Complete end-to-end session ID flow
- Browser tabs generate unique session IDs (e.g., `session_1672531200000_abc123def`)
- Session IDs persist across page refreshes via sessionStorage
- Frontend passes session IDs to backend in all WebSocket messages
- Backend extracts and tracks session IDs per client connection

### 📋 What's Not Started Yet
**Claude Session Management:** Session-aware command execution
- Need to extract Claude session IDs from stream-json output
- Need to implement `--resume` logic for subsequent messages

### 📋 What's Not Working Yet
**Actual Session Isolation:** Commands still share the same CLI process
- Multiple tabs still interfere with each other's conversations
- No session-specific command building implemented yet

## Testing Current Implementation

### Browser Console (F12 → Console)
```javascript
Browser session ID: session_1672531200000_abc123def
```
- Each tab should show different session ID
- Same tab should keep same ID after refresh

### Backend Logs
```
[WEBSOCKET] Client assigned browser session ID: session_1672531200000_abc123def
```
- Should appear when each tab sends its first message

### Browser Network Tab (F12 → Network → WS)
WebSocket messages should include sessionID:
```json
{
  "sender": "USER",
  "content": "hello",
  "sessionID": "session_1672531200000_abc123def"
}
```

## Current Problem
- All browser connections currently share the same CLI session state
- Multiple browser tabs interfere with each other's conversations
- Using basic `--resume` or `--continue` flags that only resume the "last" session
- No way to maintain separate conversation threads for different tasks

## Desired Behavior
Each browser tab/window should:
- Generate a unique session ID on first connection
- Create a new named CLI session on first message  
- Resume that specific session on subsequent messages
- Maintain session state across page refreshes and reconnections
- Work independently from other browser tabs

## Technical Solution

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