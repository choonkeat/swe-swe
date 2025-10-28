# Bug: WebSocket Broadcast Session Synchronization

## Issue Description
WebSocket broadcast functionality does not properly synchronize messages across browser tabs sharing the same Claude session ID. Each browser tab creates its own unique `browserSessionID`, preventing message synchronization even when they share the same Claude conversation session.

## Root Cause Analysis

### Current Implementation Problems

#### 1. Browser Session ID Generation (`index.html.tmpl:26-38`)
```javascript
// Problem: Always generates fresh session ID per tab
function generateSessionID() {
    return 'session_' + Date.now() + '_' + Math.random().toString(36).substr(2, 9);
}

let browserSessionID = generateSessionID(); // Always unique per tab
```

**Issue**: Each browser tab gets a unique `browserSessionID`, making it impossible for tabs to share messages even when viewing the same Claude session.

#### 2. BroadcastToSession Logic (`websocket.go:275-295`)
```go
func (s *ChatService) BroadcastToSession(item ChatItem, sessionID string) {
    // ...
    for client := range s.clients {
        if client.browserSessionID == sessionID { // Only matches exact browser session
            if err := websocket.JSON.Send(client.conn, item); err != nil {
                // ...
            }
        }
    }
}
```

**Issue**: Broadcasting uses `browserSessionID` for matching, but doesn't consider Claude session synchronization.

#### 3. Session Assignment Logic (`websocket.go:1032-1045`)
```go
// Browser session is set once and never updated
if clientMsg.SessionID != "" && client.browserSessionID == "" {
    client.browserSessionID = clientMsg.SessionID
}

// Claude session ID is separate
if clientMsg.ClaudeSessionID != "" && client.claudeSessionID == "" {
    client.claudeSessionID = clientMsg.ClaudeSessionID
}
```

**Issue**: No mechanism to group browser sessions by Claude session ID.

### Current Behavior (Detailed Session Flow)
1. **Tab A** opens `http://localhost:7000/#claude=session123`:
   - Gets `browserSessionID = session_1234_abc`
   - Sets `claudeSessionID = "session123"` 
2. **Tab B** opens `http://localhost:7000/#claude=session123`:
   - Gets `browserSessionID = session_5678_def`
   - Sets `claudeSessionID = "session123"`
3. **Tab A sends message**: Claude CLI may return different session ID (e.g., `session456`)
   - Tab A's `claudeSessionID` updates to `"session456"` (websocket.go:814-827)
   - Tab B still has `claudeSessionID = "session123"`
4. **AI response goes to Tab A only** → Tab B doesn't see the message
5. **Tab B is isolated** despite originally sharing the same URL session

### Critical Session Management Issue

**The core problem**: Cross-tab session synchronization gap when session IDs change:

```go
// Current session update logic (websocket.go:1074-1081) works correctly for individual tabs:
} else if client.claudeSessionID != clientMsg.ClaudeSessionID {
    // Handle dynamic Claude session changes - THIS WORKS!
    oldSessionID := client.claudeSessionID
    client.claudeSessionID = clientMsg.ClaudeSessionID  // Updates correctly
    client.claudeSessionHistory = append(client.claudeSessionHistory, clientMsg.ClaudeSessionID)
}
```

**But the issue is cross-tab isolation:**
- **Tab A** sends message → gets session update from `"session123"` to `"session456"`
- **Tab B** never sends message → never learns about the session change
- **Result**: Tab A has `"session456"`, Tab B still has `"session123"`
- **No mechanism exists** to propagate session changes across tabs sharing the same initial URL session

### Expected Behavior
1. Both tabs should receive the same messages when they share a Claude session ID
2. If Claude session ID changes in URL, all tabs should follow the new session
3. New tabs joining an existing Claude session should see live updates

### Critical Discovery: Claude Session ID Communication Gap

**Key insight**: When a new tab opens with the same Claude session ID from the URL fragment:

1. **Browser correctly extracts Claude session ID** from URL fragment (`#claude=session123`)
2. **Browser sends Claude session ID** to server in WebSocket message (`claudeSessionID: "session123"`)
3. **Server receives and stores** the Claude session ID correctly (`client.claudeSessionID = clientMsg.ClaudeSessionID`)

**But the critical gap is**:
- **If the tab doesn't send a user message**, the server never learns that this client belongs to that Claude session
- **The browser only sends `claudeSessionID` when sending user messages** (elm/src/Main.elm:376)
- **Silent tabs (that just opened but haven't sent messages) are invisible** to the session broadcasting system

**Current behavior**:
```
Tab A: Opens #claude=session123 → Sends message → Server knows claudeSessionID="session123"
Tab B: Opens #claude=session123 → Silent (no message) → Server claudeSessionID="" 
Tab A: Gets AI response → Only Tab B receives it (Tab B unknown to session)
```

**The fix requires**:
1. **Immediate session announcement**: Browser should send Claude session ID immediately on WebSocket connection
2. **Connect message with session info**: Send Claude session ID in connection handshake, not just user messages
3. **Server tracks silent clients**: Server should track all clients by Claude session ID, even if they haven't sent messages

## Technical Solution

### Option 1: Claude Session-Based Broadcasting (Recommended)

**Concept**: Use Claude session ID for message synchronization instead of browser session ID.

#### Changes Required:

##### 1. Enhanced Broadcast Functions (`websocket.go`)
```go
// BroadcastToClaudeSession sends messages to all clients sharing a Claude session
func (s *ChatService) BroadcastToClaudeSession(item ChatItem, claudeSessionID string) {
    if claudeSessionID == "" {
        return
    }
    
    s.mutex.Lock()
    defer s.mutex.Unlock()
    
    broadcastCount := 0
    for client := range s.clients {
        if client.claudeSessionID == claudeSessionID {
            if err := websocket.JSON.Send(client.conn, item); err != nil {
                log.Printf("Error sending to client in Claude session %s: %v", claudeSessionID, err)
                delete(s.clients, client)
                client.conn.Close()
            } else {
                broadcastCount++
            }
        }
    }
    
    log.Printf("[BROADCAST] Sent message to %d clients in Claude session: %s", broadcastCount, claudeSessionID)
}

// BroadcastToURLSession sends messages to all clients that originally shared a URL session ID
// CRITICAL: This handles cross-tab session synchronization when actual Claude session changes
func (s *ChatService) BroadcastToURLSession(item ChatItem, originalURLSessionID string) {
    if originalURLSessionID == "" {
        return
    }
    
    s.mutex.Lock()
    defer s.mutex.Unlock()
    
    broadcastCount := 0
    for client := range s.clients {
        // Check if client has this session in their history (indicating they started with this URL session)
        for _, historicalSession := range client.claudeSessionHistory {
            if historicalSession == originalURLSessionID {
                if err := websocket.JSON.Send(client.conn, item); err != nil {
                    log.Printf("Error sending to client with URL session %s: %v", originalURLSessionID, err)
                    delete(s.clients, client)
                    client.conn.Close()
                } else {
                    broadcastCount++
                }
                break // Only send once per client
            }
        }
    }
    
    log.Printf("[BROADCAST] Sent message to %d clients sharing URL session: %s", broadcastCount, originalURLSessionID)
}
```

##### 2. Update All Broadcast Calls
Replace `BroadcastToSession(item, client.browserSessionID)` with:
- `BroadcastToClaudeSession(item, client.claudeSessionID)` for messages that should be synchronized
- Keep `BroadcastToSession` for client-specific messages (like permission dialogs)

##### 3. Claude Session Updates (`websocket.go`)
```go
// Handle dynamic Claude session changes
if clientMsg.ClaudeSessionID != "" && client.claudeSessionID != clientMsg.ClaudeSessionID {
    oldSessionID := client.claudeSessionID
    client.claudeSessionID = clientMsg.ClaudeSessionID
    // Add to history
    client.claudeSessionHistory = append(client.claudeSessionHistory, clientMsg.ClaudeSessionID)
    log.Printf("[SESSION] Client switched from Claude session %s to %s", oldSessionID, client.claudeSessionID)
}
```

##### 4. Client Connection Message
Add session sync info when clients connect:
```go
// Send current session state to new clients joining existing Claude session
if client.claudeSessionID != "" {
    // Optionally send recent chat history or session state
}
```

### Option 2: Hybrid Approach

Use Claude session ID for AI message broadcasts, browser session ID for client-specific messages:

```go
type BroadcastMode int
const (
    BroadcastToBrowser BroadcastMode = iota  // Permission dialogs, etc.
    BroadcastToClaude                        // AI responses, tool results
)

func (s *ChatService) SmartBroadcast(item ChatItem, client *Client, mode BroadcastMode) {
    switch mode {
    case BroadcastToBrowser:
        s.BroadcastToSession(item, client.browserSessionID)
    case BroadcastToClaude:
        s.BroadcastToClaudeSession(item, client.claudeSessionID)
    }
}
```

## Implementation Plan

### Phase 1: Core Broadcasting Infrastructure
1. **Add `BroadcastToClaudeSession` function** (`websocket.go`)
2. **Add session switching support** for dynamic Claude session changes
3. **Add comprehensive logging** for session management and broadcasting

### Phase 2: Update Message Broadcasting
1. **Audit all `BroadcastToSession` calls** and categorize them:
   - AI responses → Convert to `BroadcastToClaudeSession`
   - Tool results → Convert to `BroadcastToClaudeSession` 
   - Permission dialogs → Keep as `BroadcastToSession`
   - Client-specific errors → Keep as `BroadcastToSession`

### Phase 3: Testing and Validation
1. **Manual testing** with multiple browser tabs
2. **Playwright tests** for multi-tab synchronization
3. **Edge case testing** (session switches, reconnections)

## Playwright Test Strategy

### Test Case 1: Basic Multi-Tab Synchronization
```javascript
test('Multi-tab session synchronization', async ({ context }) => {
    // Open two tabs with same Claude session ID
    const page1 = await context.newPage();
    const page2 = await context.newPage();
    
    await page1.goto('http://localhost:7000/#claude=test-session-123');
    await page2.goto('http://localhost:7000/#claude=test-session-123');
    
    // Send message from tab 1
    await sendMessage(page1, 'Create a test file');
    
    // Verify both tabs receive the response
    await expect(page1.locator('.ai-response')).toBeVisible();
    await expect(page2.locator('.ai-response')).toBeVisible();
    
    // Verify response content is identical
    const response1 = await page1.locator('.ai-response').textContent();
    const response2 = await page2.locator('.ai-response').textContent();
    expect(response1).toBe(response2);
});
```

### Test Case 2: Session Switching
```javascript
test('Dynamic session switching across tabs', async ({ context }) => {
    const page1 = await context.newPage();
    const page2 = await context.newPage();
    
    // Start with same session
    await page1.goto('http://localhost:7000/#claude=session-A');
    await page2.goto('http://localhost:7000/#claude=session-A');
    
    // Switch page2 to different session
    await page2.goto('http://localhost:7000/#claude=session-B');
    
    // Send message from page1 - should only affect session-A
    await sendMessage(page1, 'Test message for session A');
    
    // Page1 should receive response, page2 should not
    await expect(page1.locator('.ai-response')).toBeVisible();
    await expect(page2.locator('.ai-response')).not.toBeVisible();
});
```

### Test Case 3: New Tab Joining Existing Session
```javascript
test('New tab joins existing active session', async ({ context }) => {
    const page1 = await context.newPage();
    await page1.goto('http://localhost:7000/#claude=active-session');
    
    // Start a conversation
    await sendMessage(page1, 'Start conversation');
    await waitForResponse(page1);
    
    // Open second tab with same session
    const page2 = await context.newPage();
    await page2.goto('http://localhost:7000/#claude=active-session');
    
    // Continue conversation from page1
    await sendMessage(page1, 'Continue conversation');
    
    // Both tabs should receive the new message
    await expect(page1.locator('.ai-response').last()).toBeVisible();
    await expect(page2.locator('.ai-response').last()).toBeVisible();
});
```

## Implementation Steps with Testing

### Step 1: Add Broadcasting Infrastructure
```bash
# 1. Add BroadcastToClaudeSession function
# 2. Add logging for session management
make test  # Ensure existing functionality works
```

### Step 2: Convert AI Response Broadcasting  
```bash
# 1. Update tool result broadcasts
# 2. Update AI message broadcasts
make test  # Verify no regressions
```

### Step 3: Add Multi-Tab Tests
```bash
# 1. Add Playwright multi-tab tests
# 2. Test session synchronization
make test-playwright  # New tests should pass
```

### Step 4: Handle Session Switching
```bash
# 1. Add dynamic session change handling
# 2. Test session switches
make test  # Full test suite should pass
```

## Success Criteria

- [ ] Multiple tabs with same Claude session ID receive synchronized messages
- [ ] Permission dialogs remain tab-specific (not synchronized)
- [ ] Dynamic session switching works without reconnection required
- [ ] New tabs joining existing sessions see live updates
- [ ] All existing functionality preserved (no regressions)
- [ ] Comprehensive test coverage for multi-tab scenarios

## Files to Modify

### Core Implementation
- `cmd/swe-swe/websocket.go` - Add broadcasting logic and session management
- `tests/playwright/specs/multi-tab-sync.spec.ts` - New test file for multi-tab scenarios

### Testing Infrastructure  
- Update existing permission tests to ensure they remain tab-specific
- Add session switching test scenarios

## Priority
**High** - Significantly improves multi-tab user experience and enables collaborative workflows