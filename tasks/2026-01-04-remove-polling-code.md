# Remove HTTP Polling Fallback Code

**Date**: 2026-01-04
**Goal**: Remove the HTTP polling fallback feature from swe-swe-server, reverting to WebSocket-only terminal connections.

---

## Phase 1: Remove Client-Side Polling Code (JavaScript)

### What will be achieved
The browser will use direct WebSocket connections only, removing ~600 lines of transport abstraction, polling fallback, and polling-specific UI from `terminal-ui.js`.

### Steps

1. **Document polling keyboard UI for future reference** - Before deletion, copy the quick-action buttons HTML/CSS/JS to `docs/mobile-keyboard-ui.md` for potential future mobile WebSocket implementation:
   - Buttons: Ctrl+C, Ctrl+D, Tab, ↑, ↓, Enter
   - Input bar with Send button
   - Associated CSS and event handlers

2. **Remove transport classes** - Delete `WebSocketTransport` and `PollingTransport` classes (lines ~1-180)

3. **Remove connection state machine** - Delete `CONNECTION_STATES` enum and related state management properties (`connectionState`, `wsFailureCount`, `forcedPolling`, `wsRetryTimeout`)

4. **Remove `?transport=` query param handling** - Delete code that checks for `?transport=polling` to force polling mode

5. **Restore direct WebSocket usage** - Replace `this.transport` calls with direct `this.ws` WebSocket usage (reverting to pre-polling pattern)

6. **Remove polling UI elements** - Delete:
   - "Slow connection mode" status bar styling (`.polling-mode`, brown theme)
   - Quick-action buttons HTML (`.terminal-ui__polling-actions`)
   - Input bar HTML (`.terminal-ui__polling-input`)
   - All associated CSS

7. **Remove polling state properties** - Delete `isPollingMode` and related flags from the constructor

8. **Clean up event handlers** - Remove polling-specific button handlers and transport abstraction callbacks (`onTransportOpen`, `onTransportClose`, `onTransportError`)

### Verification

**Red**: Before changes, confirm polling code exists:
```bash
grep -c "PollingTransport\|polling-mode\|CONNECTION_STATES\|transport=polling\|Slow connection" terminal-ui.js
# Should return non-zero counts
```

**Green**: After changes, verify removal:
```bash
grep -c "PollingTransport\|polling-mode\|CONNECTION_STATES\|transport=polling\|Slow connection" terminal-ui.js
# Should return 0
```

**Refactor/Regression check**:
- Manual test: Load terminal in browser, verify WebSocket connects and terminal works
- Check console for no JavaScript errors
- Verify terminal input/output, resize, and reconnection still function

---

## Phase 2: Remove Server-Side Polling Code (Go)

### What will be achieved
Remove all polling-related code from `main.go` (~225 lines), reverting to WebSocket-only client handling.

### Steps

1. **Remove `PollingClient` type** - Delete the struct definition (lines 52-57)

2. **Remove `pollClients` field from Session struct** - Delete the map field (line 143)

3. **Revert `calculateMinSize()`** - Remove polling client size iteration (lines 248-256), restore original WebSocket-only logic

4. **Revert `ClientCount()`** - Remove `+ len(s.pollClients)` from return (line 281)

5. **Revert `BroadcastStatus()`** - Remove polling client count from viewer count (line 314)

6. **Revert `RemoveClient()`** - Remove `len(s.pollClients) > 0` check (line 175)

7. **Revert `Close()`** - Remove `s.pollClients` clearing (line 403)

8. **Remove polling HTTP routing** - Delete `/session/{uuid}/client/{clientId}/poll` and `/send` path handling (lines 766-784)

9. **Remove `handlePollRecv()` function** - Delete entire function (~130 lines, 1206-1337)

10. **Remove `handlePollSend()` function** - Delete entire function (~90 lines, 1339-1430)

11. **Revert `sessionReaper()`** - Remove polling client cleanup and resize logic (lines 881-912)

12. **Revert session initialization** - Remove `pollClients: make(...)` from `getOrCreateSession()` (line 982)

13. **Remove unused imports** - Delete `encoding/base64` and `strconv` if no longer needed

### Verification

**Red**: Before changes, confirm polling code exists:
```bash
grep -c "pollClients\|PollingClient\|handlePollRecv\|handlePollSend" main.go
# Should return non-zero counts
```

**Green**: After changes, verify removal:
```bash
grep -c "pollClients\|PollingClient\|handlePollRecv\|handlePollSend" main.go
# Should return 0
```

**Refactor/Regression check**:
- `go build` succeeds with no errors
- `go vet` passes
- Manual test: WebSocket terminal still works (connect, input, output, resize, reconnect)

---

## Phase 3: Update Golden Files & Verify

### What will be achieved
Regenerate all golden test files to reflect the polling removal, ensure build passes, and verify no regressions.

### Steps

1. **Run build and golden update** - Execute per CLAUDE.md instructions:
   ```bash
   make build golden-update
   ```

2. **Verify golden file changes** - Check that changes are as expected:
   ```bash
   git add -A cmd/swe-swe/testdata/golden
   git diff --cached --stat -- cmd/swe-swe/testdata/golden
   ```
   - Should show main.go and terminal-ui.js changes across all 24 golden variants
   - No unexpected file additions or deletions

3. **Verify polling code removed from golden files**:
   ```bash
   grep -r "pollClients\|PollingTransport" cmd/swe-swe/testdata/golden/
   # Should return nothing
   ```

4. **Run tests** (if any exist):
   ```bash
   go test ./...
   ```

5. **Final manual verification**:
   - Start the server locally
   - Connect via browser
   - Verify terminal works end-to-end

### Verification

**Red**: Before golden update, files are out of sync with templates

**Green**: After golden update:
- `make build` succeeds
- Golden files match templates exactly
- No polling code remains anywhere

**Refactor/Regression check**:
- All 24 golden test variants updated consistently
- No build or test failures
- WebSocket terminal fully functional

---

## Commits

After each phase, create a commit:

1. **Phase 1**: `refactor(client): remove HTTP polling fallback transport`
2. **Phase 2**: `refactor(server): remove HTTP polling endpoints`
3. **Phase 3**: `chore: update golden files for polling removal`

Or optionally squash into a single commit:
- `refactor: remove HTTP polling fallback feature`
