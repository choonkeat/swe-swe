# WebSocket Protocol

This document describes the WebSocket protocol used for terminal communication.

## Overview

The protocol uses WebSocket frame types to distinguish between terminal I/O and control messages:

- **Binary frames**: Terminal I/O (input and output)
- **Text frames**: JSON control messages

## Connection

WebSocket endpoint: `/ws/{session-uuid}?assistant={assistant}`

Example: `ws://localhost:1977/ws/abc123-def456?assistant=claude`

The `assistant` query parameter specifies which AI assistant to use (e.g., `claude`, `gemini`, `codex`, `aider`, `goose`, `opencode`).

## Message Types

### Binary Messages (Terminal I/O, File Upload, Terminal Resize)

#### Client → Server

| Bytes | Description |
|-------|-------------|
| `0x00` + 4 bytes | Terminal resize: `[0x00, rows_hi, rows_lo, cols_hi, cols_lo]` |
| `0x01` + 2 bytes + name + data | File upload: `[0x01, name_len_hi, name_len_lo, ...filename_bytes, ...file_data]` |
| `0x02` + index + total + data | Chunked message: `[0x02, chunk_index, total_chunks, ...chunk_data]` (used for snapshots) |
| Other | Terminal input (keystrokes and raw shell I/O) |

**Resize message client implementation:** `static/terminal-ui.js` (sendResize method)
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

**Terminal input client implementation:** `static/terminal-ui.js` (term.onData handler)
```javascript
this.term.onData(data => {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        // Convert string to Uint8Array for binary transmission
        const encoder = new TextEncoder();
        this.ws.send(encoder.encode(data));
    }
});
```

**File upload client implementation:** `static/terminal-ui.js` (handleFile method)
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

- Raw PTY output bytes, sent directly to xterm.js
- Chunked snapshots for joining clients (gzip-compressed, 0x02 prefix)

**Server binary message handler:** `main.go` (handleWebSocket function)
```go
// Check for terminal resize message (0x00 prefix)
if len(data) >= 5 && data[0] == 0x00 {
    rows := uint16(data[1])<<8 | uint16(data[2])
    cols := uint16(data[3])<<8 | uint16(data[4])
    sess.UpdateClientSize(conn, rows, cols)
    continue
}

// Check for file upload message (0x01 prefix)
if len(data) >= 3 && data[0] == 0x01 {
    nameLen := int(data[1])<<8 | int(data[2])
    // ... parse filename and file data
    filename = sanitizeFilename(filename)
    filePath := ".swe-swe/uploads/" + filename
    os.WriteFile(filePath, fileData, 0644)
    sendFileUploadResponse(sess, conn, true, filename, "")
    // Write file path to PTY for assistant to see
    sess.PTY.Write([]byte(absFilePath))
    continue
}

// Regular terminal input (keystrokes)
if _, err := sess.PTY.Write(data); err != nil {
    log.Printf("PTY write error: %v", err)
    break
}
```

#### Chunked Snapshots (Server → Client)

When a client joins an existing session, the server sends a gzip-compressed screen snapshot using chunked messages for iOS Safari compatibility.

**Chunk format:** `[0x02, chunk_index, total_chunks, ...compressed_data]`

- `0x02` - Chunk marker
- `chunk_index` - 0-based index (0-254)
- `total_chunks` - Total number of chunks (1-255)
- `compressed_data` - gzip-compressed ANSI escape sequences

**Server implementation:** `main.go` (sendChunked function)
```go
func sendChunked(conn *websocket.Conn, writeMu *sync.Mutex, data []byte, chunkSize int) (int, error) {
    totalChunks := (len(data) + chunkSize - 1) / chunkSize
    for i := 0; i < totalChunks; i++ {
        chunk := make([]byte, 3+end-start)
        chunk[0] = ChunkMarker  // 0x02
        chunk[1] = byte(i)
        chunk[2] = byte(totalChunks)
        copy(chunk[3:], data[start:end])
        conn.WriteMessage(websocket.BinaryMessage, chunk)
    }
    return totalChunks, nil
}
```

**Broadcast to clients:** `main.go` (Session.Broadcast method)
```go
func (s *Session) Broadcast(data []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	for conn := range s.wsClients {
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

**Client implementation:** `static/terminal-ui.js` (startHeartbeat method)
```javascript
startHeartbeat() {
    this.stopHeartbeat();
    this.heartbeatInterval = setInterval(() => {
        this.sendJSON({type: 'ping', data: {ts: Date.now()}});
    }, 30000); // every 30 seconds
}
```

**Server implementation:** `main.go` (handleWebSocket text message handler)
```go
if messageType == websocket.TextMessage {
    var msg struct {
        Type     string          `json:"type"`
        Data     json.RawMessage `json:"data,omitempty"`
        UserName string          `json:"userName,omitempty"`
        Text     string          `json:"text,omitempty"`
        Name     string          `json:"name,omitempty"`
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
    case "chat":
        if msg.UserName != "" && msg.Text != "" {
            sess.BroadcastChatMessage(msg.UserName, msg.Text)
        }
    case "rename_session":
        // Validate and update session name
        name := strings.TrimSpace(msg.Name)
        // Max 32 chars, alphanumeric + spaces + hyphens + underscores
        sess.Name = name
        sess.BroadcastStatus()
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

**Client implementation:** `static/terminal-ui.js` (sendChatMessage method)
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

**Server implementation:** `main.go` (chat case in handleWebSocket)
```go
case "chat":
    if msg.UserName != "" && msg.Text != "" {
        sess.BroadcastChatMessage(msg.UserName, msg.Text)
    }
```

**Server broadcast:** `main.go` (Session.BroadcastChatMessage method)
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
	// ...
	for conn := range s.wsClients {
		conn.WriteMessage(websocket.TextMessage, data)
	}
}
```

#### status (Session Status Broadcast)

**Server → Client (Sent when client connects/disconnects, terminal resizes, or session is renamed):**
```json
{
  "type": "status",
  "viewers": 2,
  "cols": 120,
  "rows": 30,
  "assistant": "Claude",
  "sessionName": "my-feature",
  "uuidShort": "a3c12"
}
```

**Fields:**
- `viewers`: Number of active clients connected to this session
- `cols`: Terminal width in columns
- `rows`: Terminal height in rows
- `assistant`: Display name of the AI assistant (e.g., "Claude", "Gemini")
- `sessionName`: User-assigned session name (empty string if unnamed)
- `uuidShort`: First 5 characters of session UUID

**Server implementation:** `main.go` (Session.BroadcastStatus method)
```go
func (s *Session) BroadcastStatus() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, cols := s.calculateMinSize()
	uuidShort := s.UUID
	if len(s.UUID) >= 5 {
		uuidShort = s.UUID[:5]
	}
	status := map[string]interface{}{
		"type":        "status",
		"viewers":     len(s.wsClients),
		"cols":        cols,
		"rows":        rows,
		"assistant":   s.AssistantConfig.Name,
		"sessionName": s.Name,
		"uuidShort":   uuidShort,
	}

	data, err := json.Marshal(status)
	// ...
	for conn := range s.wsClients {
		conn.WriteMessage(websocket.TextMessage, data)
	}
}
```

**Client implementation:** `static/terminal-ui.js` (status message handler in ws.onmessage)
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
- After client connects (Session.AddClient method)
- After client disconnects (Session.RemoveClient method)
- After terminal is resized (Session.UpdateClientSize method)
- After session is renamed (rename_session case in handleWebSocket)

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

**Server implementation:** `main.go` (sendFileUploadResponse function)
```go
func sendFileUploadResponse(sess *Session, conn *websocket.Conn, success bool, filename, errMsg string) {
    response := map[string]interface{}{
        "type":    "file_upload",
        "success": success,
    }
    if filename != "" {
        response["filename"] = filename
    }
    if errMsg != "" {
        response["error"] = errMsg
    }
    sess.writeMu.Lock()
    conn.WriteJSON(response)
    sess.writeMu.Unlock()
}
```

**Client implementation:** `static/terminal-ui.js` (file_upload message handler in ws.onmessage)
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
- Uploaded files are saved to `.swe-swe/uploads/` directory in the workspace
- Filenames are sanitized to prevent directory traversal
- File path is printed to PTY so assistant can see where file was saved

---

#### rename_session (Session Rename)

**Client → Server:**
```json
{
  "type": "rename_session",
  "name": "my-feature"
}
```

**Fields:**
- `name`: New session name (max 32 chars, alphanumeric + spaces + hyphens + underscores)

**Server behavior:**
1. Validates name (max 32 chars, allowed characters)
2. Updates session name
3. Broadcasts updated `status` message to all clients

**Server implementation:** `main.go` (rename_session case in handleWebSocket)
```go
case "rename_session":
    name := strings.TrimSpace(msg.Name)
    if len(name) > 32 {
        continue // reject
    }
    // Validate characters
    for _, r := range name {
        if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
             (r >= '0' && r <= '9') || r == ' ' || r == '-' || r == '_') {
            continue // reject
        }
    }
    sess.Name = name
    sess.BroadcastStatus()
```

---

#### exit (Process Exit Notification)

**Server → Client (Sent when shell process exits cleanly with code 0):**
```json
{
  "type": "exit",
  "exitCode": 0
}
```

**Fields:**
- `exitCode`: The exit code of the process (0 for success)

**When sent:**
- When the shell process exits with code 0 (success)
- NOT sent for non-zero exits (process auto-restarts instead)

**Server implementation:** `main.go` (Session.BroadcastExit method)
```go
func (s *Session) BroadcastExit(exitCode int) {
    exitJSON := map[string]interface{}{
        "type":     "exit",
        "exitCode": exitCode,
    }
    data, _ := json.Marshal(exitJSON)
    for conn := range s.wsClients {
        conn.WriteMessage(websocket.TextMessage, data)
    }
}
```

**Client behavior:** Can prompt user to start a new session or reconnect.

---

## Session Lifecycle

1. **Connect**: Client connects to `/ws/{uuid}?assistant={assistant}`
2. **Snapshot** (existing session): Server sends gzip-compressed screen snapshot as chunked binary messages
3. **Resize**: Client sends resize message
4. **Heartbeat**: Client sends ping every 30s
5. **Terminal I/O**: Bidirectional binary messages
6. **Rename**: Client can rename session, server broadcasts status
7. **Disconnect**: WebSocket closes, client auto-reconnects

**Snapshot on join:** `main.go` (handleWebSocket, after AddClient)
```go
// If this is a new session, start the PTY reader goroutine
if isNew {
    sess.startPTYReader()
} else {
    // Send snapshot to catch up the new client with existing screen state
    // Snapshot is gzip-compressed, sent as chunked messages for iOS Safari compatibility
    snapshot := sess.GenerateSnapshot()
    numChunks, err := sendChunked(conn, &sess.writeMu, snapshot, DefaultChunkSize)
    if err != nil {
        log.Printf("Failed to send snapshot chunks: %v", err)
    } else {
        log.Printf("Sent screen snapshot (%d bytes in %d chunks)", len(snapshot), numChunks)
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
| `rename_session` | Client → Server | Control | ✅ Implemented |
| `exit` | Server → Client | Control | ✅ Implemented |
| Terminal resize (0x00) | Client → Server | Binary | ✅ Implemented |
| File upload (0x01) | Client → Server | Binary | ✅ Implemented |
| Chunked message (0x02) | Server → Client | Binary | ✅ Implemented |
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
