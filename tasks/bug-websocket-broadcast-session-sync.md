# Bug: WebSocket Broadcast Session Synchronization

## Issue Description
WebSocket broadcast functionality does not properly synchronize messages across browser tabs sharing the same Claude session ID.

## Current Behavior
When multiple browser tabs visit the same URL containing a Claude session ID in the URL hash fragment, they do not receive synchronized WebSocket messages.

## Expected Behavior
1. **Session-based Broadcasting**: All clients with the same Claude session ID should receive the same incoming WebSocket messages
2. **Dynamic Session Following**: If the Claude session ID changes in the URL hash, all connected clients should:
   - Update their session association
   - Begin receiving messages for the new session
   - Stop receiving messages from the old session

## Technical Requirements
- Locate all clients by their Claude session ID (extracted from URL hash fragment)
- Broadcast messages to all clients sharing the same session
- Handle session ID changes dynamically without reconnection
- Maintain session synchronization across multiple browser tabs

## Use Cases
- Multiple tabs for same coding session
- Collaborative viewing of same session
- Session continuity when URL hash changes

## Priority
Medium - Affects multi-tab user experience and session continuity