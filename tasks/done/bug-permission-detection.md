# Bug: Permission Error Detection Not Working for "This command requires approval"

## Description
The websocket permission detection system fails to recognize "This command requires approval" as a permission error, causing the system to not trigger the permission request flow properly.

## Expected Behavior
When Claude returns a tool_result with `is_error: true` and content "This command requires approval", the system should:
1. Detect this as a permission error
2. Send a permission_request to the frontend
3. Terminate the current process
4. Wait for user permission response

## Actual Behavior
The system does not recognize "This command requires approval" as a permission error, so the permission request flow is not triggered.

## Root Cause
The `isPermissionError()` function in `cmd/swe-swe/websocket.go:206-211` only checks for:
- "requested permissions"
- "haven't granted it yet" 
- "permission denied"

But does not check for "This command requires approval".

## Log Evidence
```
2025/10/12 16:07:03 [STDOUT] {"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"This command requires approval","is_error":true,"tool_use_id":"toolu_019NQz5efud4xu9JM1ubhS1r"}]},"parent_tool_use_id":null,"session_id":"bdd53fca-1555-42dd-a360-ced92dd93087","uuid":"0b37e381-4b4b-4732-a9f2-c56a7a08ebc5"}
```

## Solution
Update the `isPermissionError()` function to include "This command requires approval" as a recognized permission error string.

## Files to Modify
- `cmd/swe-swe/websocket.go` - Update `isPermissionError()` function

## Priority
High - This breaks the permission system functionality