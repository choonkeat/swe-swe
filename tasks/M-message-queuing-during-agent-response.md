# Feature: Message Queuing During Agent Response

## Overview
Allow users to compose and queue messages while the agent is still processing/responding to a previous message. This enables a more fluid conversation flow without waiting for each agent iteration to complete.

## Key Features

### 1. Message Input Behavior
- Keep message input enabled during agent response
- Show visual indicator that message will be queued
- Display queue position/status (e.g., "Message will be sent after current task completes")
- Allow editing/canceling queued messages

### 2. Queue Management
- Queue messages in order of submission
- Limit queue size (suggested: 3-5 messages)
- Clear queue option
- Visual queue display showing pending messages

### 3. Interaction with Agent Operations
- **Permission Prompts**: Queued messages must NOT interfere with:
  - Tool permission dialogs (Bash, Write, Edit, etc.)
  - File selection dialogs
  - Any interactive agent requests
- **Stop Button**: Remains the primary interruption method
  - Clicking Stop cancels current operation
  - Optionally: Stop clears queue vs. Stop only stops current

### 4. Message Processing Behavior
- Send queued messages automatically after agent completes
- Optional delay between messages (prevent overwhelming)
- Combine related queued messages intelligently (optional advanced feature)

## Technical Considerations

### State Management
```typescript
interface QueueState {
  messages: QueuedMessage[];
  isProcessing: boolean;
  currentPermissionDialog: PermissionType | null;
}

interface QueuedMessage {
  id: string;
  content: string;
  timestamp: Date;
  status: 'queued' | 'sending' | 'sent';
}
```

### UI States
1. **Normal**: Input enabled, send button active
2. **Agent Processing**: Input enabled, "Queue" indicator shown
3. **Permission Dialog Active**: Input enabled but dimmed, queue paused
4. **Queue Full**: Input disabled with "Queue full" message

### Edge Cases
- User queues conflicting instructions
- Permission dialog appears while typing
- Network interruption during queue processing
- User edits queued message while agent is processing
- Rapid fire message submissions

## User Interface Design

### Visual Indicators
- Queued message counter badge
- Different send button state (e.g., "Queue" vs "Send")
- Inline queue preview below input
- Toast notifications for queue status

### Interaction Flow
1. User types while agent is working
2. Press Enter/Send → Message queued
3. Visual confirmation of queue
4. Agent completes → Queue processes automatically
5. Each queued message sent sequentially

## Configuration Options
```yaml
messageQueue:
  enabled: true
  maxQueueSize: 3
  autoSendDelay: 500ms
  clearQueueOnStop: false
  combineRelatedMessages: false
```

## Benefits
- Non-blocking conversation flow
- Better user thought continuity
- Reduced waiting time perception
- Batch instruction capability

## Risks and Mitigations
- **Risk**: User confusion about message order
  - **Mitigation**: Clear visual queue display
- **Risk**: Conflicting instructions in queue
  - **Mitigation**: Warning when detected
- **Risk**: Permission dialog interruption
  - **Mitigation**: Pause queue during dialogs

## Alternatives Considered
- **Interrupt and Replace**: New message cancels current task
  - Rejected: Too disruptive, Stop button serves this purpose
- **Immediate Interruption**: Send message immediately
  - Rejected: Could break agent state, permission flow

## Estimation

### T-Shirt Size: M (Medium)

### Breakdown
- **Queue Implementation**: S
  - Basic queue data structure
  - Message storage and retrieval
  
- **UI State Management**: M
  - Multiple input states
  - Visual indicators
  - Queue display component
  
- **Permission Dialog Integration**: M
  - Detect active dialogs
  - Pause/resume queue
  - State synchronization
  
- **Testing Complexity**: M
  - Async flow testing
  - Permission interaction testing
  - Edge case coverage

### Impact Analysis
- **User Experience**: High positive impact
- **Codebase Changes**: Moderate - new state management layer
- **Architecture**: Medium impact - message flow redesign
- **Performance Risk**: Low - client-side queue only

### Agent-Era Estimation Notes
This is "Medium" because:
- Complex state interactions with existing systems
- UX decisions requiring human input
- Permission system integration complexity
- Multiple valid implementation approaches needing evaluation