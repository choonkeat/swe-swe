# WebSocket Protocol Refactor - Hybrid Binary/Text Messages

**Goal:** Refactor WebSocket protocol to use WebSocket frame types for message differentiation:
- **Binary frames** = Terminal I/O (fast, unchanged)
- **Text frames** = JSON messages (chat, metadata, future features)

**Current Protocol:**
- Server → Client: Raw PTY bytes (binary)
- Client → Server: `0x00` prefix = resize, otherwise raw terminal input

**New Protocol:**
- Binary frames: Terminal I/O (unchanged)
- Text frames: JSON with `{"type": "...", ...}` structure

---

## Progress Tracking

| Step | Description | Status | Commit |
|------|-------------|--------|--------|
| 1 | Add JSON message handler on server | ✅ | `feat: add JSON message handler for WebSocket` |
| 2 | Add JSON message sender on client | ✅ | `feat: add JSON message handling on client` |
| 3 | Implement ping/pong heartbeat | ✅ | `feat: add heartbeat ping/pong` |
| 4 | Test backward compatibility | ✅ | (tested) |

---

## Step 1: Add JSON Message Handler on Server ✅

**Goal:** Server handles text (JSON) WebSocket messages separately from binary terminal I/O.

**Changes to `main.go`:**

1. In `handleWebSocket()`, check message type after `conn.ReadMessage()`:
   - `websocket.TextMessage` → parse JSON, route by type
   - `websocket.BinaryMessage` → existing terminal/resize logic

2. Define message types:
   ```go
   type WSMessage struct {
       Type string          `json:"type"`
       Data json.RawMessage `json:"data,omitempty"`
   }
   ```

3. Handle "ping" message type (for testing):
   ```go
   case "ping":
       conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"pong"}`))
   ```

**Test:**
- Start server
- Use browser devtools to send: `ws.send('{"type":"ping"}')`
- Verify response: `{"type":"pong"}`
- Verify terminal I/O still works (binary)

**Files changed:**
- `main.go`

---

## Step 2: Add JSON Message Sender on Client ✅

**Goal:** Client can send and receive JSON messages alongside binary terminal I/O.

**Changes to `static/terminal-ui.js`:**

1. Add method to send JSON messages:
   ```javascript
   sendJSON(obj) {
       if (this.ws && this.ws.readyState === WebSocket.OPEN) {
           this.ws.send(JSON.stringify(obj));
       }
   }
   ```

2. Update `onmessage` handler to distinguish text vs binary:
   ```javascript
   ws.onmessage = (event) => {
       if (event.data instanceof ArrayBuffer) {
           // Binary = terminal output
           this.term.write(new Uint8Array(event.data));
       } else if (typeof event.data === 'string') {
           // Text = JSON message
           this.handleJSONMessage(JSON.parse(event.data));
       }
   };
   ```

3. Add JSON message handler:
   ```javascript
   handleJSONMessage(msg) {
       console.log('JSON message:', msg);
       // Future: handle chat, notifications, etc.
   }
   ```

**Test:**
- Open terminal in browser
- Verify terminal I/O works
- In devtools console: `document.querySelector('terminal-ui').sendJSON({type:'ping'})`
- Verify pong response in console log

**Files changed:**
- `static/terminal-ui.js`

---

## Step 3: Implement Ping/Pong Heartbeat ✅

**Goal:** Use the new JSON protocol for connection health monitoring.

**Changes to `static/terminal-ui.js`:**

1. Add heartbeat on connect:
   ```javascript
   startHeartbeat() {
       this.heartbeatInterval = setInterval(() => {
           this.sendJSON({type: 'ping', ts: Date.now()});
       }, 30000); // every 30s
   }
   ```

2. Handle pong response:
   ```javascript
   handleJSONMessage(msg) {
       if (msg.type === 'pong') {
           console.log('Heartbeat OK');
       }
   }
   ```

3. Stop heartbeat on disconnect.

**Changes to `main.go`:**

1. Echo back timestamp in pong:
   ```go
   case "ping":
       response := map[string]interface{}{"type": "pong", "ts": msg.Data}
       conn.WriteJSON(response)
   ```

**Test:**
- Open terminal, wait 30+ seconds
- Verify heartbeat pings/pongs in logs
- Verify terminal remains responsive

**Files changed:**
- `main.go`
- `static/terminal-ui.js`

---

## Step 4: Test Backward Compatibility ✅

**Goal:** Verify all existing functionality works.

**Test scenarios:**
1. Single client terminal I/O
2. Multiplayer session sync (snapshot on join)
3. Terminal resize
4. Process restart
5. Reconnect after disconnect

**Polish:**
- Add logging for JSON message handling
- Document the protocol in code comments

**Files changed:**
- None (testing only)

---

## Message Type Reference

| Type | Direction | Description |
|------|-----------|-------------|
| `ping` | Client → Server | Heartbeat request |
| `pong` | Server → Client | Heartbeat response |
| `chat` | Both | Future: in-session chat |
| `notify` | Server → Client | Future: notifications |

---

## Notes

- Binary frames for terminal I/O = no performance impact
- Text frames for control messages = easy to extend
- WebSocket naturally distinguishes frame types
- No breaking changes to existing terminal functionality
