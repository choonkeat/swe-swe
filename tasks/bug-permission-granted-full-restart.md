# Bug: Assistant Performs Full Task Restart After Permission Granted Instead of Simple Retry

## Problem Description
When a Bash command requires permission and the user grants it (types "y"), the assistant restarts the entire task from the beginning instead of simply retrying the failed command. This leads to inefficient behavior and redundant work.

## Observed Behavior
From the PDF `debug/swe-swe Chat resuming-after-permission-dialog.pdf`:

1. Assistant tries to execute: `mv tasks/bug-chat-auto-scroll-interruption.md tasks/done/bug-chat-auto-scroll-interruption.md`
2. Permission dialog appears for Bash access
3. User grants permission by typing "y"
4. **Issue**: Assistant responds with "I'll check if the bug described in `tasks/bug-chat-auto-scroll-interruption.md` has been implemented..."
5. Assistant re-reads all files, re-verifies implementation, re-checks everything
6. Finally executes the same `mv` command that was originally requested

## Expected Behavior
After permission is granted, the assistant should:
1. Simply retry the exact Bash command that failed
2. Continue with the next step in the task
3. NOT restart the entire verification process

## Impact
- **Performance**: Unnecessary re-execution of all previous steps
- **User Experience**: Confusing to see the assistant repeat work already done
- **Token Usage**: Wastes tokens re-reading files and re-analyzing code
- **Time**: Significantly longer to complete tasks that require permissions

## Example from PDF
### What Happened:
```
1. Assistant verifies implementation ✅
2. Assistant tries: mv command → Permission required
3. User grants permission
4. Assistant starts over: "I'll check if the bug described..."
5. Re-reads the bug file
6. Re-greps for implementation
7. Re-reads index.html.tmpl
8. Re-verifies CSS styles
9. Finally runs the mv command
```

### What Should Happen:
```
1. Assistant verifies implementation ✅
2. Assistant tries: mv command → Permission required
3. User grants permission
4. Assistant retries: mv command ✅
5. Continues with next task
```

## Root Cause Analysis
The issue appears to be that when permission is granted, the system replays the entire original user message ("can we look to see if tasks/bug-chat-auto-scroll-interruption.md is implemented? if fully implemented, move it into tasks/done ?") rather than just retrying the specific failed tool call.

This is likely related to the fix in `bug-permission-dialog-process-continues.md` where the `lastUserMessage` is replayed after permission is granted. The implementation stores and replays the entire user request instead of maintaining context about what specific operation failed.

## Priority
**Medium** - This is inefficient but doesn't break functionality. It affects user experience and resource usage.

## Suggested Solution

### Option 1: Store Failed Tool Call (Preferred)
Instead of replaying the entire user message, store the specific tool call that failed:

```go
type Client struct {
    // ... existing fields
    lastFailedToolCall *ToolCall  // Store the specific failed operation
    taskContext        *TaskState // Store where we were in the task
}
```

When permission is granted, retry only the failed tool call and continue from where we left off.

### Option 2: Implement Checkpoint System
Create checkpoints in the task execution that can be resumed from:
- Before each tool call, save a checkpoint
- After permission granted, resume from the last checkpoint
- Avoids re-doing completed work

### Option 3: Simple Command Retry
For Bash commands specifically, just store and retry the exact command:
```go
type Client struct {
    // ... existing fields
    lastFailedBashCommand string // Store just the bash command
}
```

## Related Issues
- `bug-permission-dialog-process-continues.md` (fixed) - Addressed the race condition but introduced this restart behavior
- The fix for the race condition stores `lastUserMessage` and replays it entirely

## Success Criteria
1. After permission granted, only the failed operation is retried
2. No re-execution of previously successful steps
3. Task continues from the point of failure
4. Todo list (if used) maintains its state correctly

## Test Cases
1. Task with single Bash command requiring permission
2. Multi-step task where permission needed mid-task
3. Task with multiple permission requests
4. Permission denied then granted scenarios

## Implementation Notes
- Need to differentiate between "retry last command" vs "restart entire task"
- Consider maintaining execution context/state
- Ensure todo list state is preserved across permission dialogs
- May need to modify the permission granted handler in `websocket.go`