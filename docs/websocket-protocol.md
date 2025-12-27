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

**Resize message client implementation:** `static/terminal-ui.js:686-697`
```javascript
sendResize() {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        const rows = this.term.rows;
        const cols = this.term.cols;
        const msg = new Uint8Array([
            0x00,
            (rows >> 8) & 0xFF, rows & 0xFF,
            (cols >> 8) & 0xFF, cols & 0xFF
        ]);
        this.ws.send(msg);
    }
}
```

**Terminal input client implementation:** `static/terminal-ui.js:1063-1069`
```javascript
this.term.onData(data => {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        // Convert string to Uint8Array for binary transmission
        const encoder = new TextEncoder();
        this.ws.send(encoder.encode(data));
    }
});
```

**File upload client implementation:** `static/terminal-ui.js:1295-1336` (handleFile method)
```javascript
async handleFile(file) {
    console.log('File dropped:', file.name, file.type, file.size);

    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
        this.showTemporaryStatus('Not connected', 3000);
        return;
    }

    const encoder = new TextEncoder();

    if (this.isTextFile(file)) {
        // Read and paste text directly to terminal
        const text = await this.readFileAsText(file);
        if (text === null) {
            this.showTemporaryStatus(`Error reading: ${file.name}`, 5000);
            return;
        }
        this.ws.send(encoder.encode(text));
        this.showTemporaryStatus(`Pasted: ${file.name} (${this.formatFileSize(text.length)})`);
    } else {
        // Binary file: send as binary upload with 0x01 prefix
        // Format: [0x01, name_len_hi, name_len_lo, ...name_bytes, ...file_data]
        const fileData = await this.readFileAsBinary(file);
        if (fileData === null) {
            this.showTemporaryStatus(`Error reading: ${file.name}`, 5000);
            return;
        }
        const nameBytes = encoder.encode(file.name);
        const nameLen = nameBytes.length;

        // Build the message: 0x01 + 2-byte name length + name + file data
        const message = new Uint8Array(1 + 2 + nameLen + fileData.length);
        message[0] = 0x01; // file upload message type
        message[1] = (nameLen >> 8) & 0xFF; // name length high byte
        message[2] = nameLen & 0xFF; // name length low byte
        message.set(nameBytes, 3);
        message.set(fileData, 3 + nameLen);

        this.ws.send(message);
        this.showTemporaryStatus(`Uploaded: ${file.name} (${this.formatFileSize(file.size)})`);
    }
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

**Broadcast to clients:** `main.go:213-225`
```go
func (s *Session) Broadcast(data []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	for conn := range s.clients {
		if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
			log.Printf("Broadcast write error: %v", err)
		}
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

**Client implementation:** `static/terminal-ui.js:1047-1052`
```javascript
startHeartbeat() {
    this.stopHeartbeat();
    this.heartbeatInterval = setInterval(() => {
        this.sendJSON({type: 'ping', data: {ts: Date.now()}});
    }, 30000); // every 30 seconds
}
```

**Server implementation:** `main.go:807-837`
```go
if messageType == websocket.TextMessage {
    var msg struct {
        Type     string          `json:"type"`
        Data     json.RawMessage `json:"data,omitempty"`
        UserName string          `json:"userName,omitempty"`
        Text     string          `json:"text,omitempty"`
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
        if err := conn.WriteJSON(response); err != nil {
            log.Printf("Failed to send pong: %v", err)
        }
    case "chat":
        // Handle incoming chat message
        if msg.UserName != "" && msg.Text != "" {
            sess.BroadcastChatMessage(msg.UserName, msg.Text)
            log.Printf("Chat message from %s: %s", msg.UserName, msg.Text)
        }
    default:
        log.Printf("Unknown message type: %s", msg.Type)
    }
    continue
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

**Client implementation:** `static/terminal-ui.js:998-1022`
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

    // Send to server
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

**Server implementation:** `main.go:828-833`
```go
case "chat":
    // Handle incoming chat message
    if msg.UserName != "" && msg.Text != "" {
        sess.BroadcastChatMessage(msg.UserName, msg.Text)
        log.Printf("Chat message from %s: %s", msg.UserName, msg.Text)
    }
```

**Server broadcast:** `main.go:260-285`
```go
func (s *Session) BroadcastChatMessage(userName, text string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	chatJSON := map[string]interface{}{
		"type":      "chat",
		"userName":  userName,
		"text":      text,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	data, err := json.Marshal(chatJSON)
	if err != nil {
		log.Printf("BroadcastChatMessage marshal error: %v", err)
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	for conn := range s.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("BroadcastChatMessage write error: %v", err)
		}
	}
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

**Server implementation:** `main.go:228-256` (BroadcastStatus function)
```go
func (s *Session) BroadcastStatus() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, cols := s.calculateMinSize()
	status := map[string]interface{}{
		"type":      "status",
		"viewers":   len(s.clients),
		"cols":      cols,
		"rows":      rows,
		"assistant": s.AssistantConfig.Name,
	}

	data, err := json.Marshal(status)
	if err != nil {
		log.Printf("BroadcastStatus marshal error: %v", err)
		return
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	for conn := range s.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("BroadcastStatus write error: %v", err)
		}
	}
	log.Printf("Session %s: broadcast status (viewers=%d, size=%dx%d)", s.UUID, len(s.clients), cols, rows)
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
- After client connects (`main.go:116`, AddClient method)
- After client disconnects (`main.go:141`, RemoveClient method)
- After terminal is resized (`main.go:167`, UpdateClientSize method)

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

**Snapshot on join:** `main.go:778-791`
```go
// If this is a new session, start the PTY reader goroutine
if isNew {
    sess.startPTYReader()
} else {
    // Send snapshot to catch up the new client with existing screen state
    snapshot := sess.GenerateSnapshot()
    sess.writeMu.Lock()
    err := conn.WriteMessage(websocket.BinaryMessage, snapshot)
    sess.writeMu.Unlock()
    if err != nil {
        log.Printf("Failed to send snapshot: %v", err)
    } else {
        log.Printf("Sent screen snapshot to new client (%d bytes)", len(snapshot))
    }
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
