# Debug Log Overlay

## Goal

Replace terminal-based debug logging with overlay notifications (like chat/file upload status), controlled by `?debug=true` query parameter. This enables us to instrument PTY output for "waiting for input" detection research without polluting the terminal.

## Phases

1. Phase 1: Add debug mode infrastructure ✅
2. Phase 2: Migrate existing debug logs ✅
3. Phase 3: Add PTY output instrumentation

---

## Phase 1: Add debug mode infrastructure

### What will be achieved
A `debugMode` property and `debugLog(message)` method that displays messages in the overlay (like chat notifications) only when `?debug=true` is in the URL.

### Steps

1. Add `this.debugMode` property in constructor, initialized from `URLSearchParams`
2. Add `debugLog(message, durationMs = 3000)` method that:
   - Checks `this.debugMode`, returns early if false
   - Calls `showStatusNotification()` with `[DEBUG] ` prefix
3. Optionally add a distinct CSS class `.debug` for debug messages (different styling from `.system`)

### Verification (using test container + MCP browser)

1. Build and run test container:
   ```bash
   ./scripts/02-test-container-build.sh
   HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh
   ```

2. **Test without debug param**:
   - Navigate to `http://host.docker.internal:9899/`
   - Select an assistant, connect
   - Take snapshot, confirm no `[DEBUG]` messages in overlay

3. **Test with debug param**:
   - Navigate to `http://host.docker.internal:9899/claude/{uuid}?debug=true`
   - Take snapshot, confirm `[DEBUG]` messages appear in overlay (top-right)
   - Confirm they do NOT appear in terminal output

4. **Regression check**:
   - Drag/drop a file, confirm upload notification still works
   - Confirm chat overlay still works

5. Teardown: `./scripts/04-test-container-down.sh`

---

## Phase 2: Migrate existing debug logs

### What will be achieved
Replace all `this.term.write('[DEBUG]...')` calls with `this.debugLog()`, so existing debug output moves from terminal to overlay (when enabled).

### Steps

1. Identify all existing debug log calls:
   - Line 80: `this.term.write('[DEBUG] initTerminal done\r\n');`
   - Line 84: `this.term.write('[DEBUG] scheduling connect() in 200ms\r\n');`
   - Line 86: `this.term.write('[DEBUG] setTimeout fired, calling connect()\r\n');`
   - Line 1015: `this.term.write('[DEBUG] connect() called\r\n');`
   - Line 1032: `this.term.write('[DEBUG] Creating WebSocket to: ' + url + '\r\n');`
   - Line 1036: `this.term.write('[DEBUG] WebSocket created, readyState=' + this.ws.readyState + '\r\n');`
   - Line 1038: `this.term.write('[DEBUG] WebSocket constructor threw: ' + e.message + '\r\n');`
   - Line 1051: `this.term.write('[DEBUG] WebSocket stuck in CONNECTING state...\r\n');`

2. Replace each with `this.debugLog('message')` (without `[DEBUG]` prefix since `debugLog` adds it)

3. Remove `\r\n` suffixes (not needed for overlay)

### Verification (using test container + MCP browser)

1. Rebuild: `./scripts/02-test-container-build.sh`
2. Run: `HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh`

3. **Test without debug param**:
   - Navigate to `http://host.docker.internal:9899/`
   - Select assistant, connect
   - Take snapshot, confirm terminal output is clean (no `[DEBUG]` lines)

4. **Test with debug param**:
   - Navigate with `?debug=true`
   - Take snapshot, confirm debug messages appear in overlay
   - Confirm connection still works (WebSocket connects successfully)

5. Teardown: `./scripts/04-test-container-down.sh`

---

## Phase 3: Add PTY output instrumentation

### What will be achieved
Instrument the terminal data flow to log output events, enabling research into detecting "waiting for user input" state. This gives us visibility into what data flows through the terminal and when.

### Steps

1. Add tracking state in constructor:
   - `this.lastOutputTime = null` - timestamp of last PTY output
   - `this.outputIdleTimer = null` - timer for idle detection

2. Modify `onTerminalData(data)` method to:
   - Update `this.lastOutputTime = Date.now()`
   - Clear and restart idle timer
   - Call `this.debugLog()` with output stats (byte count, time since last output)

3. Add idle detection callback:
   - After N ms of no output, call `this.debugLog('Output idle for Nms - user input needed?')`
   - Start with 2000ms threshold (configurable later)

4. Add `this.debugLog()` call for input events too (optional, helps see full picture):
   - In `term.onData()` handler, log when user sends input

### Verification (using test container + MCP browser)

1. Rebuild: `./scripts/02-test-container-build.sh`
2. Run: `HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh`

3. **Test idle detection**:
   - Navigate with `?debug=true`
   - Select assistant (e.g., `bash` for simpler testing)
   - Wait at prompt
   - Take snapshot, confirm `Output idle` debug message appears in overlay

4. **Test output activity**:
   - Run a command that produces output (e.g., `ls -la`)
   - Take snapshot, confirm output stats appear in overlay
   - Confirm idle timer resets after output

5. **Test with Claude CLI** (if available):
   - Observe debug messages while Claude is working vs waiting
   - Note patterns for future audio UX implementation

6. Teardown: `./scripts/04-test-container-down.sh`
