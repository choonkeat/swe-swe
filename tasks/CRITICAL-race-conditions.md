# CRITICAL: Race Condition Vulnerabilities

## Priority: 🔴 CRITICAL - Production Blocker

## Problem Statement

The codebase contains multiple severe race conditions that will cause data corruption, crashes, and undefined behavior under concurrent load. These issues are particularly severe because WebSocket servers handle multiple concurrent connections by design.

## Identified Race Conditions

### 1. Session History Corruption (websocket.go:319-328)

**Location:** `cmd/swe-swe/websocket.go:319-328`

**Current Code:**
```go
client.processMutex.Lock()
historyLen := len(client.claudeSessionHistory)
for i := historyLen - 1; i >= 0; i-- {
    sessionID := client.claudeSessionHistory[i]
    if sessionID != primarySessionID {
        sessionIDsToTry = append(sessionIDsToTry, sessionID)
    }
}
client.processMutex.Unlock()
// ... later in code ...
client.claudeSessionHistory = append(client.claudeSessionHistory, newSessionID) // RACE!
```

**Problem:**
- The mutex is released between reading and writing `claudeSessionHistory`
- Another goroutine can modify the slice between operations
- Can cause: index out of bounds, lost updates, corrupted history

**Impact:** Session recovery will fail randomly, users lose conversation context

### 2. Active Process Management Race (websocket.go:606-609, 792-815)

**Location:** Multiple locations in `executeAgentCommandWithSession`

**Current Code:**
```go
// Location 1: Setting active process
client.processMutex.Lock()
client.activeProcess = cmd
log.Printf("[PROCESS] Stored active process PID: %d", cmd.Process.Pid)
client.processMutex.Unlock()

// Location 2: Killing process (different goroutine)
client.processMutex.Lock()
processToKill := client.activeProcess
client.activeProcess = nil
client.processMutex.Unlock()
// RACE: activeProcess can be set to new value here!
if processToKill != nil {
    terminateProcess(processToKill) // May kill wrong process!
}
```

**Problem:**
- Process reference copied but then used outside mutex protection
- New process can be assigned between nil assignment and termination
- Can terminate wrong process or leave zombie processes

**Impact:** Wrong processes killed, resource leaks, system instability

### 3. Permission State Inconsistency (websocket.go:1019-1025)

**Location:** Permission response handler

**Current Code:**
```go
client.processMutex.Lock()
pendingTool := client.pendingToolPermission
client.allowedTools = clientMsg.AllowedTools
client.skipPermissions = clientMsg.SkipPermissions
client.pendingToolPermission = "" 
client.processMutex.Unlock()
// State is inconsistent if read during partial update
```

**Problem:**
- Multiple related fields updated non-atomically
- Other goroutines see partial updates
- Permission checks may use inconsistent state

**Impact:** Security bypass, incorrect permission enforcement

### 4. Tool Use Cache Corruption (websocket.go:71-73)

**Location:** ChatService struct

**Current Code:**
```go
type ChatService struct {
    toolUseCache map[string]ToolUseInfo // No synchronization!
    cacheMutex   sync.Mutex
}

// Usage without proper locking
svc.toolUseCache[toolID] = ToolUseInfo{...} // RACE!
```

**Problem:**
- Map accessed without mutex protection in some code paths
- Concurrent map writes cause panic in Go

**Impact:** Server crashes with "concurrent map writes" panic

### 5. Client Map Access (websocket.go)

**Location:** Broadcast operations

**Current Code:**
```go
func (svc *ChatService) BroadcastToSession(item ChatItem, sessionID string) {
    svc.mutex.Lock()
    defer svc.mutex.Unlock()
    for client := range svc.clients {
        if client.browserSessionID == sessionID {
            // RACE: client fields accessed outside mutex
            client.conn.Write(...) 
        }
    }
}
```

**Problem:**
- Client struct fields accessed after releasing service mutex
- Client can be modified/closed by another goroutine

**Impact:** Nil pointer dereferences, connection errors

## Root Causes

1. **Improper Mutex Scope**: Locks released too early before operations complete
2. **Missing Synchronization**: Some shared data has no protection
3. **Lock Ordering Issues**: Potential for deadlocks with nested locks
4. **Value vs Reference Semantics**: Copying pointers doesn't protect the underlying data

## Fix Requirements

### Immediate Fixes (Phase 1)

1. **Session History Protection**
```go
// Add dedicated mutex for session operations
type Client struct {
    // ... existing fields ...
    sessionMutex sync.RWMutex // Separate mutex for session data
}

// Use read lock for queries
func (c *Client) GetSessionHistory() []string {
    c.sessionMutex.RLock()
    defer c.sessionMutex.RUnlock()
    return append([]string{}, c.claudeSessionHistory...) // Return copy
}

// Use write lock for modifications
func (c *Client) AddSessionToHistory(sessionID string) {
    c.sessionMutex.Lock()
    defer c.sessionMutex.Unlock()
    c.claudeSessionHistory = append(c.claudeSessionHistory, sessionID)
}
```

2. **Process Lifecycle Management**
```go
// Atomic process operations
func (c *Client) SetActiveProcess(cmd *exec.Cmd) {
    c.processMutex.Lock()
    defer c.processMutex.Unlock()
    c.activeProcess = cmd
}

func (c *Client) TerminateActiveProcess() error {
    c.processMutex.Lock()
    defer c.processMutex.Unlock()
    
    if c.activeProcess == nil {
        return nil
    }
    
    err := terminateProcess(c.activeProcess)
    c.activeProcess = nil
    return err
}
```

3. **Permission State Atomicity**
```go
// Use atomic state transitions
type PermissionState struct {
    AllowedTools    []string
    SkipPermissions bool
    PendingTool     string
}

func (c *Client) UpdatePermissions(state PermissionState) {
    c.processMutex.Lock()
    defer c.processMutex.Unlock()
    
    c.allowedTools = state.AllowedTools
    c.skipPermissions = state.SkipPermissions
    c.pendingToolPermission = state.PendingTool
}
```

### Long-term Fixes (Phase 2)

1. **Implement Channel-Based Architecture**
```go
// Replace shared state with channels
type Client struct {
    commands   chan Command
    responses  chan Response
    state      *ClientState // Accessed only by single goroutine
}

// Single goroutine owns state
func (c *Client) stateManager() {
    for cmd := range c.commands {
        // All state modifications here
        c.state.apply(cmd)
        c.responses <- c.state.process(cmd)
    }
}
```

2. **Add Race Detection to CI**
```bash
# Add to Makefile
test-race:
    go test -race ./...
    
build-race:
    go build -race -o bin/swe-swe-race ./cmd/swe-swe
```

3. **Implement Proper Context Cancellation**
```go
func executeWithTimeout(ctx context.Context, timeout time.Duration) {
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()
    
    // All operations respect context
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
        // Do work
    }
}
```

## Testing Requirements

### Unit Tests
```go
func TestConcurrentSessionHistory(t *testing.T) {
    client := &Client{}
    var wg sync.WaitGroup
    
    // Spawn 100 goroutines modifying history
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            client.AddSessionToHistory(fmt.Sprintf("session-%d", id))
        }(i)
    }
    
    wg.Wait()
    
    // Verify all sessions recorded
    if len(client.GetSessionHistory()) != 100 {
        t.Errorf("Lost session updates due to race condition")
    }
}
```

### Integration Tests
- Use Go's race detector: `go test -race`
- Load test with multiple concurrent WebSocket connections
- Chaos testing with random disconnections/reconnections

## Validation

### Success Criteria
1. ✅ No race detector warnings in any test
2. ✅ Load test with 100+ concurrent connections passes
3. ✅ No panic from concurrent map writes
4. ✅ Process management is deterministic
5. ✅ Session history never corrupted

### Performance Metrics
- Measure lock contention with pprof
- Ensure < 5% performance degradation after fixes
- Monitor goroutine count for leaks

## Implementation Priority

1. **Week 1**: Fix concurrent map writes (prevents crashes)
2. **Week 1**: Fix process management races (prevents wrong process kills)
3. **Week 2**: Fix session history races (prevents data loss)
4. **Week 2**: Fix permission state races (prevents security issues)
5. **Week 3**: Implement channel-based refactoring
6. **Week 4**: Add comprehensive race testing

## Risk Assessment

**Current Risk Level:** 🔴 CRITICAL
- **Probability:** 100% - These races WILL occur under load
- **Impact:** Data corruption, crashes, security vulnerabilities
- **Mitigation:** Must fix before production deployment

## Notes

- Do NOT attempt to fix races by adding delays/sleeps
- Each fix must be validated with race detector
- Consider using sync/atomic for simple counters
- Document all synchronization invariants in code comments
- Review https://golang.org/doc/articles/race_detector.html

## References

- [Go Race Detector](https://golang.org/doc/articles/race_detector.html)
- [Effective Go - Concurrency](https://golang.org/doc/effective_go.html#concurrency)
- [Go Memory Model](https://golang.org/ref/mem)