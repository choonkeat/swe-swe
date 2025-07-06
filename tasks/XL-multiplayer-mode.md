# Feature: Multi-player Mode - Shared Conversation Sessions

## Overview
Enable multiple browser clients to participate in the same conversation with the agent, preventing conflicting file edits and enabling team collaboration. This transforms swe-swe from individual sessions to a shared workspace model.

## Key Features

### 1. Session Management
- Single shared conversation thread for all connected users
- User presence indicators (who's online)
- User identification (names/avatars)
- Session persistence and rejoin capability
- Optional session passwords/invite links

### 2. Message Attribution
- Show which team member sent each message
- Visual indicators for "typing" status
- Message timestamps with timezone handling
- Thread view with user avatars

### 3. Concurrency Control
- **Single Agent Instance**: Only one agent response at a time
- **Message Queuing**: Team messages during agent work
  - Similar to task #003 but multi-user aware
  - "Jane queued a message" indicator
  - Option to view/cancel queued messages
- **Permission Coordination**: 
  - Who can approve tool permissions?
  - Voting system vs. designated approver
  - Timeout handling for absent approvers

### 4. File Edit Coordination
- Lock files during agent edits
- Show which files agent is currently modifying
- Conflict prevention, not resolution
- Real-time file change notifications

### 5. User Roles and Permissions
```yaml
roles:
  owner:
    - start_session
    - approve_all_permissions
    - kick_users
    - end_session
  member:
    - send_messages
    - view_conversation
    - approve_own_permissions
  observer:
    - view_conversation
    - view_file_changes
```

## Technical Architecture

### State Synchronization
- WebSocket for real-time updates
- Operational Transform or CRDT for message ordering
- Centralized state management
- Optimistic UI updates with reconciliation

### Session Model
```typescript
interface Session {
  id: string;
  participants: User[];
  conversation: Message[];
  agentState: {
    isProcessing: boolean;
    currentTask: string;
    lockedFiles: string[];
  };
  messageQueue: QueuedMessage[];
  permissionRequests: PermissionRequest[];
}
```

### Conflict Scenarios
1. **Simultaneous Messages**: Handle race conditions
2. **Permission Conflicts**: Multiple approval attempts
3. **Session State Divergence**: Network partitions
4. **File System Race**: External changes during agent work

## User Experience Flows

### Joining a Session
1. First user creates session → becomes owner
2. Share session URL/code with team
3. New users join → see full history
4. Late joiners catch up with replay

### During Agent Processing
- All users see typing indicator
- File lock indicators appear
- Only designated approver(s) see permission dialogs
- Other users see "Waiting for approval from..."
- Queued messages show sender attribution

### Message Queue Behavior (Multi-user)
- Each user can queue messages
- Queue shows all pending messages with attribution
- Optional: Combine similar requests
- Optional: Priority based on role

## Design Decisions Needed

### 1. Permission Approval Model
**Option A: First Come First Serve**
- Any user can approve
- Fast but potentially chaotic

**Option B: Designated Approver**
- Session owner or rotating role
- Controlled but potential bottleneck

**Option C: Consensus/Voting**
- Require N approvals
- Safe but slow

### 2. Message Queue Interaction
- Should users see each other's queued messages?
- Can users cancel others' queued messages?
- How to handle queue order fairness?

### 3. Session Persistence
- How long to maintain session state?
- Reconnection grace period?
- History limits?

## Security Considerations
- Authentication vs. simple session codes
- Rate limiting per user
- Audit trail of actions
- Encrypted communication
- Resource usage limits

## UI/UX Mockup Ideas
```
┌─────────────────────────────────────┐
│ 🟢 Alice  🟢 Bob  🔴 Charlie (away) │
├─────────────────────────────────────┤
│ Alice: Update the config file       │
│                                     │
│ 🤖 Agent: Looking at config.js...   │
│ [Tool Output...]                    │
│                                     │
│ Bob: Also check the tests          │
│ [Queued - will send after current] │
└─────────────────────────────────────┘
│ Charlie is typing...                │
│ [Message input - disabled for you]  │
└─────────────────────────────────────┘
```

## Implications for Other Features

### Task #003 (Message Queuing)
- Extend to show message author
- Fair queue ordering across users
- Prevent queue flooding by single user

### Task #002 (Auto-focus)
- Disable when another user is typing
- Smart focus based on last interaction

## Benefits
- True collaborative debugging
- Knowledge sharing during problem solving
- Prevents conflicting agent operations
- Team learning opportunities

## Challenges
- Complex state synchronization
- Permission model decisions
- Network reliability requirements
- Scaling beyond small teams

## Estimation

### T-Shirt Size: XL (Extra Large)

### Breakdown
- **WebSocket Infrastructure**: L
  - Real-time message sync
  - Presence system
  - Connection management
  
- **State Management**: L
  - Distributed state sync
  - Conflict resolution
  - Queue coordination
  
- **Permission System**: M
  - Multi-user approval flow
  - Role management
  
- **UI Changes**: L
  - Presence indicators
  - Multi-user queue display
  - Permission UI adaptation

### Impact Analysis
- **User Experience**: High positive for teams, negative for solo users if mandatory
- **Codebase Changes**: Major - fundamental architecture change
- **Architecture**: Complete redesign of session model
- **Performance Risk**: High - real-time sync overhead

### Agent-Era Estimation Notes
This is "Extra Large" because:
- Fundamental architecture change from single to multi-user
- Many UX decisions with trade-offs
- Complex distributed systems problems
- Testing matrix explodes with concurrent users
- Performance implications need careful consideration
- Security model becomes critical

### Open Questions for Discussion
1. Should multi-player be opt-in or default?
2. How to handle permission approval timeout?
3. Maximum users per session?
4. Graceful degradation for network issues?
5. Integration with existing auth systems?