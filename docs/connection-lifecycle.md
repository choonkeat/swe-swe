# Connection Lifecycle & Disconnection Scenarios

This document describes all WebSocket connection states, disconnection scenarios, and their effects on the user experience.

## Visual States

### Connected State
- **Status bar**: Blue (`#007acc`), text "Connected"
- **Status icon**: Green dot (`#4caf50`), steady
- **Terminal**: Full opacity (100%)
- **Timer**: Shows uptime (e.g., "5m 32s")

**Code reference:** `static/terminal-ui.js` (status bar CSS)
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

**Code reference:** `static/terminal-ui.js` (disconnected and error CSS classes)
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

**Code reference:** `static/terminal-ui.js` (reconnecting CSS class)
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

**Code reference:** `static/terminal-ui.js` (term.onData handler)
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

**Code reference:** `main.go` (handleWebSocket ReadMessage error handling)
```go
messageType, data, err := conn.ReadMessage()
if err != nil {
    if websocket.IsCloseError(err, websocket.CloseNormalClosure, ...) {
        log.Printf("WebSocket closed: %v", err)
    } else {
        log.Printf("WebSocket read error: %v", err)
    }
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

**Code reference:** `static/terminal-ui.js` (onclose handler)
```javascript
this.ws.onclose = () => {
    this.stopUptimeTimer();
    this.stopHeartbeat();
    this.updateStatus('error', 'Connection closed');
    this.scheduleReconnect();
};
```

**Reconnect timing:** 1s → 2s → 4s → 8s → 16s → 32s → 60s (max)

**Code reference:** `static/terminal-ui.js` (getReconnectDelay method)
```javascript
getReconnectDelay() {
    return Math.min(1000 * Math.pow(2, this.reconnectAttempts), 60000);
}
```

---

### 3. Shell Exits (Any Exit Code)

**Trigger:** User types `exit`, shell script completes, process crashes, or is killed

**Server behavior:**
1. PTY read returns EOF or error
2. Check for pending replacement (YOLO toggle):
   - **If pending replacement set**: Start new process with replacement command, continue
   - **If no pending replacement**: Session ends
3. Broadcasts exit message to clients
4. If no clients connected: PTY reader exits silently

**Client behavior:**
- Sees `[Process exited with code X]` in terminal
- Receives `exit` JSON message with exit code
- Browser shows "Session ended" dialog
- User can click OK to return to session selection

**Code reference:** `main.go` (startPTYReader, exit code handling)
```go
n, err := ptyFile.Read(buf)
if err != nil {
    // Get exit code
    exitCode := 0
    if cmd != nil {
        if err := cmd.Wait(); err != nil {
            if exitErr, ok := err.(*exec.ExitError); ok {
                exitCode = exitErr.ExitCode()
            }
        }
    }

    // Check for pending replacement (e.g., YOLO toggle)
    s.mu.Lock()
    replacement := s.pendingReplacement
    s.pendingReplacement = ""
    s.mu.Unlock()

    if replacement != "" {
        // Start replacement process (YOLO toggle case)
        if err := s.RestartProcess(replacement); err != nil {
            // Handle restart failure
        }
        continue  // Don't end session
    }

    // No replacement - session ends
    clientCount := len(s.wsClients)
    if clientCount == 0 {
        return  // No viewers, exit silently
    }

    exitMsg := []byte(fmt.Sprintf("\r\n[Process exited with code %d]\r\n", exitCode))
    s.Broadcast(exitMsg)
    s.BroadcastExit(exitCode)
    return
}
```

**Note:** Process replacement (via pending replacement) is used by the YOLO toggle feature. When YOLO is toggled, the current process is killed and a new one starts with the YOLO flag.

---

### 4. Shell Exits with Error (exit 1, crash, SIGKILL)

**Trigger:** Shell crashes, killed by signal, or exits with non-zero code

**Server behavior:** Same as scenario 3 - all exits end the session (no automatic restart).

**Client behavior:**
- Sees `[Process exited with code X]` in terminal
- Receives `exit` JSON message with exit code
- Browser shows "Session ended" dialog

**Note:** Earlier versions auto-restarted on non-zero exits. Current behavior treats all exits the same - session ends regardless of exit code. Process replacement only happens via explicit user action (YOLO toggle).

---

### 5. Process Replacement Fails (YOLO Toggle)

**Trigger:** YOLO toggle requested but cannot spawn new shell (e.g., shell binary missing, permission denied)

**Server behavior:**
1. Writes error to virtual terminal and broadcasts to all clients
2. Logs error
3. PTY reader goroutine exits (returns)
4. Session ends

**Client behavior:**
- Sees error message in terminal: `[Failed to restart process: <error_details>]`
- Browser shows "Session ended" dialog

**Code reference:** `main.go` (startPTYReader, RestartProcess error handling)
```go
if replacement != "" {
    if err := s.RestartProcess(replacement); err != nil {
        log.Printf("Session %s: failed to start replacement: %v", s.UUID, err)
        errMsg := []byte("\r\n[Failed to restart process: " + err.Error() + "]\r\n")
        s.Broadcast(errMsg)
        s.BroadcastExit(1)
        return
    }
}
```

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

**Code reference:** `main.go` (Session.Close method)
```go
func (s *Session) Close() {
    s.mu.Lock()
    defer s.mu.Unlock()

    for conn := range s.wsClients {
        conn.Close()
    }
    s.wsClients = make(map[*websocket.Conn]bool)

    if s.Cmd != nil && s.Cmd.Process != nil {
        s.Cmd.Process.Kill()
        s.Cmd.Wait()
    }
    if s.PTY != nil {
        s.PTY.Close()
    }
}
```

---

### 8. Session Cleanup (Process Exit)

**Trigger:** Shell process exits and session reaper detects it

**Server behavior:**
1. Session reaper runs every minute
2. Finds sessions where the process has exited (`ProcessState.Exited()`)
3. Calls `sess.Close()` (closes PTY, cleans up resources)
4. Removes session from map

**Note:** Sessions persist until the process exits - there is no TTL-based expiration. Sessions with active processes remain available indefinitely, even with 0 clients connected.

**Client behavior:** N/A (session cleaned up after process exit)

**Code reference:** `main.go` (sessionReaper goroutine)
```go
func sessionReaper() {
    ticker := time.NewTicker(time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        sessionsMu.Lock()
        for uuid, sess := range sessions {
            // Only clean up sessions where the process has exited
            if sess.Cmd != nil && sess.Cmd.ProcessState != nil && sess.Cmd.ProcessState.Exited() {
                log.Printf("Session cleaned up (process exited): %s", uuid)
                sess.Close()
                delete(sessions, uuid)
            }
        }
        sessionsMu.Unlock()
    }
}
```

**After process exit:** If client reconnects to same UUID, a new session is created (fresh shell, no history).

---

### 9. WebSocket Write Error (server-side)

**Trigger:** Client's receive buffer full, network issue on server side

**Server behavior:**
- `conn.WriteMessage()` returns error
- Logged but connection not explicitly closed
- Client may be removed on next read error

**Code reference:** `main.go` (Session.Broadcast write error logging)
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

**Code reference:** `static/terminal-ui.js` (scheduleReconnect method)
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

**Code reference:** `static/terminal-ui.js` (ws.onopen handler)
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
| Shell exit | OPEN → Dialog | Blue "Connected" | 100% | N/A | Ended |
| YOLO toggle | OPEN | Blue "YOLO" | 100% | Sent | Replaced |
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
