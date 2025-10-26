# Task: Implement Robust Session Persistence and Recovery

## Priority: HIGH 🔴

## Problem Statement
The application lacks robust session persistence and recovery mechanisms, causing critical failures when:
- Claude CLI cannot find a session ID (returns "No conversation found with session ID")
- Browser reconnects after network interruption
- WebSocket connections drop and reconnect
- Users reload the browser window
- Mobile clients experience frequent disconnections (sleep/wake, network switches, backgrounding)

**This is CRITICAL for mobile users** who experience frequent disconnections due to:
- Device sleep/wake cycles
- Unstable network conditions (especially on cellular)
- App backgrounding/foregrounding
- Network switching (WiFi ↔ cellular)
- Browser memory pressure causing reloads

## Observed Production Issue (2025/10/25)

### Session Recovery Failure
**What happened:**
1. Client tried to resume with session ID: `81407d78-753d-45ec-9783-bb3c0a8ade0a`
2. Server executed: `claude --resume 81407d78-753d-45ec-9783-bb3c0a8ade0a`
3. Claude CLI returned error: "No conversation found with session ID"
4. Process exited with status 1
5. **Browser client was stuck with no recovery mechanism**
6. User had to manually refresh browser, losing all context

**Evidence:** See `logs.txt` lines 28-41 for the actual failure sequence

## Current Implementation Problems

### 1. No Session Validation
- Server blindly attempts to resume sessions without checking if they exist
- No pre-validation before spawning Claude process
- No fallback when session is invalid

### 2. Poor Error Recovery
- When session resume fails, browser gets stuck
- No automatic retry mechanism
- No user-friendly error messages
- No option to start fresh session while preserving user input

### 3. Incomplete State Synchronization
- Session state not properly synchronized between server and client
- No mechanism to detect stale session IDs
- No cleanup of invalid sessions

## Implementation Requirements

### Phase 1: Session Validation & Recovery (CRITICAL)

#### 1.1 Session Validation Before Resume
**Files to modify:** `cmd/swe-swe/main.go`

```go
// Add session validation before attempting resume
func validateSession(sessionID string) bool {
    // Check if session exists in Claude
    // Can use `claude list` or similar command
    // Cache valid sessions for performance
}

// Modify connection handler to validate first
if sessionID != "" {
    if !validateSession(sessionID) {
        // Send error to client with recovery options
        // Don't spawn process yet
    }
}
```

#### 1.2 Automatic Fallback Mechanism
**Files to modify:** `cmd/swe-swe/main.go`, `cmd/swe-swe/index.html.tmpl`

- If session resume fails, automatically create new session
- Preserve user's pending input/context
- Send clear message to user about what happened
- Option to retry with same session ID (in case of temporary failure)

#### 1.3 Client-Side Recovery UI
**Files to modify:** `elm/src/Main.elm`, `cmd/swe-swe/index.html.tmpl`

- Add error state handling for session failures
- Show clear error message: "Previous session not found. Starting fresh session..."
- Add "Retry" and "Start New" buttons
- Preserve any unsent messages during recovery

### Phase 2: Connection Resilience

#### 2.1 Enhanced Reconnection Logic
**Files to modify:** `cmd/swe-swe/index.html.tmpl`

```javascript
// Improve WebSocket reconnection with exponential backoff
let reconnectAttempts = 0;
const maxReconnectAttempts = 10;
const baseDelay = 1000;

function reconnectWebSocket() {
    if (reconnectAttempts >= maxReconnectAttempts) {
        showPermanentFailureUI();
        return;
    }
    
    const delay = Math.min(baseDelay * Math.pow(2, reconnectAttempts), 30000);
    reconnectAttempts++;
    
    setTimeout(() => {
        connectWebSocket();
    }, delay);
}

// Add connection state tracking
let connectionState = 'disconnected'; // 'connecting', 'connected', 'reconnecting'
```

#### 2.2 Message Queue Persistence
**Files to modify:** `cmd/swe-swe/index.html.tmpl`

- Persist message queue to localStorage
- Restore and retry queued messages after reconnection
- Add timeout for old queued messages
- Show queue status to user

### Phase 3: Session State Management

#### 3.1 Server-Side Session Registry
**Files to modify:** `cmd/swe-swe/main.go`

```go
type SessionInfo struct {
    ID          string
    Created     time.Time
    LastActive  time.Time
    Status      string // "active", "disconnected", "expired"
    ProcessPID  int
}

var sessionRegistry = make(map[string]*SessionInfo)
var sessionMutex sync.RWMutex

// Add session lifecycle methods
func registerSession(id string) { /* ... */ }
func updateSessionActivity(id string) { /* ... */ }
func markSessionDisconnected(id string) { /* ... */ }
func cleanupExpiredSessions() { /* ... */ }
```

#### 3.2 Session Persistence Layer
**New file:** `cmd/swe-swe/session_store.go`

- Implement file-based or SQLite session storage
- Persist session metadata between server restarts
- Track session history and recovery attempts
- Implement session garbage collection

### Phase 4: Mobile-Specific Optimizations

#### 4.1 Aggressive Keep-Alive
**Files to modify:** `cmd/swe-swe/index.html.tmpl`, `cmd/swe-swe/main.go`

- Implement ping/pong at 30-second intervals
- Detect connection loss within 1 minute
- Start reconnection immediately on detection

#### 4.2 State Preservation
**Files to modify:** `elm/src/Main.elm`, `cmd/swe-swe/index.html.tmpl`

- Save conversation state to localStorage frequently
- Restore UI state after reconnection
- Preserve scroll position and input text

## Testing Requirements

### Manual Testing Scenarios
1. **Session Not Found:**
   - Delete Claude session manually
   - Try to reconnect with old session ID
   - Verify graceful recovery

2. **Network Interruption:**
   - Disable network for 30 seconds
   - Re-enable and verify reconnection
   - Check message queue preserved

3. **Browser Reload:**
   - Reload at various points in conversation
   - Verify session resumes correctly
   - Check no messages lost

4. **Server Restart:**
   - Stop server mid-conversation
   - Restart server
   - Verify client reconnects and resumes

5. **Mobile Simulation:**
   - Use Chrome DevTools network throttling
   - Simulate offline/online transitions
   - Test with slow 3G profile

### Automated Testing
- Add integration tests for session recovery
- Test WebSocket reconnection logic
- Verify message queue persistence

## Files to Examine and Modify

### Critical Files:
1. **`cmd/swe-swe/main.go`** - Main server and WebSocket handler
2. **`cmd/swe-swe/index.html.tmpl`** - Client-side WebSocket and session logic
3. **`elm/src/Main.elm`** - Elm application state management

### Supporting Files:
4. **`cmd/swe-swe/static/css/styles.css`** - Add styles for error states
5. **`Makefile`** - May need test targets
6. **New:** `cmd/swe-swe/session_store.go` - Session persistence layer

## Success Criteria

### Must Have:
- ✅ Session validation before resume attempt
- ✅ Automatic fallback to new session when resume fails
- ✅ Clear error messages to user
- ✅ Message queue preservation during disconnection
- ✅ WebSocket auto-reconnection with backoff
- ✅ Session state persistence across server restarts

### Should Have:
- ✅ Session registry with lifecycle management
- ✅ Mobile-optimized keep-alive
- ✅ Connection state UI indicators
- ✅ Retry mechanism for failed operations

### Nice to Have:
- ✅ Session history tracking
- ✅ Metrics/logging for debugging
- ✅ Configuration for session timeout
- ✅ Multi-tab session coordination

## Implementation Order

1. **Day 1:** Implement session validation and basic recovery (Phase 1)
2. **Day 2:** Add reconnection improvements (Phase 2.1-2.2)
3. **Day 3:** Build session registry and persistence (Phase 3)
4. **Day 4:** Add mobile optimizations (Phase 4)
5. **Day 5:** Testing and refinement

## Risk Mitigation

- **Backward Compatibility:** Ensure old sessions still work
- **Performance:** Cache session validations to avoid repeated checks
- **Security:** Don't expose session details in error messages
- **User Experience:** Always provide clear feedback during issues

## Notes for Implementation

- Start with Phase 1 as it addresses the critical production issue
- Use existing logging infrastructure for debugging
- Consider adding feature flags for gradual rollout
- Document all new message types and state transitions
- Add comments explaining mobile-specific considerations

## References

- Current failure logs: See `logs.txt` lines 28-41
- Claude CLI documentation: Check `claude --help` for session commands
- WebSocket protocol: Follow RFC 6455 for proper connection handling
- Mobile best practices: Implement recommendations from Google's PWA guidelines