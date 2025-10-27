# Bug: Stop Button Sometimes Unresponsive

## Issue Description
The "Stop" link that appears during processing sometimes doesn't respond when clicked. Users click it but the process continues running without being terminated.

## Initial Investigation Areas

### Frontend Stop Button Flow
- How Stop button click is handled in Main.elm
- Message sending to backend via websocket
- UI state updates after clicking Stop

### Backend Stop Message Processing
- Websocket message handling for "stop" type (websocket.go:1008-1025)
- Process termination logic (`terminateProcess` function)
- Context cancellation and cleanup
- Race conditions between stop and process completion

### Potential Issues to Investigate

#### 1. Race Conditions
- Stop message arrives after process already completed
- Multiple stop messages sent simultaneously
- Context cancellation vs process termination timing

#### 2. Process State Management
- `client.activeProcess` tracking accuracy
- Mutex locking issues in `client.processMutex`
- Process state transitions (running -> stopping -> stopped)

#### 3. Websocket Message Handling
- Message delivery reliability
- Stop message priority vs other messages
- Connection state during stop attempts

#### 4. Process Termination Robustness
- `terminateProcess` function effectiveness (websocket.go:107-141)
- SIGINT vs SIGKILL handling
- Timeout handling (30-second grace period)
- PTY vs pipe process differences

#### 5. UI State Synchronization
- Stop button state management
- Processing indicator vs actual process state
- `exec_end` event delivery after stop

## Investigation Tasks

### Code Analysis Required
1. **Frontend Stop Flow** - Trace from button click to websocket send
2. **Backend Stop Handler** - Analyze websocket.go:1008-1025 message processing
3. **Process Termination** - Review terminateProcess and interruptProcess functions
4. **State Management** - Check activeProcess and cancelFunc coordination
5. **Race Condition Analysis** - Identify timing windows for failures

### Test Scenarios
1. Click Stop immediately after sending message
2. Click Stop during active processing
3. Click Stop multiple times rapidly
4. Click Stop just before process completes naturally
5. Test with different agent types (claude vs goose)
6. Test with slow vs fast network connections

### Expected Behaviors
- Stop should always terminate active process within 30 seconds
- UI should immediately show stopping state
- Process should not continue after stop confirmed
- New messages should be blocked until stop completes

## Files to Examine
- `elm/src/Main.elm` - Stop button click handling
- `cmd/swe-swe/websocket.go:1008-1025` - Stop message processing
- `cmd/swe-swe/websocket.go:107-141` - terminateProcess function
- `cmd/swe-swe/websocket.go:649-653` - activeProcess management

## Success Criteria
- Stop button responds within 1 second of click
- Process termination occurs within 5 seconds (well under 30s timeout)
- UI accurately reflects process state at all times
- No zombie processes or goroutine leaks after stop