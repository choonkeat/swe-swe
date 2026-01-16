# HTTP Polling Fallback for xterm.js Terminal

## Goal

Add HTTP polling fallback for xterm.js terminal to handle flaky mobile WebSocket connections. When WebSocket is healthy, use it for real-time streaming. When WebSocket fails, fall back to snapshot-based polling with batch input via textarea.

## Design Summary

- **WebSocket (primary)**: Real-time byte stream, smooth experience
- **Polling (fallback)**: Snapshot every 250ms, batch input via textarea
- **Hot standby**: Polling client registered on connect, activates immediately on WS failure
- **No deduplication needed**: Snapshot is complete screen state, WS reconnect sends snapshot

## Phases

### Phase 1: Server - Polling Endpoints

**What will be achieved**: Server gains two new HTTP endpoints for polling-based terminal access.

**Steps**:

1. Add `PollingClient` struct in main.go:
   ```go
   type PollingClient struct {
       ID        string
       SessionID string
       Size      TermSize
       LastPoll  time.Time
   }
   ```

2. Extend `Session` struct:
   - Add `pollClients map[string]*PollingClient`
   - Initialize in `getOrCreateSession()`

3. Add `GET /session/{sessionUUID}/client/{clientId}/poll` handler:
   - Get or validate session exists
   - Register/update polling client, set `LastPoll = now`
   - Call `GenerateSnapshot()`
   - Return JSON: `{ terminal (base64), viewers, cols, rows, assistant }`

4. Add `POST /session/{sessionUUID}/client/{clientId}/send` handler:
   - Validate session and client exist
   - Parse JSON body: `{ type: "input"|"resize", data: string }`
   - For "input": write to PTY
   - For "resize": update polling client's Size, recalculate PTY size
   - Return 200 OK

5. Add polling client reaper (in existing `sessionReaper` goroutine):
   - Remove polling clients with `LastPoll` > 60 seconds ago

**Verification**:

- Red: `curl /session/{uuid}/client/test123/poll?assistant=claude` returns 404
- Green:
  - Poll endpoint returns JSON with: `terminal` (non-empty base64), `viewers` (>= 1), `cols`, `rows`, `assistant`
  - Send endpoint returns 200
  - Subsequent poll returns different (longer) `terminal` base64 string

---

### Phase 2: Server - Viewer Count Integration

**What will be achieved**: Polling clients are properly counted in `ClientCount()` and included in `BroadcastStatus()`.

**Steps**:

1. Rename existing client maps for clarity:
   - `clients` → `wsClients`
   - `clientSizes` → `wsClientSizes`
   - Update all references

2. Update `ClientCount()`:
   ```go
   func (s *Session) ClientCount() int {
       s.mu.RLock()
       defer s.mu.RUnlock()
       return len(s.wsClients) + len(s.pollClients)
   }
   ```

3. Update `calculateMinSize()`:
   - Include polling client sizes in min calculation

4. Trigger `BroadcastStatus()` on polling client changes:
   - When polling client registers (first poll)
   - When polling client is reaped (60s timeout)

5. Add helper methods:
   - `AddPollingClient(clientId string)`
   - `RemovePollingClient(clientId string)`
   - `UpdatePollingClientSize(clientId string, rows, cols uint16)`

**Verification**:

- Red: Poll via HTTP, viewer count still shows "1" (polling not counted)
- Green:
  - Connect via WebSocket, see "1 viewer"
  - Poll via HTTP with new clientId
  - WebSocket client sees "2 viewers"
  - Stop polling for 60+ seconds
  - WebSocket client sees "1 viewer" again

---

### Phase 3: Server Testing via Playwright

**What will be achieved**: Validate server polling endpoints work by deploying to test container and testing with MCP Playwright.

**Steps**:

1. Build and deploy to test container:
   ```bash
   ./scripts/02-test-container-build.sh
   HOST_PORT=11977 HOST_IP=<YOUR_IP> ./scripts/03-test-container-run.sh
   ```

2. Use Playwright to navigate to test instance:
   - Open `http://<YOUR_IP>:11977/`
   - Select an assistant to create a session
   - Capture session UUID from URL

3. Test poll endpoint via browser console:
   ```javascript
   const resp = await fetch('/session/{uuid}/client/test123/poll?assistant=claude');
   const data = await resp.json();
   console.log('Poll response:', data);
   ```

4. Test send endpoint via browser console:
   ```javascript
   await fetch('/session/{uuid}/client/test123/send', {
     method: 'POST',
     headers: { 'Content-Type': 'application/json' },
     body: JSON.stringify({ type: 'input', data: 'echo hello\n' })
   });
   ```

5. Test viewer count integration:
   - Verify status bar shows increased viewer count after polling

6. Verify snapshot is valid terminal data:
   ```javascript
   const terminal = atob(data.terminal);
   console.log('First bytes:', terminal.slice(0, 20));
   // Should see \x1b[2J\x1b[H (clear + home)
   ```

7. Teardown:
   ```bash
   ./scripts/04-test-container-down.sh
   ```

**Verification**:

- Poll endpoint returns 200 with valid JSON structure
- Send endpoint returns 200, subsequent poll shows changed terminal
- Viewer count increments when polling client registers
- No console errors in browser

---

### Phase 4: Client - Transport Abstraction

**What will be achieved**: Extract current WebSocket logic into `WebSocketTransport` class, preparing for polling transport.

**Steps**:

1. Define transport interface (conceptual):
   ```javascript
   // Both transports implement:
   // - connect(), disconnect(), send(data), sendJSON(obj), isConnected()
   // Both call back to UI:
   // - ui.onTransportOpen(), onTransportClose(), onTransportError()
   // - ui.onTerminalData(data), onJSONMessage(msg)
   ```

2. Create `WebSocketTransport` class in terminal-ui.js:
   - Move WebSocket creation logic
   - Move onopen, onmessage, onclose, onerror handlers
   - Move sendResize logic (binary with 0x00 prefix)
   - Move sendJSON logic

3. Refactor `TerminalUI` to use transport:
   - Replace `this.ws` with `this.transport`
   - Add callback methods
   - Update all ws.send() and ws.readyState references

4. Add transport selection stub:
   ```javascript
   connect() {
       const params = new URLSearchParams(window.location.search);
       const transportType = params.get('transport');

       if (transportType === 'polling') {
           console.warn('Polling transport not implemented yet');
           this.updateStatus('error', 'Polling transport not implemented');
           return;
       }

       this.transport = new WebSocketTransport(this);
       this.transport.connect();
   }
   ```

**Verification**:

- Red: `?transport=polling` → error message, no terminal data
- Green (no query param): WebSocket connects, terminal works, reconnection works

---

### Phase 5: Client - Polling Transport

**What will be achieved**: Implement `PollingTransport` class that polls for snapshots and sends input via POST.

**Steps**:

1. Create `PollingTransport` class:
   ```javascript
   class PollingTransport {
       constructor(ui) {
           this.ui = ui;
           this.clientId = 'poll-' + Math.random().toString(36).substr(2, 9);
           this.active = false;
           this.pollInterval = 250;
       }
   }
   ```

2. Implement `connect()`:
   - Set active = true
   - Call ui.onTransportOpen()
   - Start pollLoop()

3. Implement `pollLoop()`:
   - Fetch snapshot from poll endpoint
   - Decode base64, call ui.onTerminalData()
   - Send status via ui.onJSONMessage()
   - Sleep pollInterval, repeat

4. Implement `send(data)`:
   - POST to send endpoint with type: "input"

5. Implement `sendJSON(obj)`:
   - Handle ping (fake pong locally)
   - Handle resize (POST to send endpoint)

6. Implement `disconnect()` and `isConnected()`

7. Wire up in TerminalUI.connect():
   - `?transport=polling` → PollingTransport
   - Otherwise → WebSocketTransport
   - Note: terminal input disabled in polling mode (Phase 6 adds textarea)

8. Expose transport for console testing:
   ```javascript
   window.terminalUI = this;
   ```

**Verification**:

- Red: `?transport=polling` → error/nothing
- Green (via Playwright console):
  ```javascript
  await window.terminalUI.transport.send('echo hello\n');
  // Next snapshot shows 'hello'
  ```

---

### Phase 6: Client - Polling Mode UI

**What will be achieved**: Show batch input textarea + quick-action buttons when in polling mode.

**Steps**:

1. Add polling mode indicator to status bar:
   - Add 'polling-mode' class
   - Show "Slow connection mode" text

2. Add CSS for polling mode

3. Create polling input UI:
   ```html
   <div class="terminal-ui__polling-input">
       <input type="text" placeholder="Type command...">
       <button>Send</button>
   </div>
   ```

4. Add quick-action buttons:
   ```html
   <div class="terminal-ui__polling-actions">
       <button data-send="\x03">Ctrl+C</button>
       <button data-send="\x04">Ctrl+D</button>
       <button data-send="\t">Tab</button>
       <button data-send="\x1b[A">↑</button>
       <button data-send="\x1b[B">↓</button>
       <button data-send="\n">Enter</button>
   </div>
   ```

5. Show/hide polling UI based on transport type

6. Wire up input handlers:
   - Send button click
   - Enter key in input
   - Quick action button clicks

7. Optionally reuse Paste overlay for multi-line input

**Verification**:

- Red: `?transport=polling` → no input UI
- Green:
  - `?transport=polling` → polling input bar visible
  - Type "ls", click Send → next snapshot shows output
  - Click Ctrl+C → sends interrupt
  - No query param → input bar hidden

---

### Phase 7: Client - Connection State Machine

**What will be achieved**: Automatic fallback from WebSocket to polling on connection failure, recovery when WS reconnects.

**States**:

```javascript
static STATES = {
    DISCONNECTED: 'disconnected',
    WS_CONNECTING: 'ws_connecting',
    WS_ACTIVE: 'ws_active',
    FALLBACK: 'fallback'  // Polling active, WS retrying in background
};
```

**Steps**:

1. Add state tracking:
   ```javascript
   this.connectionState = STATES.DISCONNECTED;
   this.wsFailureCount = 0;
   ```

2. Modify connect() for state machine:
   - If forced polling → start polling, skip WS
   - Otherwise → WS_CONNECTING, try WS

3. Handle WS failure → FALLBACK:
   - Start polling transport
   - Schedule WS retry

4. Implement WS retry during fallback:
   - Retry every 10-30s with backoff
   - Never give up, just slow down

5. Handle WS recovery:
   - Stop polling
   - Switch to WS transport
   - Update UI

6. Update UI based on state:
   - Show/hide polling input
   - Update status bar text

**State Transitions**:

```
DISCONNECTED → WS_CONNECTING → WS_ACTIVE
                     ↓              ↓
                  (fail)      (disconnect)
                     ↓              ↓
                     └──→ FALLBACK ←┘
                            ↓
                      (WS reconnects)
                            ↓
                        WS_ACTIVE
```

**Verification**:

1. Connect normally → WS_ACTIVE
2. Kill server → FALLBACK, polling UI appears, terminal updates via snapshots
3. Restart server → WS reconnects → WS_ACTIVE, polling UI hides
4. Kill server again → FALLBACK, retries continue (verify in console)

**Not covered (known risks for future hardening)**:

- Rapid state transitions / race conditions
- Network flapping edge cases

Mitigate via code review: clear state variable, cancel pending timeouts on state change, guard transitions.

---

## Testing Infrastructure

- Test container: `./scripts/02-test-container-build.sh` + `./scripts/03-test-container-run.sh`
- Test URL: `http://<YOUR_IP>:11977/`
- Force polling: `?transport=polling` query parameter
- MCP Playwright for browser automation and console testing
