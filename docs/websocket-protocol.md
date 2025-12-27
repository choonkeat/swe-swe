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

### Binary Messages (Terminal I/O, File Upload, Terminal Resize)

#### Client → Server

| Bytes | Description |
|-------|-------------|
| `0x00` + 4 bytes | Terminal resize: `[0x00, rows_hi, rows_lo, cols_hi, cols_lo]` |
| `0x01` + 2 bytes + name + data | File upload: `[0x01, name_len_hi, name_len_lo, ...filename_bytes, ...file_data]` |
| Other | Terminal input (keystrokes and raw shell I/O) |

**Resize message client implementation:** `static/terminal-ui.js:258-268`
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

**Terminal input client implementation:** `static/terminal-ui.js:754-758`
```javascript
this.term.onData(data => {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.ws.send(data);
    }
});
```

**File upload client implementation:** `static/terminal-ui.js:1295-1336`
```javascript
sendFileUpload(file) {
    const reader = new FileReader();
    reader.onload = (event) => {
        const data = event.target.result;
        const filename = file.name;
        const nameBytes = new TextEncoder().encode(filename);
        const nameLen = nameBytes.length;

        const msg = new Uint8Array(3 + nameLen + data.byteLength);
        msg[0] = 0x01;  // File upload marker
        msg[1] = (nameLen >> 8) & 0xFF;
        msg[2] = nameLen & 0xFF;
        msg.set(nameBytes, 3);
        msg.set(new Uint8Array(data), 3 + nameLen);

        this.ws.send(msg);
    };
    reader.readAsArrayBuffer(file);
}
```

#### Server → Client

Raw PTY output bytes, sent directly to xterm.js.

**Server binary message handler:** `main.go:841-903`
```go
// Binary messages: resize, file upload, or terminal input
if len(data) >= 5 && data[0] == 0x00 {
    // Terminal resize
    rows := uint16(data[1])<<8 | uint16(data[2])
    cols := uint16(data[3])<<8 | uint16(data[4])
    sess.UpdateClientSize(conn, rows, cols)
    continue
}

if len(data) >= 3 && data[0] == 0x01 {
    // File upload
    nameLen := uint16(data[1])<<8 | uint16(data[2])
    if len(data) < 3+int(nameLen) {
        log.Printf("Invalid file upload message: incomplete name")
        continue
    }
    filename := string(data[3 : 3+nameLen])
    fileData := data[3+nameLen:]
    // Save file and broadcast
    sess.HandleFileUpload(conn, filename, fileData)
    continue
}

// Regular terminal input (keystrokes)
if _, err := sess.PTY.Write(data); err != nil {
    log.Printf("PTY write error: %v", err)
    break
}
```

**Broadcast to clients:** `main.go:142-155`
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

#### status (Session Status Broadcast)

**Server → Client (Sent when client connects/disconnects or terminal resizes):**
```json
{
  "type": "status",
  "viewers": 2,
  "cols": 120,
  "rows": 30,
  "assistant": "claude"
}
```

**Fields:**
- `viewers`: Number of active clients connected to this session
- `cols`: Terminal width in columns
- `rows`: Terminal height in rows
- `assistant`: Name of the detected AI assistant (or empty string if none)

**Server implementation:** `main.go:227-256` (BroadcastStatus function)
```go
func (s *Session) BroadcastStatus() {
    s.mu.RLock()
    clientCount := len(s.clients)
    s.mu.RUnlock()

    status := map[string]interface{}{
        "type":      "status",
        "viewers":   clientCount,
        "cols":      s.ClientSize.Cols,
        "rows":      s.ClientSize.Rows,
        "assistant": s.DetectedAssistant,
    }
    // ... broadcast to all clients
}
```

**Client implementation:** `static/terminal-ui.js:769-778`
```javascript
case 'status':
    if (msg.viewers !== undefined) {
        this.updateViewerCount(msg.viewers);
    }
    if (msg.cols !== undefined && msg.rows !== undefined) {
        // Update terminal size display
        this.updateSizeDisplay(msg.cols, msg.rows);
    }
    if (msg.assistant) {
        this.updateAssistantDisplay(msg.assistant);
    }
    break;
```

**When sent:**
- After client connects (`main.go:115`)
- After client disconnects (`main.go:141`)
- After terminal is resized (`main.go:167`)

---

#### file_upload (File Upload Response)

**Server → Client (Sent after file upload completes):**
```json
{
  "type": "file_upload",
  "success": true,
  "filename": "document.pdf",
  "error": null
}
```

or on error:

```json
{
  "type": "file_upload",
  "success": false,
  "filename": "document.pdf",
  "error": "Failed to save file: permission denied"
}
```

**Fields:**
- `success`: Boolean indicating if file was saved successfully
- `filename`: Name of the uploaded file
- `error`: Error message if `success` is false, null otherwise

**File upload flow:**
1. Client sends binary message with 0x01 prefix containing filename and file data
2. Server parses and saves file to `.swe-swe/uploads/{sanitized_filename}`
3. File path is written to PTY for assistant to access
4. Server sends this JSON response confirming success or error

**Server implementation:** `main.go:937-952` (sendFileUploadResponse function)
```go
func (s *Session) sendFileUploadResponse(conn *websocket.Conn, filename string, success bool, err string) {
    response := map[string]interface{}{
        "type":     "file_upload",
        "success":  success,
        "filename": filename,
    }
    if err != "" {
        response["error"] = err
    } else {
        response["error"] = nil
    }
    conn.WriteJSON(response)
}
```

**Client implementation:** `static/terminal-ui.js:786-793`
```javascript
case 'file_upload':
    if (msg.success) {
        this.showNotification(`File uploaded: ${msg.filename}`);
    } else {
        this.showNotification(`Upload failed: ${msg.error}`, 'error');
    }
    break;
```

**File storage location:**
- Uploaded files are saved to `.swe-swe/uploads/` directory in metadata
- Filenames are sanitized to prevent directory traversal
- File path is printed to PTY so assistant can see where file was saved

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

## Summary of All Message Types

| Type | Direction | Category | Status |
|------|-----------|----------|--------|
| `ping` / `pong` | Client ↔ Server | Control | ✅ Implemented |
| `chat` | Client → Server → Client | Control | ✅ Implemented |
| `status` | Server → Client | Control | ✅ Implemented |
| `file_upload` | Server → Client | Control | ✅ Implemented |
| Terminal resize (0x00) | Client → Server | Binary | ✅ Implemented |
| File upload (0x01) | Client → Server | Binary | ✅ Implemented |
| Terminal I/O | Client ↔ Server | Binary | ✅ Implemented |

## Extensibility

The protocol is designed to be extensible. Future message types could include:

| Type | Direction | Description |
|------|-----------|-------------|
| `notify` | Server → Client | System notifications |
| `cursor` | Server → Client | Other users' cursor positions |
| `typing` | Client → Server | Typing indicator for other users |
| `presence` | Server → Client | User join/leave notifications |
| `sync` | Server → Client | Synchronized state updates |
