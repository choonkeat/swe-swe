# Bug: Chat Message Delay Before Processing Indicator

## Issue Description
When sending a chat message from the browser, there is a noticeable delay (several seconds) between:
1. The swe-swe name appearing 
2. The processing indicator with "Stop" link showing up

Users experience this as the bot name appearing but with nothing below it for several seconds.

## Root Cause Analysis

### Timeline of Events
1. **User sends message** (instant)
2. **Bot name "swe-swe" appears** (websocket.go:1176-1180) - instant
3. **Processing delay** - several seconds gap
4. **Processing indicator appears** (websocket.go:679-682) - after command starts

### Source of Delay
The delay occurs between steps 2 and 4 due to several factors:

#### Backend Processing Chain (websocket.go)
- `websocket.go:1180` - Bot sender item sent immediately
- `websocket.go:1183-1192` - Goroutine launched for `tryExecuteWithSessionHistory`
- Session validation and command preparation happens
- `websocket.go:677` - Process finally started with PID
- `websocket.go:679-682` - `exec_start` event sent (triggers processing indicator)

#### Specific Delay Sources
1. **Session Validation** (`validateClaudeSession` in websocket.go:433-467)
   - Runs `claude --resume sessionID --print "echo test"` with 5-second timeout
   - This validation happens for every subsequent message
   - Network/filesystem delays in claude binary execution

2. **Command Preparation** (websocket.go:495-566)
   - Command line argument parsing and modification
   - Adding resume flags, permissions, allowed tools
   - Multiple string operations and slicing

3. **Process Startup** (websocket.go:572-677)
   - `exec.CommandContext` creation
   - Pipe setup (stdin, stdout, stderr)
   - Actual process start with `cmd.Start()`

### Frontend Flow (Main.elm)
- Bot name immediately displayed when `ChatBot` message received
- `isTyping` indicator only shows when `ChatExecStart` message received
- No intermediate loading state between bot name and processing

## Impact
- Poor user experience with apparent "hanging" bot responses
- Users may think the system is broken or unresponsive
- May lead to duplicate message sends or browser refreshes

## Proposed Solutions

### Option 1: Immediate Processing Indicator
Show processing indicator immediately when bot name appears, before command validation:

```go
// In websocket.go after sending bot sender item
svc.BroadcastToSession(ChatItem{
    Type: "exec_start",
}, client.browserSessionID)

// Then do session validation and command execution
go func() {
    // ... existing validation and execution logic
}()
```

### Option 2: Progressive Status Updates
Add intermediate status messages:
- "Validating session..."
- "Preparing command..."
- "Starting process..."

### Option 3: Optimize Session Validation
- Cache session validation results
- Use faster validation method than full claude command
- Run validation asynchronously in background

### Option 4: Preemptive Session Warming
Start preparing the next command immediately after the previous one completes.

## Recommended Fix
**Option 1** is the simplest and most effective:
- Move `exec_start` event to immediately after bot sender item
- Provides immediate visual feedback
- Minimal code changes required
- Maintains existing error handling

## Files Affected
- `cmd/swe-swe/websocket.go:1180` - Add exec_start event
- `elm/src/Main.elm` - Already handles exec_start properly

## Test Cases
1. Send first message - verify immediate processing indicator
2. Send subsequent message - verify no delay before indicator
3. Send message with invalid session - verify indicator still shows during validation
4. Test with slow network/filesystem - verify user gets immediate feedback