# WebSocket Protocol

This document describes the WebSocket protocol used for terminal communication.

## Overview

The protocol uses WebSocket frame types to distinguish between terminal I/O and control messages:

- **Binary frames**: Terminal I/O (input and output)
- **Text frames**: JSON control messages

## Connection

WebSocket endpoint: `/ws/{session-uuid}`

Example: `ws://localhost:9898/ws/abc123-def456`

## Message Types

### Binary Messages (Terminal I/O)

#### Client → Server

| Bytes | Description |
|-------|-------------|
| `0x00` + 4 bytes | Resize: `[0x00, rows_hi, rows_lo, cols_hi, cols_lo]` |
| Other | Terminal input (keystrokes) |

**Implementation:** `static/terminal-ui.js:357-363`
```javascript
this.term.onData(data => {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        const encoder = new TextEncoder();
        this.ws.send(encoder.encode(data));
    }
});
```

**Resize message:** `static/terminal-ui.js:258-268`
```javascript
sendResize() {
    const rows = this.term.rows;
    const cols = this.term.cols;
    const msg = new Uint8Array([
        0x00,
        (rows >> 8) & 0xFF, rows & 0xFF,
        (cols >> 8) & 0xFF, cols & 0xFF
    ]);
    this.ws.send(msg);
}
```

#### Server → Client

Raw PTY output bytes, sent directly to xterm.js.

**Implementation:** `main.go:553-566`
```go
// Handle binary messages (terminal I/O)
if len(data) >= 5 && data[0] == 0x00 {
    rows := uint16(data[1])<<8 | uint16(data[2])
    cols := uint16(data[3])<<8 | uint16(data[4])
    sess.UpdateClientSize(conn, rows, cols)
    continue
}
// Regular terminal input
if _, err := sess.PTY.Write(data); err != nil {
    log.Printf("PTY write error: %v", err)
    break
}
```

**Broadcast to clients:** `main.go:144-152`
```go
func (s *Session) Broadcast(data []byte) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    for conn := range s.clients {
        conn.WriteMessage(websocket.BinaryMessage, data)
    }
}
```

### Text Messages (JSON Control)

All text messages are JSON objects with a `type` field.

#### ping / pong (Heartbeat)

**Client → Server:**
```json
{"type": "ping", "data": {"ts": 1703318400000}}
```

**Server → Client:**
```json
{"type": "pong", "data": {"ts": 1703318400000}}
```

**Client implementation:** `static/terminal-ui.js:343-348`
```javascript
startHeartbeat() {
    this.stopHeartbeat();
    this.heartbeatInterval = setInterval(() => {
        this.sendJSON({type: 'ping', data: {ts: Date.now()}});
    }, 30000);
}
```

**Server implementation:** `main.go:527-550`
```go
if messageType == websocket.TextMessage {
    var msg struct {
        Type string          `json:"type"`
        Data json.RawMessage `json:"data,omitempty"`
    }
    if err := json.Unmarshal(data, &msg); err != nil {
        log.Printf("Invalid JSON message: %v", err)
        continue
    }
    switch msg.Type {
    case "ping":
        response := map[string]interface{}{"type": "pong"}
        if msg.Data != nil {
            response["data"] = msg.Data
        }
        conn.WriteJSON(response)
    }
}
```

#### chat (In-Session Chat)

**Client → Server (Send message):**
```json
{
  "type": "chat",
  "userName": "Alice",
  "text": "Hello everyone!"
}
```

**Server → Client (Broadcast to all):**
```json
{
  "type": "chat",
  "userName": "Alice",
  "text": "Hello everyone!",
  "timestamp": "2025-12-24T16:03:13Z"
}
```

**Client implementation:** `static/terminal-ui.js:829-853`
```javascript
sendChatMessage() {
    const input = this.querySelector('.terminal-ui__chat-input');
    if (!input) return;

    const text = input.value.trim();
    if (!text) return;

    // If no username, prompt for one
    if (!this.currentUserName) {
        const userName = this.getUserName();
        if (!userName) return;
    }

    // Send to server (relies on server broadcast for display)
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.sendJSON({
            type: 'chat',
            userName: this.currentUserName,
            text: text
        });
        // Clear input for next message
        input.value = '';
        input.focus();
    }
}
```

**Server implementation:** `main.go:894-899`
```go
case "chat":
    // Handle incoming chat message
    if msg.UserName != "" && msg.Text != "" {
        sess.AddChatMessage(msg.UserName, msg.Text)
        log.Printf("Chat message from %s: %s", msg.UserName, msg.Text)
    }
```

**Server broadcast:** `main.go:264-283`
```go
func (s *Session) AddChatMessage(userName, text string) {
    msg := ChatMessage{
        UserName:  userName,
        Text:      text,
        Timestamp: time.Now(),
    }
    s.chatMessages = append(s.chatMessages, msg)
    if len(s.chatMessages) > 10 {
        s.chatMessages = s.chatMessages[len(s.chatMessages)-10:]
    }
    s.broadcastChat(msg)
}
```

#### chat_history (Chat History on Connection)

**Server → Client (Sent when new client joins):**
```json
{
  "type": "chat_history",
  "messages": [
    {
      "userName": "Alice",
      "text": "First message",
      "timestamp": "2025-12-24T16:00:00Z"
    },
    {
      "userName": "Bob",
      "text": "Second message",
      "timestamp": "2025-12-24T16:01:00Z"
    }
  ]
}
```

**Client implementation:** `static/terminal-ui.js:609-618`
```javascript
case 'chat_history':
    // Chat history on connection
    if (msg.messages && Array.isArray(msg.messages)) {
        msg.messages.forEach(histMsg => {
            if (histMsg.userName && histMsg.text) {
                const isOwn = histMsg.userName === this.currentUserName;
                this.addChatMessage(histMsg.userName, histMsg.text, isOwn);
            }
        });
    }
    break;
```

**Server implementation:** `main.go:326-356`
```go
func (s *Session) SendChatHistory(conn *websocket.Conn) {
    history := s.GetChatHistory()
    messages := make([]map[string]interface{}, len(history))
    for i, msg := range history {
        messages[i] = map[string]interface{}{
            "userName":  msg.UserName,
            "text":      msg.Text,
            "timestamp": msg.Timestamp.Format(time.RFC3339),
        }
    }
    // ... marshal and send
}
```

**Sent automatically on join:** `main.go:121-122`
```go
// Send chat history to new client
go s.SendChatHistory(conn)
```

## Session Lifecycle

1. **Connect**: Client connects to `/ws/{uuid}`
2. **Snapshot** (existing session): Server sends screen snapshot as binary
3. **Resize**: Client sends resize message
4. **Heartbeat**: Client sends ping every 30s
5. **Terminal I/O**: Bidirectional binary messages
6. **Disconnect**: WebSocket closes, client auto-reconnects

**Snapshot on join:** `main.go:503-511`
```go
if isNew {
    sess.startPTYReader()
} else {
    snapshot := sess.GenerateSnapshot()
    conn.WriteMessage(websocket.BinaryMessage, snapshot)
}
```

## Future Message Types

The protocol is extensible. Planned types:

| Type | Direction | Description |
|------|-----------|-------------|
| `notify` | Server → Client | System notifications |
| `cursor` | Server → Client | Other users' cursor positions |
| `typing` | Client → Server | Typing indicator for other users |
