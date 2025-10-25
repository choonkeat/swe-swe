# Session Persistence Robustness Concern

## Concern
Need to understand and document how robust our Claude session persistence is across:
- Browser window reloads
- WebSocket reconnections
- Clicking `Stop` on process

**This is VERY IMPORTANT because mobile clients have a high rate of losing connection:**
- Device sleep/wake cycles
- Unstable network conditions
- App backgrounding/foregrounding
- Network switching (WiFi to cellular and vice versa)

## Documentation Needed
Thorough step-by-step documentation covering:

### 1. Server Boot Up
- Initial server state
- How sessions are initialized
- What data structures are created

### 2. Browser Connection
- How browser establishes initial connection
- What handshake/initialization happens
- How session is created/assigned

### 3. Message Flow
- All various possible messages browser can send
- How each message type is handled
- State changes triggered by each message

### 4. Reconnection Scenarios
- Browser reload
  - What happens to existing session
  - How browser identifies itself on reconnect
  - How server matches to existing session
  
- WebSocket disconnection/reconnection
  - Detection mechanism
  - State preservation during disconnect
  - Reconnection handshake
  
- Clicking Stop
  - What happens to the process
  - How state is preserved/cleaned up
  - Impact on session

### 5. State Synchronization
- How server state changes affect browser state
- How browser state changes affect server state
- Consistency mechanisms

### 6. Session Management
- How browser sends messages after reconnection
- How server knows which session the message belongs to
- How server asks Claude CLI to use specific session ID
- What happens when session ID is not found
  - Error handling
  - Recovery mechanisms
  - User experience impact

## Goal
Create comprehensive documentation that maps out the entire session lifecycle and identifies potential failure points or areas for improvement in session persistence robustness.