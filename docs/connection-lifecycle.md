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

**Trigger:** User types `exit`, shell script completes, or process terminates cleanly with exit code 0

**Server behavior:**
1. PTY read returns EOF or error
2. Check exit code:
   - **Exit code 0** (success): Process is NOT restarted, session remains
   - **Non-zero exit code** (error): Process is restarted after 500ms
3. Broadcasts appropriate message to clients
4. If no clients connected: PTY reader exits immediately

**Client behavior on exit 0:**
- Sees `[Process exited successfully]` in terminal
- WebSocket remains connected
- Shell does NOT restart
- User can start a new command or session
- No status bar change (still "Connected")

**Client behavior on non-zero exit:**
- Sees `[Process exited with code X, restarting...]` in terminal
- WebSocket remains connected
- New shell prompt appears after 500ms restart
- No status bar change (still "Connected")

**Code reference:** `main.go:397-462` (PTY reader with exit code checking)
```go
n, err := ptyFile.Read(buf)
if err != nil {
    clientCount := sess.ClientCount()
    if clientCount == 0 {
        log.Printf("Session %s: process died with no clients, not restarting", s.UUID)
        return
    }

    // Check exit code to determine restart behavior
    var exitCode int
    if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
        exitCode = status.ExitStatus()
    }

    if exitCode == 0 {
        // Successful exit - don't restart
        successMsg := []byte("\r\n[Process exited successfully]\r\n")
        s.vtMu.Lock()
        s.vt.Write(successMsg)
        s.vtMu.Unlock()
        s.Broadcast(successMsg)
        return
    }

    // Non-zero exit - restart with message
    restartMsg := []byte(fmt.Sprintf("\r\n[Process exited with code %d, restarting...]\r\n", exitCode))
    s.vtMu.Lock()
    s.vt.Write(restartMsg)
    s.vtMu.Unlock()
    s.Broadcast(restartMsg)

    // Wait before restarting
    time.Sleep(500 * time.Millisecond)
    if err := s.RestartProcess(s.AssistantConfig.ShellRestartCmd); err != nil {
        // ... handle restart failure (see scenario 5)
    }
}
```

**Key Difference from Prior Behavior:** Earlier versions restarted on all exits. Current version only restarts on non-zero exit codes, allowing sessions to terminate cleanly on successful exit.

---

### 4. Shell Exits with Error (exit 1, crash, SIGKILL)

**Trigger:** Shell crashes, killed by signal, or exits with non-zero code

**Server behavior:**
1. Detects non-zero exit code
2. Broadcasts `[Process exited with code X, restarting...]` message
3. Waits 500ms
4. Restarts shell automatically
5. Sends new prompt

**Client behavior:**
- Sees `[Process exited with code X, restarting...]` in terminal
- WebSocket remains connected
- New shell prompt appears after 500ms
- No status bar change (still "Connected")

**Note:** The server treats exit 0 and non-zero exits differently (see scenario 3). Exit 0 does NOT trigger restart, while non-zero exits always restart.

---

### 5. Shell Restart Fails

**Trigger:** Cannot spawn new shell (e.g., shell binary missing, permission denied)

**Server behavior:**
1. Writes error to virtual terminal and broadcasts to all clients
2. Logs error
3. PTY reader goroutine exits (returns)
4. Session remains in memory but is "dead" (no subprocess)
5. WebSocket connections remain open

**Client behavior:**
- Sees error message in terminal: `[Failed to restart process: <error_details>]`
- WebSocket stays connected
- Status bar still shows "Connected"
- User input keystrokes are accepted but discarded (PTY is dead)
- No visual indication that shell is dead (potential UX issue)

**Code reference:** `main.go:451-459` (PTY reader error handling)
```go
if err := s.RestartProcess(s.AssistantConfig.ShellRestartCmd); err != nil {
    log.Printf("Session %s: failed to restart process: %v", s.UUID, err)
    errMsg := []byte("\r\n[Failed to restart process: " + err.Error() + "]\r\n")
    s.vtMu.Lock()
    s.vt.Write(errMsg)
    s.vtMu.Unlock()
    s.Broadcast(errMsg)
    return  // PTY reader exits, session becomes dead
}
```

**Known Issue:** Client appears "Connected" even though shell is dead. The session shows as healthy while it cannot process any input. Keystrokes are silently discarded because PTY is no longer reading/writing.

**Potential Improvements:**
- Send JSON error message instead of (or in addition to) broadcast
- Close WebSocket to force client reconnection
- Set a "session dead" flag to reject further input attempts

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

**Code reference:** `main.go:675-692` (sessionReaper goroutine)
```go
func sessionReaper() {
    ticker := time.NewTicker(time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        sessionsMu.Lock()
        for uuid, sess := range sessions {
            if sess.ClientCount() == 0 && time.Since(sess.LastActive()) > sessionTTL {
                log.Printf("Session expired: %s (idle for %v)", uuid, time.Since(sess.LastActive()))
                sess.Close()
                delete(sessions, uuid)
            }
        }
        sessionsMu.Unlock()
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
