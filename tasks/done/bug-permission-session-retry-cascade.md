# Bug: Permission Error Triggers Session Retry Cascade with Multiple Active Processes

## Problem Description
When a permission error occurs (e.g., Edit tool requires approval), the system terminates the current process correctly but then automatically retries with older session IDs from history. This creates a cascade of new Claude processes that may continue running even after the permission dialog is handled, leading to multiple active sessions and confusing behavior.

## Observed Behavior

### From Screenshots (permission-1.png through permission-3.png):
1. Claude attempts to write to `/Users/choonkeatchew/git/choonkeat/swe-swe/tasks/bug-permission-granted-full-restart.md`
2. Permission dialog appears correctly
3. After user grants permission, Claude launches a Task agent
4. The Task agent reads the file and completes successfully
5. Multiple permission dialogs appear for the same operation

### From Screenshot (permission-but-continue.png):
Shows extensive output continuing while permission dialog is displayed, indicating process is still active.

### From Logs Analysis:
```
2025/10/26 15:43:34 [EXEC] Permission error detected, terminating process completely
2025/10/26 15:43:34 [PROCESS] Attempting graceful termination of process PID: 94461
2025/10/26 15:43:34 [PROCESS] Process terminated gracefully
2025/10/26 15:43:34 [SESSION] Retrying with older session ID from history (attempt 2/3): 914b00d7-480a-4f3d-a633-66bce2bd0d0a
2025/10/26 15:43:34 [WEBSOCKET] Terminating existing agent process before starting new one
2025/10/26 15:43:35 [SESSION] Using --resume with Claude session ID: 914b00d7-480a-4f3d-a633-66bce2bd0d0a
```

Then later:
```
2025/10/26 15:45:07 [EXEC] Permission error detected, terminating process completely
2025/10/26 15:45:07 [PROCESS] Process terminated gracefully
2025/10/26 15:45:07 [SESSION] Retrying with older session ID from history (attempt 3/3): c25699d0-da2d-41c7-b93a-fd73486ee059
```

## Root Cause Analysis

### The Session Retry Cascade Problem

The system implements a retry mechanism when Claude commands fail, which includes:
1. Maintaining a history of session IDs
2. On failure, retrying with older session IDs (up to 3 attempts)
3. Each retry spawns a new Claude process

When a permission error occurs:
1. Process is terminated correctly via `terminateProcess()`
2. But the retry logic kicks in automatically
3. New process starts with older session ID
4. If that also hits a permission error, another retry occurs
5. Result: Multiple Claude processes may be spawned

### Evidence of Multiple Sessions
From the logs, we can see session ID changes:
- Initial: `26443d90-7fa3-4533-874b-927e3aa1a771`
- Retry 2: `914b00d7-480a-4f3d-a633-66bce2bd0d0a` → New: `1e72a1a6-76da-4a10-a864-335ecc2121b9`
- Retry 3: `c25699d0-da2d-41c7-b93a-fd73486ee059` → New: `bdf4636b-44f8-46c7-bce2-19235cd0494e`

Each retry creates a new process, and if not properly terminated, these accumulate.

## Why This is Different from Previously Fixed Bugs

### bug-permission-detection.md (FIXED)
- Fixed: Recognition of "This command requires approval" as permission error
- Not addressed: Session retry behavior on permission errors

### bug-permission-dialog-process-continues.md (FIXED)  
- Fixed: Process termination before showing permission dialog
- Fixed: Added `terminateProcess()` for graceful shutdown
- Not addressed: Preventing retry cascade on permission errors

### Current Issue (NEW)
- The retry mechanism itself becomes problematic during permission errors
- Multiple processes are spawned due to automatic retries
- Each retry attempt may encounter the same permission issue

## Impact

1. **Resource Consumption**: Multiple Claude processes consuming CPU/memory
2. **Confusing Output**: Messages from different sessions interleaved
3. **Permission Dialog Spam**: User may see multiple permission dialogs
4. **State Confusion**: Multiple sessions with different states
5. **Mobile Impact**: Especially problematic on resource-constrained devices

## Proposed Solution

### Solution 1: Disable Retry on Permission Errors (Recommended)

Modify `tryExecuteWithSessionHistory` to NOT retry when permission error detected:

```go
func tryExecuteWithSessionHistory(...) {
    // ... existing code ...
    
    // Check if it's a permission error BEFORE retrying
    if isPermissionError(errorContent) {
        log.Printf("[SESSION] Permission error detected, skipping session retry")
        // Handle permission error without retry
        handlePermissionError(client, cmd)
        return // Don't proceed with retry logic
    }
    
    // Only retry for non-permission errors
    if shouldRetry && attemptCount < maxRetries {
        // ... existing retry logic ...
    }
}
```

### Solution 2: Track Permission State

Add a flag to prevent retries during permission handling:

```go
type Client struct {
    // ... existing fields ...
    isHandlingPermission bool  // Prevent retries during permission
}

// When permission error detected:
client.isHandlingPermission = true
// When permission granted/denied:
client.isHandlingPermission = false
```

### Solution 3: Terminate All Spawned Processes

Keep track of all processes spawned during retries:

```go
type Client struct {
    // ... existing fields ...
    retryProcesses []*exec.Cmd  // Track all retry processes
}

// On permission error, terminate all:
for _, proc := range client.retryProcesses {
    terminateProcess(proc)
}
```

## Implementation Checklist

- [ ] Identify where session retry logic is triggered (likely in `tryExecuteWithSessionHistory`)
- [ ] Add check for permission errors before retry attempts
- [ ] Ensure permission errors bypass retry mechanism entirely
- [ ] Test that single permission dialog appears for permission errors
- [ ] Verify no zombie processes after permission handling
- [ ] Test with commands that fail for non-permission reasons (should still retry)

## Test Cases

1. **Single Permission Error**
   - Trigger Edit permission error
   - Verify only ONE process and ONE dialog
   - Grant permission and verify continuation

2. **Multiple Sequential Permission Errors**  
   - Trigger permission error
   - Grant permission
   - Trigger another permission error
   - Verify no process accumulation

3. **Non-Permission Errors**
   - Trigger network error or invalid session
   - Verify retry mechanism still works
   - Ensure proper fallback behavior

4. **Rapid Permission Triggers**
   - Execute multiple permission-requiring commands quickly
   - Verify proper serialization and no race conditions

## Success Metrics

- ✅ Only ONE Claude process active during permission dialog
- ✅ No automatic retries for permission errors
- ✅ Clean process termination before permission dialog
- ✅ Proper session continuation after permission granted
- ✅ Retry mechanism still works for non-permission failures

## Priority
**High** - This causes resource issues and confusing UX with multiple processes and dialogs

## Files to Review
- `cmd/swe-swe/websocket.go:558-618` - tryExecuteWithSessionHistory (retry logic)
- `cmd/swe-swe/websocket.go:705-731` - Permission error handling
- `cmd/swe-swe/websocket.go:990-1009` - Permission granted handler

## Related Tasks
- `tasks/done/bug-permission-detection.md` - Permission detection (FIXED)
- `tasks/done/bug-permission-dialog-process-continues.md` - Process termination (FIXED)
- `tasks/bug-permission-granted-full-restart.md` - Full restart issue (CURRENT)