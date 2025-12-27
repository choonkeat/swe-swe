# Connection Lifecycle & Disconnection Scenarios

This document describes all WebSocket connection states, disconnection scenarios, and their effects on the user experience.

## Visual States

### Connected State
- **Status bar**: Blue (`#007acc`), text "Connected"
- **Status icon**: Green dot (`#4caf50`), steady
- **Terminal**: Full opacity (100%)
- **Timer**: Shows uptime (e.g., "5m 32s")

**Code reference:** `static/terminal-ui.js:117-121`
```css
.terminal-ui__status-bar {
    background: #007acc;
}
```

### Error/Disconnected State
- **Status bar**: Red (`#d32f2f`), text "Connection closed" or "Connection error"
- **Status icon**: White dot, pulsing animation
- **Terminal**: Faded to 50% opacity
- **Timer**: Cleared (empty)

**Code reference:** `static/terminal-ui.js:72-74, 123-124`
```css
.terminal-ui__terminal.disconnected {
    opacity: 0.5;
}
.terminal-ui__status-bar.error {
    background: #d32f2f;
}
```

### Reconnecting State
- **Status bar**: Orange (`#f57c00`), text "Reconnecting in Xs..."
- **Status icon**: White dot, pulsing animation
- **Terminal**: Faded to 50% opacity
- **Timer**: Shows countdown to next reconnect attempt

**Code reference:** `static/terminal-ui.js:126-128`
```css
.terminal-ui__status-bar.reconnecting {
    background: #f57c00;
}
```

---

## User Input During Disconnection

### Can the user type?
**Yes**, but keystrokes are **silently dropped**.

The terminal input handler checks WebSocket state before sending:

**Code reference:** `static/terminal-ui.js:362-367`
```javascript
this.term.onData(data => {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        const encoder = new TextEncoder();
        this.ws.send(encoder.encode(data));
    }
    // If not OPEN, data is silently discarded
});
```

### Will the user know it doesn't work?
**Partially:**
1. **Visual cues**: Terminal fades to 50% opacity, status bar turns red/orange
2. **Status text**: Shows "Connection closed" or "Reconnecting in Xs..."
3. **No explicit feedback**: Keystrokes don't produce any error message or visual indication that they were dropped

**Potential improvement**: Could show a toast notification or buffer keystrokes for replay after reconnect.

---

## Disconnection Scenarios

### 1. Client Closes Browser Tab/Window

**Trigger:** User closes tab, navigates away, or browser crashes

**Server behavior:**
- WebSocket read returns error: `websocket: close 1001 (going away)`
- `handleWebSocket()` breaks out of read loop
- `defer sess.RemoveClient(conn)` executes
- Client removed from session, PTY size recalculated

**Client behavior:** N/A (client is gone)

**Code reference:** `main.go:521-524`
```go
messageType, data, err := conn.ReadMessage()
if err != nil {
    log.Printf("WebSocket read error: %v", err)
    break
}
```

---

### 2. Network Disconnection (WiFi drop, cable unplug)

**Trigger:** Network connectivity lost

**Server behavior:**
- Eventually detects via WebSocket read/write error
- May take time due to TCP keepalive settings
- Client removed from session when error detected

**Client behavior:**
1. `ws.onclose` fires (may be delayed)
2. Status → "Connection closed" (red)
3. Terminal fades to 50%
4. Heartbeat stops
5. Auto-reconnect scheduled with exponential backoff

**Code reference:** `static/terminal-ui.js:312-317`
```javascript
this.ws.onclose = () => {
    this.stopUptimeTimer();
    this.stopHeartbeat();
    this.updateStatus('error', 'Connection closed');
    this.scheduleReconnect();
};
```

**Reconnect timing:** 1s → 2s → 4s → 8s → 16s → 32s → 60s (max)

**Code reference:** `static/terminal-ui.js:234-236`
```javascript
getReconnectDelay() {
    return Math.min(1000 * Math.pow(2, this.reconnectAttempts), 60000);
}
```

---

### 3. Shell Exits Normally (exit 0)

**Trigger:** User types `exit`, shell script completes, or process terminates cleanly

**Server behavior:**
1. PTY read returns EOF
2. If clients connected: broadcasts `[Process exited, restarting...]`
3. Waits 500ms, then restarts shell
4. If no clients: PTY reader exits, session remains until TTL expires

**Client behavior:**
- Sees `[Process exited, restarting...]` in terminal
- WebSocket remains connected
- New shell prompt appears after restart
- No status bar change (still "Connected")

**Code reference:** `main.go:284-320`
```go
n, err := ptyFile.Read(buf)
if err != nil {
    // Process has died - check if we should restart
    if clientCount == 0 {
        log.Printf("Session %s: process died with no clients, not restarting", s.UUID)
        return
    }
    // Notify clients of restart
    restartMsg := []byte("\r\n[Process exited, restarting...]\r\n")
    s.Broadcast(restartMsg)
    // Wait a bit before restarting
    time.Sleep(500 * time.Millisecond)
    s.RestartProcess(shellRestartCmd)
}
```

---

### 4. Shell Exits with Error (exit 1, crash, SIGKILL)

**Trigger:** Shell crashes, killed by signal, or exits with non-zero code

**Server behavior:** Same as normal exit - process is restarted automatically

**Client behavior:** Same as normal exit - sees restart message, new prompt

**Note:** The server doesn't distinguish between exit 0 and exit 1. Both trigger restart.

---

### 5. Shell Restart Fails

**Trigger:** Cannot spawn new shell (e.g., shell binary missing, permission denied)

**Server behavior:**
1. Broadcasts `[Failed to restart process: <error>]`
2. PTY reader goroutine exits
3. Session remains in memory but is "dead"
4. WebSocket connections remain open but no shell I/O

**Client behavior:**
- Sees error message in terminal
- WebSocket stays connected
- Status bar still shows "Connected"
- User input is written to dead PTY (no effect)

**Code reference:** `main.go:310-318`
```go
if err := s.RestartProcess(shellRestartCmd); err != nil {
    log.Printf("Session %s: failed to restart process: %v", s.UUID, err)
    errMsg := []byte("\r\n[Failed to restart process: " + err.Error() + "]\r\n")
    s.Broadcast(errMsg)
    return // PTY reader exits
}
```

**Potential issue:** Client thinks it's connected but shell is dead. Could improve by sending a JSON error message and/or closing WebSocket.

---

### 6. Server Process Killed (SIGTERM, SIGKILL, crash)

**Trigger:** `kill <pid>`, server panic, OOM kill, deployment

**Server behavior:** Immediate termination, no cleanup

**Client behavior:**
1. `ws.onclose` fires immediately
2. Status → "Connection closed" (red)
3. Terminal fades to 50%
4. Auto-reconnect starts
5. If server restarts quickly: reconnects to new session (old session state lost)
6. If server stays down: continues retry with exponential backoff

---

### 7. Server Closes WebSocket Intentionally

**Trigger:** Session cleanup, admin action, or programmatic close

**Server behavior:**
- Calls `conn.Close()` on WebSocket
- Client removed from session

**Client behavior:** Same as network disconnection - auto-reconnect triggered

**Code reference:** `main.go:159-163` (Session.Close method)
```go
for conn := range s.clients {
    conn.Close()
}
s.clients = make(map[*websocket.Conn]bool)
```

---

### 8. Session TTL Expiration

**Trigger:** Session has 0 clients for longer than TTL (default: 1 hour)

**Server behavior:**
1. Session reaper runs every minute
2. Finds sessions with 0 clients idle > TTL
3. Calls `sess.Close()` (kills process, closes PTY)
4. Removes session from map

**Client behavior:** N/A (no clients connected)

**Code reference:** `main.go:430-438`
```go
for uuid, sess := range sessions {
    if sess.ClientCount() == 0 && time.Since(sess.LastActive()) > sessionTTL {
        log.Printf("Session expired: %s (idle for %v)", uuid, time.Since(sess.LastActive()))
        sess.Close()
        delete(sessions, uuid)
    }
}
```

**After TTL:** If client reconnects to same UUID, a new session is created (fresh shell, no history).

---

### 9. WebSocket Write Error (server-side)

**Trigger:** Client's receive buffer full, network issue on server side

**Server behavior:**
- `conn.WriteMessage()` returns error
- Logged but connection not explicitly closed
- Client may be removed on next read error

**Code reference:** `main.go:148-151`
```go
if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
    log.Printf("Broadcast write error: %v", err)
}
```

---

## Reconnection Behavior

### Automatic Reconnection

1. On `ws.onclose` or `ws.onerror`, client schedules reconnect
2. Exponential backoff: 1s, 2s, 4s, 8s, 16s, 32s, 60s (max)
3. Countdown shown in status bar
4. On successful reconnect:
   - Status → "Connected" (blue)
   - Timer resets to 0s
   - Heartbeat restarts
   - If same session exists: receives screen snapshot
   - If session expired: new session created (fresh shell)

**Code reference:** `static/terminal-ui.js:238-256`
```javascript
scheduleReconnect() {
    const delay = this.getReconnectDelay();
    this.reconnectAttempts++;
    let remaining = Math.ceil(delay / 1000);
    this.updateStatus('reconnecting', `Reconnecting in ${remaining}s...`);
    // Countdown interval...
    this.reconnectTimeout = setTimeout(() => {
        clearInterval(this.countdownInterval);
        this.connect();
    }, delay);
}
```

### Reconnect Counter Reset

Counter resets to 0 on successful connection:

**Code reference:** `static/terminal-ui.js:290`
```javascript
this.ws.onopen = () => {
    this.reconnectAttempts = 0;
    // ...
};
```

---

## Summary Table

| Scenario | WebSocket | Status Bar | Terminal | User Input | Shell |
|----------|-----------|------------|----------|------------|-------|
| Connected | OPEN | Blue "Connected" | 100% | Sent | Running |
| Network drop | CLOSED | Red "Connection closed" | 50% | Dropped | Running (server) |
| Reconnecting | CONNECTING | Orange "Reconnecting..." | 50% | Dropped | Running (server) |
| Shell exit | OPEN | Blue "Connected" | 100% | Sent | Restarting |
| Shell restart fail | OPEN | Blue "Connected" | 100% | Dead | Dead |
| Server killed | CLOSED | Red "Connection closed" | 50% | Dropped | Dead |
| Tab closed | N/A | N/A | N/A | N/A | Running (server) |

---

## Potential Improvements

1. **Keystroke feedback**: Show visual indicator when keystrokes are dropped
2. **Keystroke buffering**: Buffer keystrokes during brief disconnects, replay on reconnect
3. **Dead shell detection**: Send JSON error when shell can't restart, close WebSocket
4. **Connection quality indicator**: Show latency from heartbeat in status bar
5. **Manual reconnect button**: Allow user to trigger immediate reconnect
6. **Session state indicator**: Show if session is new vs restored after reconnect
