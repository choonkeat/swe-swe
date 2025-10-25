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

### Expected Behavior
When a permission dialog is triggered:
1. The process should pause/suspend execution
2. No further processing should occur until the user responds to the permission dialog
3. After permission is granted/denied, the process should either:
   - Continue from where it paused (if granted)
   - Gracefully handle the denial and stop or provide alternatives

### Technical Details
The error message shows:
- File path: `/Users/choonkeatchew/git/choonkeat/swe-swe/tasks/session-persistence-robustness-concern.md`
- Operation: Edit (attempting to write to file)
- The system correctly detects the permission requirement but doesn't halt execution

### Related Issues
- This may be related to the session persistence robustness concerns, especially for mobile clients
- Could interact poorly with WebSocket reconnections if permission dialogs are pending

## Priority
**High** - This affects core functionality and user experience, especially for mobile users who are more likely to encounter permission issues and have slower response times.

## Research Findings

### Current Implementation Analysis
After thorough investigation of the codebase, I've identified the following:

1. **Permission Detection**: The system correctly detects permission errors in `websocket.go:638` by checking error messages for permission-related text
2. **Process Termination**: When a permission error is detected, the code correctly:
   - Sends a permission request to the frontend (`websocket.go:646`)
   - Tracks the pending tool permission (`websocket.go:655`)
   - Calls `cancel()` to terminate the context (`websocket.go:660`)
   - Calls `cmd.Wait()` to wait for process termination (`websocket.go:669`)

### Root Cause
The issue is a **race condition** between Claude's output generation and permission error detection:

1. Claude attempts a tool use (e.g., Edit) that requires permission
2. The tool returns an error immediately
3. Claude receives the error and starts generating a response (e.g., "I'll update the task document...")
4. This response is already in the output pipeline/buffer
5. The system detects the permission error and sends the permission dialog
6. Claude's buffered response gets displayed BEFORE the process is fully terminated
7. The `cancel()` and `cmd.Wait()` are called, but the damage is done

The problem is that Claude operates in a streaming fashion with JSON output mode, and by the time the permission error is detected and processed, Claude has already generated and buffered additional output that gets sent to the frontend.

### Why This Happens
- The permission check happens AFTER the tool result is received
- Claude immediately starts generating a response to the tool error
- The JSON streaming means messages are sent as soon as they're generated
- There's no mechanism to prevent buffered messages from being sent once a permission error is detected

## Proposed Solutions

### Solution 1: Buffer Output Until Permission Check (Recommended)
Implement a buffering mechanism that holds Claude's output after a tool error until we verify it's not a permission issue:

```go
// When a tool_result with is_error=true is received:
1. Start buffering all subsequent output
2. Check if it's a permission error
3. If permission error:
   - Discard the buffer
   - Send permission request
   - Terminate the process
4. If not permission error:
   - Flush the buffer
   - Continue normally
```

### Solution 2: Immediate Process Suspension
When any tool error occurs, immediately suspend the process before checking the error type:

```go
// Pseudo-code
if content.Type == "tool_result" && content.IsError {
    // Immediately pause stdout reading
    suspendOutput = true
    
    if isPermissionError(content.Content) {
        // Handle permission request
        cancel()
    } else {
        // Resume output
        suspendOutput = false
    }
}
```

### Solution 3: Pre-emptive Permission Check
Check permissions BEFORE sending the tool use to Claude:

```go
// Before forwarding tool_use to the process
if toolRequiresPermission(toolName) && !hasPermission(client, toolName) {
    // Don't forward the tool_use
    // Send permission request immediately
    // Only continue if permission granted
}
```

### Solution 4: Message Filtering
Filter out Claude's response messages that come after a permission error:

```go
// Track when permission error occurred
if permissionErrorDetected {
    // Filter out any assistant messages until permission is resolved
    if claudeMsg.Type == "assistant" {
        continue // Don't broadcast this message
    }
}
```

## Recommended Fix Implementation

The most robust solution is **Solution 1 (Buffer Output)** combined with **Solution 3 (Pre-emptive Check)**:

1. **Pre-emptive**: Check if a tool requires permission before it's executed
2. **Defensive**: Buffer output after tool errors to catch any missed permission issues
3. **Clean**: Prevents confusing partial responses from appearing

This dual approach ensures:
- Most permission requests happen before Claude even tries the tool
- Any edge cases are caught by the buffering mechanism
- User never sees confusing partial responses

## CRITICAL NEW FINDING: Multiple Processes Still Running

### Evidence from logs.txt
After granting permission and the process supposedly completing, messages CONTINUE arriving from a DIFFERENT Claude session that wasn't properly terminated:

```
Line 518: [EXEC] Process completed successfully 
Line 519: [SESSION] Successfully resumed with session ID: dad078c6-f419-4cac-be2b-4b1d423f3194
Lines 521-547: MORE MESSAGES from session 36581b64-73d1-429a-bb0e-f95179dfc7b4!
```

This reveals a **SEVERE BUG**: The permission error cancellation only stops ONE process, but there are MULTIPLE Claude processes running from the retry attempts, and they continue executing even after the UI shows completion.

### Why This Is Worse Than Expected
1. **Zombie Processes**: Old Claude sessions continue running after permission dialogs
2. **Multiple Parallel Executions**: Each retry creates a NEW process without killing the old one
3. **No Stop Button**: UI thinks process is done, but Claude keeps generating output
4. **Resource Waste**: Multiple Claude API calls running simultaneously
5. **Unpredictable Results**: User might get responses from the wrong session

### Root Cause of Multiple Processes
When permission is denied and then granted:
1. First process hits permission error and calls `cancel()`
2. User grants permission
3. System starts a NEW process with `tryExecuteWithSessionHistory`
4. BUT the old process isn't fully dead - it's still streaming output!
5. Both processes continue running in parallel

## Implementation Plan

### URGENT Phase 0: Fix Process Termination (CRITICAL)
**Files to modify**: `cmd/swe-swe/websocket.go`

The `cancel()` context cancellation is NOT killing the Claude CLI process properly. We need:

1. **Store the actual process PID**:
   ```go
   type Client struct {
       activeProcess *exec.Cmd
       // ... existing fields
   }
   ```

2. **Force kill the process on permission error**:
   ```go
   if client.activeProcess != nil {
       client.activeProcess.Process.Kill()
       client.activeProcess = nil
   }
   ```

3. **Prevent duplicate processes**:
   - Check if a process is already running before starting a new one
   - Kill any existing process before starting retry

**Effort**: 2-3 hours
**Impact**: Prevents zombie processes and resource exhaustion

### Session ID Implications of Killing Processes

**IMPORTANT CONSIDERATION**: Killing the Claude CLI process mid-execution has significant implications for session management:

#### How Session IDs Currently Work
1. **Claude CLI generates a session ID** when it starts (seen in `session_id` field of JSON output)
2. **Browser stores this ID** and sends it back for continuity
3. **Server uses `--resume` flag** with the session ID to continue conversations
4. **Session history is maintained** in `claudeSessionHistory` (max 10 IDs)

#### What Happens When We Kill a Process

**Scenario 1: Process Killed During Permission Error**
```
1. Claude starts with session A, generates some output
2. Permission error detected, process killed with Process.Kill()
3. Session A is now in an INCONSISTENT STATE on Claude's servers
4. When user grants permission and retries:
   - Using --resume with session A might fail or produce unexpected results
   - Claude might not have the full context of what was attempted
```

**Scenario 2: Multiple Processes with Same Session**
```
1. Process 1 uses session A
2. Permission error, Process 1 killed
3. Process 2 starts, tries to --resume session A
4. CONFLICT: Two processes tried to use same session
5. Claude API might reject or produce corrupted state
```

#### Risks of Force Killing
1. **Session Corruption**: Abruptly killed sessions leave Claude's backend in unknown state
2. **Context Loss**: Partial tool executions aren't properly rolled back
3. **Resume Failures**: Next `--resume` attempt might fail with "No conversation found"
4. **Mobile Impact**: Mobile users already face session persistence issues (see session-persistence-robustness-concern.md)

#### Safer Approach: Graceful Termination

Instead of `Process.Kill()`, we should:

1. **Send interrupt signal first**: Try `Process.Signal(os.Interrupt)` 
2. **Wait briefly for graceful shutdown**: Give Claude time to clean up
3. **Only force kill if necessary**: After timeout, use `Process.Kill()`
4. **Track session state**: Mark sessions as "dirty" if force killed
5. **Start fresh on retry**: Don't use `--resume` after a force kill

```go
// Graceful termination with timeout
func terminateProcess(cmd *exec.Cmd) {
    if cmd.Process != nil {
        // Try graceful shutdown first
        cmd.Process.Signal(os.Interrupt)
        
        done := make(chan error, 1)
        go func() {
            done <- cmd.Wait()
        }()
        
        select {
        case <-time.After(2 * time.Second):
            // Force kill after timeout
            cmd.Process.Kill()
        case <-done:
            // Process terminated gracefully
        }
    }
}
```

#### Recommended Solution

**For Permission Errors**: 
- Don't kill the process at all
- Let it complete naturally but filter its output
- Start fresh session for retry (don't use --resume)

**For Stop Button**:
- Use graceful termination 
- Mark session as potentially corrupted
- Inform user if session recovery fails

This approach maintains session integrity while preventing zombie processes.

### Phase 1: Add Message Filtering (Quick Fix)
**Files to modify**: `cmd/swe-swe/websocket.go`

1. Add a field to track permission error state in the Client struct:
   ```go
   permissionErrorDetected bool
   ```

2. When permission error is detected (line 658), set the flag:
   ```go
   client.permissionErrorDetected = true
   ```

3. Filter assistant messages after permission error:
   ```go
   if client.permissionErrorDetected && claudeMsg.Type == "assistant" {
       continue // Don't broadcast
   }
   ```

4. Clear flag when permission is resolved (line 867)

**Effort**: 1-2 hours
**Impact**: Immediately stops confusing messages from appearing

### Phase 2: Implement Output Buffering
**Files to modify**: `cmd/swe-swe/websocket.go`

1. Add a message buffer to the Client struct:
   ```go
   messageBuffer []ChatItem
   bufferingOutput bool
   ```

2. When tool_result with error is detected, start buffering:
   ```go
   if content.Type == "tool_result" && content.IsError {
       client.bufferingOutput = true
   }
   ```

3. Buffer messages instead of broadcasting when buffering is active

4. On permission error: discard buffer and show dialog
5. On non-permission error: flush buffer and continue

**Effort**: 3-4 hours
**Impact**: Clean solution that prevents any race conditions

### Phase 3: Pre-emptive Permission Checking
**Files to modify**: `cmd/swe-swe/websocket.go`, possibly Claude CLI integration

1. Intercept tool_use messages before they reach Claude
2. Check if tool requires permission and client hasn't granted it
3. If permission needed, show dialog BEFORE Claude attempts the tool
4. Only forward to Claude after permission granted

**Effort**: 4-6 hours
**Impact**: Best UX - permission dialogs appear before errors occur

### Testing Plan
1. Test with various permission-requiring tools (Edit, Write, etc.)
2. Test rapid tool uses that fail with permissions
3. Test permission grant/deny flows
4. Test with mobile clients (slower response times)
5. Test session resumption with pending permissions

### Rollout Strategy
1. Implement Phase 1 first as immediate mitigation
2. Deploy and monitor for any issues
3. Implement Phase 2 for more robust handling
4. Implement Phase 3 for optimal UX
5. Consider backporting to mobile-specific code paths if needed

## Permission Retry Message Simplification

The permission retry message should be simplified to just "continue" because:

1. **It matches Claude CLI's expected input** - When Claude CLI pauses for permission, it's waiting for a simple continuation command like "continue" or "y", not a full explanation.

2. **Avoids confusing the AI assistant** - The verbose message "Permission fixed. Try again. (If editing files, you would need to read them again)" could be interpreted as a new user request or instruction, potentially causing Claude to:
   - Re-read files unnecessarily
   - Start a new task instead of continuing the interrupted one
   - Get confused about what to do next

3. **Maintains conversation flow** - "continue" is a clear signal to resume the interrupted operation from where it left off, without adding extra context that might derail the current task.

4. **Prevents redundant actions** - The original message explicitly suggests re-reading files, which may not always be necessary and could waste time/tokens.

The simpler "continue" message ensures Claude CLI resumes exactly where it stopped, treating it as a continuation of the interrupted flow rather than a new instruction.