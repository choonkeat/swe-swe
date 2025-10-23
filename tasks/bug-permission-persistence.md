# Fix: Permission Persistence Bug in Session Resumption

## Problem Description

**Bug**: Permissions previously granted to Claude are lost/forgotten in later chat messages within the same session when Claude resumes using `--resume`.

**Root Cause**: The backend logic in `websocket.go` has a conditional flaw where `--allowedTools` is only passed to Claude when `skipPermissions` is false, but it should always be passed when allowed tools exist, regardless of the skip permissions setting.

**Impact**: Users have to re-grant the same tool permissions repeatedly within a single conversation, creating poor UX.

## Current Architecture (Mostly Correct)

The browser-based permission persistence is already implemented:

1. **Frontend State**: Elm model stores `allowedTools: List String` (Main.elm:103)
2. **Message Protocol**: `ClientMessage` includes `AllowedTools []string` field (websocket.go:52)
3. **Backend Handling**: `executeAgentCommand` accepts and processes `allowedTools` parameter

## Root Cause Analysis

### Location: `websocket.go:319-322`

```go
// CURRENT BUGGY CODE:
if skipPermissions {
    // Add --dangerously-skip-permissions flag
    cmdArgs = append(cmdArgs[:insertPos], append([]string{"--dangerously-skip-permissions"}, cmdArgs[insertPos:]...)...)
} else if len(allowedTools) > 0 {  // ❌ BUG: This won't run if skipPermissions was true
    // Add allowed tools as a comma-separated list
    cmdArgs = append(cmdArgs, "--allowedTools", strings.Join(allowedTools, ","))
}
```

**Problem**: The `else if` condition means if `skipPermissions` is true, the `allowedTools` are never passed to Claude, even though they should be passed regardless.

## Detailed Fix Plan

### Step 1: Fix Backend Logic in `websocket.go`

**File**: `cmd/swe-swe/websocket.go`
**Lines**: 319-322
**Change**: Convert `else if` to separate `if` statement

```go
// FIXED CODE:
if skipPermissions {
    // Add --dangerously-skip-permissions only if user explicitly chose to skip
    insertPos := 1
    if !isFirstMessage && claudeSessionID != "" {
        insertPos = 3 // After claude --resume sessionID
    }
    cmdArgs = append(cmdArgs[:insertPos], append([]string{"--dangerously-skip-permissions"}, cmdArgs[insertPos:]...)...)
}
// ALWAYS add allowed tools if we have them (separate from skipPermissions)
if len(allowedTools) > 0 {
    cmdArgs = append(cmdArgs, "--allowedTools", strings.Join(allowedTools, ","))
}
```

### Step 2: Verify Frontend State Management

**File**: `elm/src/Main.elm`
**Lines**: 103, 631-632, 661-662, 679-680

Confirm these components work correctly:
- `allowedTools` is properly updated when permissions are granted
- `allowedTools` is included in outgoing messages to backend
- `allowedTools` persists across multiple messages in the same session

### Step 3: Add Logging for Debugging

**File**: `cmd/swe-swe/websocket.go`
**Location**: After line 322

Add debug logging to verify the fix:

```go
if len(allowedTools) > 0 {
    log.Printf("[PERMISSIONS] Passing allowed tools to Claude: %v", allowedTools)
    cmdArgs = append(cmdArgs, "--allowedTools", strings.Join(allowedTools, ","))
}
```

### Step 4: Testing Plan

#### Test Case 1: Basic Permission Persistence
1. Start fresh chat session
2. Send message that triggers tool permission request (e.g., "read a file")
3. Grant permission for tool (e.g., "Read")
4. Send another message that requires the same tool
5. **Expected**: No permission request should appear
6. **Current Behavior**: Permission request appears again (BUG)

#### Test Case 2: Skip Permissions + Allowed Tools
1. Start fresh chat session  
2. Send message that triggers permission request
3. Choose "YOLO" (skip all permissions)
4. Send another message
5. **Expected**: No permission requests, and any previously allowed tools should still be passed
6. **Verify**: Check logs for both `--dangerously-skip-permissions` and `--allowedTools` flags

#### Test Case 3: Session Resumption
1. Start chat, grant some tool permissions
2. Refresh browser (simulates session resumption)
3. Send new message requiring previously granted tool
4. **Expected**: Should work without re-requesting permission
5. **Note**: This tests browser state persistence across page reloads

### Step 5: Edge Cases to Consider

1. **Empty allowed tools list**: Ensure `--allowedTools` flag is not added with empty string
2. **Mixed permissions**: Some tools allowed, some denied, some skipped
3. **Multiple tools in single message**: Comma-separated list format
4. **Session ID changes**: What happens if Claude session expires and gets new ID

## Implementation Notes

### Why This Fix Is Correct

1. **Separation of Concerns**: `skipPermissions` and `allowedTools` serve different purposes:
   - `skipPermissions`: User chose to bypass ALL permission checks
   - `allowedTools`: Specific tools that user has explicitly allowed

2. **Backward Compatibility**: This change doesn't break existing behavior, just fixes the edge case

3. **Browser-First Architecture**: Keeps permissions in browser state where user can see/manage them

### Potential Risks

1. **Low Risk**: This is a small logical change that makes the code do what it was already intended to do
2. **Testing Required**: Need to verify all permission workflows still work
3. **Claude CLI Compatibility**: Ensure `--allowedTools` flag works correctly with current Claude CLI version

## Success Criteria

✅ **Primary**: User grants tool permission once, doesn't get asked again in same session
✅ **Secondary**: `skipPermissions` and specific `allowedTools` can coexist
✅ **Tertiary**: Debug logs show both flags being passed to Claude when appropriate

## Related Files

- `cmd/swe-swe/websocket.go` - Main fix location
- `elm/src/Main.elm` - Frontend permission state management  
- Tests to be added for regression prevention