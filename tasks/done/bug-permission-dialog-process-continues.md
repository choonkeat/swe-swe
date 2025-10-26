# Bug: Process Continues Running During Permission Dialog

## Problem Description
When Claude requests file write permissions and the permission dialog appears, the underlying process continues to execute in the background. This creates several issues:

### Observed Behavior
From the screenshot `permission-but-process-still-runs.png`:
1. Claude attempts to edit a file at `/Users/choonkeatchew/git/choonkeat/swe-swe/tasks/session-persistence-robustness-concern.md`
2. A permission dialog appears asking for write access
3. The dialog shows the full error context including the attempted file operations
4. **Critical Issue**: While the permission dialog is displayed, Claude continues processing and shows "I'll update the task document to emphasize the importance for mobile clients"
5. The "Stop" button remains active, indicating the process is still running

### Impact
1. **Confusing UX**: The user sees Claude continuing to work while being asked for permission, creating uncertainty about what's happening
2. **Wasted Computation**: Claude may continue processing and generating responses that will fail or need to be re-done after permission is granted/denied
3. **State Inconsistency**: The system may be in an inconsistent state where Claude thinks it has completed tasks that actually failed due to permissions
4. **Mobile Impact**: Particularly problematic for mobile users who may have slower response times to permission dialogs due to:
   - Screen size constraints making dialogs harder to notice
   - Context switching between apps
   - Network latency issues

## Priority
**High** - This affects core functionality and user experience, especially for mobile users who are more likely to encounter permission issues and have slower response times.

## Root Cause Analysis

### The Race Condition
The issue is a **race condition** between Claude's output generation and permission error detection:

1. Claude attempts a tool use (e.g., Edit) that requires permission
2. The tool returns an error immediately
3. Claude receives the error and starts generating a response (e.g., "I'll update the task document...")
4. This response is already in the output pipeline/buffer
5. The system detects the permission error and sends the permission dialog
6. Claude's buffered response gets displayed BEFORE the process is fully terminated
7. The `cancel()` and `cmd.Wait()` are called, but the damage is done

### Why This Happens
- The permission check happens AFTER the tool result is received
- Claude immediately starts generating a response to the tool error
- The JSON streaming means messages are sent as soon as they're generated
- There's no mechanism to prevent buffered messages from being sent once a permission error is detected

### Evidence of Multiple Processes
After granting permission and the process supposedly completing, messages CONTINUE arriving from a DIFFERENT Claude session that wasn't properly terminated:

```
Line 518: [EXEC] Process completed successfully 
Line 519: [SESSION] Successfully resumed with session ID: dad078c6-f419-4cac-be2b-4b1d423f3194
Lines 521-547: MORE MESSAGES from session 36581b64-73d1-429a-bb0e-f95179dfc7b4!
```

This reveals that permission error cancellation only stops ONE process, but there are MULTIPLE Claude processes running from retry attempts.

## Current Implementation Status

### Already Implemented ✅
1. **Process Tracking**: Added `activeProcess *exec.Cmd` field to Client struct
2. **Graceful Termination Function**: `terminateProcess()` function that:
   - Sends interrupt signal first
   - Waits 2 seconds for graceful shutdown
   - Force kills if necessary
3. **Basic Process Management**: Clears activeProcess references appropriately
4. **Simplified Retry Message**: Changed from verbose message to just "continue"

### Still Problematic ❌
The current implementation still has the race condition because it:
```go
// Current problematic flow (websocket.go:707-719)
log.Printf("[EXEC] Permission error detected, terminating process")
cancel()  // Cancel context
// PROBLEM: Sending permission request while process still running!
svc.BroadcastToSession(permissionRequest, client.browserSessionID)
cmd.Wait()
```

The permission dialog is sent WHILE the process is still potentially generating output.

## The Solution: Terminate-First Approach

### Core Principle
**Terminate the process COMPLETELY before sending the permission dialog.**

This eliminates all race conditions by ensuring Claude cannot generate any messages while the permission dialog is displayed.

### Implementation

```go
// Correct implementation
log.Printf("[EXEC] Permission error detected, terminating process")

// FIRST: Ensure process is completely dead
terminateProcess(cmd)  // Blocks for up to 2s, guarantees termination
client.activeProcess = nil

// THEN: Send permission request (process is guaranteed dead)
svc.BroadcastToSession(permissionRequest, client.browserSessionID)
// No need for cmd.Wait() - terminateProcess already handled it
```

### Why This Works

1. **Eliminates Race Conditions**: No possibility of Claude generating messages during permission dialog
2. **Simple Implementation**: No complex buffering or filtering needed
3. **Deterministic Behavior**: Same result every time
4. **Guaranteed Cleanup**: Process is dead before user sees dialog

### Tradeoffs (Acceptable)

1. **Session Loss**: The killed Claude session can't be resumed with `--resume`
   - **Mitigation**: Store and replay the last user message after permission granted

2. **Performance**: Fresh session start requires re-sending conversation history
   - **Mitigation**: This only happens on permission errors (should be rare)

## Enhanced Implementation with Context Preservation

To minimize the impact of session loss:

```go
type Client struct {
    // ... existing fields
    lastKilledSessionID string  // Track killed session to avoid reuse
    lastUserMessage     string  // Save message for replay after permission
}

// On permission error:
func handlePermissionError(client *Client, cmd *exec.Cmd) {
    // Save context for recovery
    client.lastKilledSessionID = client.claudeSessionID
    client.lastUserMessage = getCurrentUserMessage()
    
    // Terminate process completely
    terminateProcess(cmd)
    client.activeProcess = nil
    
    // Now safe to send permission dialog
    sendPermissionDialog(client)
}

// After permission granted:
func handlePermissionGranted(client *Client) {
    // Start fresh, don't use killed session
    // Replay the original user message
    tryExecuteWithSessionHistory(
        ctx, svc, client,
        client.lastUserMessage,  // Replay original request
        true,                     // Force fresh session
        allowedTools,
        skipPermissions,
        ""                       // Don't use killed session ID
    )
}
```

## Implementation Status: COMPLETED ✅

### Changes Made (2025-10-26)

1. **Modified permission error handling** (`websocket.go:707-731`)
   - Now calls `terminateProcess()` BEFORE sending permission dialog
   - Stores active process reference and clears it atomically
   - Process is guaranteed dead before dialog is shown

2. **Added context preservation fields to Client struct** (`websocket.go:36-37`)
   - Added `lastKilledSessionID` to track killed sessions
   - Added `lastUserMessage` to save messages for replay

3. **Store context before terminating process** (`websocket.go:705-709`)
   - Saves killed session ID when permission error occurs
   - Saves user message when received (`websocket.go:1017-1020`)

4. **Modified permission granted handler** (`websocket.go:990-1009`)
   - Replays last user message with fresh session after permission granted
   - Avoids reusing killed session IDs
   - Falls back to "continue" if no saved message

5. **Testing**
   - ✅ All tests pass (`make test`)
   - ✅ Build successful (`make build`)

## Implementation Checklist

- [x] Modify permission error handling in `websocket.go:707-719` to call `terminateProcess()` BEFORE sending permission dialog
- [x] Add `lastKilledSessionID` and `lastUserMessage` fields to Client struct
- [x] Store context before terminating process on permission error
- [x] Modify permission granted handler to replay last message with fresh session
- [x] Test with various permission-requiring tools (Edit, Write, etc.)
- [ ] Test with rapid tool uses that trigger permissions (manual testing needed)
- [ ] Verify no zombie processes remain after permission dialogs (manual testing needed)
- [ ] Test mobile client behavior with slower response times (manual testing needed)

## Success Metrics

- **Zero zombie processes**: No Claude processes running after permission dialogs
- **Clean UX**: No confusing messages during permission dialogs  
- **Predictable behavior**: Same result every time
- **Session recovery**: Successfully replays user message after permission granted

## Mobile Considerations

Mobile clients face unique challenges:
1. **Slower Permission Response**: Users take longer to notice and respond to dialogs
2. **Session Fragility**: Mobile sessions are already prone to disconnections
3. **Resource Constraints**: Zombie processes are especially problematic on mobile

The terminate-first approach helps mobile users by:
- Preventing resource exhaustion from zombie processes
- Providing clean, predictable behavior
- Avoiding confusing partial responses

## Conclusion

The terminate-first approach solves the core problem by eliminating the race condition entirely. While it results in session loss, this is an acceptable tradeoff for:

- Complete elimination of race conditions
- Much simpler implementation
- Guaranteed resource cleanup
- Predictable, deterministic behavior

This should be implemented immediately as a single change to the permission error handling flow.