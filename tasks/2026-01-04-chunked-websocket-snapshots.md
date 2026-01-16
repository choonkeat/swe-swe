# Implement 500KB VT Buffer with Chunked WebSocket Snapshots

**Date**: 2026-01-04
**Goal**: Implement 500KB VT buffer with gzip compression and 8KB chunked WebSocket delivery in swe-swe-server, enabling reliable mobile terminal viewing with scrollback.
**Research**: `research/2026-01-04-ios-safari-websocket-chunking.md`
**ADR**: `docs/adr/0015-chunked-websocket-snapshots.md`
**Reference**: `/workspace/tmp/ws-test/main.go` (working prototype)

---

## Phase 1: Server - Increase VT buffer & add compression ✅ DONE

### What will be achieved
The vt10x terminal emulator will maintain 500KB of scrollback buffer (up from current size), and snapshot generation will include gzip compression.

### Steps

1. ✅ **Locate vt10x initialization** in `main.go` - find where `vt10x.New()` is called and current buffer size
2. ⚠️ **Increase scrollback buffer** - Note: vt10x doesn't support true scrollback buffer; it maintains the current screen state only. The chunking protocol (Phase 2) enables reliable transmission of whatever size the screen buffer is.
3. ✅ **Add gzip compression helper** - Created `compressSnapshot(data []byte) ([]byte, error)` function using `compress/gzip`
4. ✅ **Modify `GenerateSnapshot()`** - Returns compressed data, added compression stats logging
5. ✅ **Add compression stats logging** - Logs "Snapshot compressed: X -> Y bytes (Z%)"
6. ✅ **Add chunk protocol constants** - ChunkMarker (0x02), DefaultChunkSize (8192), MinChunkSize (512)

### Verification (TDD style)

**Red**: Before changes:
```bash
# Check current buffer size in code
grep -n "vt10x" main.go
# Snapshot is uncompressed (no gzip magic bytes 0x1f 0x8b at start)
```

**Green**: After changes:
```bash
# Verify compression function exists and works
go test -run TestCompressSnapshot
# Snapshot starts with gzip magic bytes
# Logs show compression ratio (e.g., "Compressed 50000 -> 8000 bytes (16%)")
```

**Refactor/Regression**:
- `go build` succeeds
- `go vet` passes
- Existing WebSocket clients still receive data (though they won't parse it yet - Phase 3 fixes this)

---

## Phase 2: Server - Chunked snapshot delivery

### What will be achieved
Compressed snapshots will be split into 8KB chunks and sent as multiple WebSocket binary messages with a chunk header protocol.

### Steps

1. **Define chunk protocol constants** - `ChunkMarker = 0x02`, `DefaultChunkSize = 8192`, `MinChunkSize = 512`
2. **Create `sendChunked()` function** - Takes connection, compressed data, chunk size; sends `[0x02, index, total, ...data]` per chunk
3. **Replace direct snapshot send** - In `handleWebSocket()` where snapshot is sent to new clients, call `sendChunked()` instead of `WriteMessage()`
4. **Replace broadcast snapshot send** - If snapshots are broadcast elsewhere, update those calls too
5. **Handle edge case** - If compressed data fits in one chunk, still use chunk protocol for consistency
6. **Add chunk logging** - Log "Sent chunk X/Y (Z bytes)" for debugging

### Verification (TDD style)

**Red**: Before changes:
```bash
# Snapshot sent as single message
grep -n "WriteMessage.*snapshot\|WriteMessage.*Snapshot" main.go
# No chunk protocol in code
grep -c "0x02" main.go  # Should be 0 or unrelated
```

**Green**: After changes:
```bash
# sendChunked function exists
grep -n "func sendChunked" main.go
# Chunk protocol used for snapshots
# Server logs show "Sent chunk 1/N", "Sent chunk 2/N", etc.
```

**Refactor/Regression**:
- `go build` succeeds
- Server starts and accepts WebSocket connections
- Chunks are sent (visible in logs), though client can't parse yet (Phase 3)

---

## Phase 3: Client - Chunk reassembly & decompression

### What will be achieved
The browser client will collect incoming chunks, reassemble them, decompress with the DecompressionStream API, and write the terminal data to xterm.js.

### Steps

1. **Add chunk state variables** - `this.chunks = []`, `this.expectedChunks = 0` in TerminalUI constructor
2. **Detect chunk messages** - In `onmessage` handler, check if binary data starts with `0x02`
3. **Implement `handleChunk(data)`** - Extract index, total, payload; store in `chunks[index]`
4. **Detect completion** - When all chunks received, trigger reassembly
5. **Implement `reassembleChunks()`** - Concatenate all chunk payloads into single Uint8Array
6. **Implement `decompressSnapshot(data)`** - Use `DecompressionStream('gzip')` API to decompress
7. **Write to terminal** - Pass decompressed data to `this.term.write()`
8. **Reset chunk state** - Clear `chunks` and `expectedChunks` after successful processing
9. **Handle non-chunk binary** - Existing binary messages (terminal output, resize) still work as before

### Reference Implementation (from ws-test)

**Server (Go)**:
```go
chunk[0] = 0x02 // Chunk marker
chunk[1] = byte(i)
chunk[2] = byte(totalChunks)
copy(chunk[3:], compressed[start:end])
```

**Client (JS)**:
```javascript
const arr = new Uint8Array(data);
if (arr[0] !== 0x02) return; // Not a chunk

const chunkIndex = arr[1];
const totalChunks = arr[2];
const chunkData = arr.slice(3);
chunks[chunkIndex] = chunkData;

// Reassemble when complete
if (received === expectedChunks) {
    const decompressed = await decompress(compressed);
    // DecompressionStream('gzip') API
}
```

### Verification (TDD style)

**Red**: Before changes:
```bash
# No chunk handling in client
grep -c "0x02\|handleChunk\|DecompressionStream" terminal-ui.js  # Should be 0
# Loading terminal shows garbled/no output (compressed data not parsed)
```

**Green**: After changes:
```bash
# Chunk handling exists
grep -n "handleChunk\|DecompressionStream" terminal-ui.js
# Console logs "CHUNK 1/N", "CHUNK 2/N", "REASSEMBLED: X -> Y bytes"
# Terminal displays correctly on initial load
```

**Refactor/Regression**:
- No JavaScript console errors
- Terminal output displays correctly
- Resize still works
- Reconnection still receives snapshot and displays correctly
- Desktop browser works
- iOS Safari works (the whole point!)

---

## Phase 4: Server - Adaptive chunk sizing (optional)

### What will be achieved
The server will track chunk delivery success per session and automatically reduce chunk size if clients disconnect shortly after receiving chunks.

### Steps

1. **Add chunk tracking fields to Session** - `ChunkSize int`, `LastChunkSent time.Time`, `StableCount int`
2. **Initialize default chunk size** - Set `ChunkSize = 8192` when session created
3. **Implement `markChunkSent()`** - Record `LastChunkSent = time.Now()` before sending chunks
4. **Implement `markDeliveryFailed()`** - Called on client disconnect; if disconnect within 3 seconds of chunk send, reduce `ChunkSize` by 15%
5. **Implement `checkDeliverySuccess()`** - Called 3 seconds after chunk send; increment `StableCount` if still connected
6. **Apply minimum chunk size** - Never reduce below 512 bytes
7. **Use session chunk size** - Pass `sess.ChunkSize` to `sendChunked()` instead of hardcoded value
8. **Log adaptive events** - "Session X: reducing chunk 8192 -> 6963" on failure

### Verification (TDD style)

**Red**: Before changes:
```bash
# No adaptive sizing fields
grep -c "ChunkSize\|LastChunkSent\|StableCount" main.go  # Should be 0
# All clients use same fixed chunk size
```

**Green**: After changes:
```bash
# Adaptive fields exist
grep -n "ChunkSize.*int\|LastChunkSent\|markDeliveryFailed" main.go
# Logs show "delivery stable" for healthy connections
# Simulated flaky client shows "reducing chunk X -> Y"
```

**Refactor/Regression**:
- Normal clients unaffected (stable at 8KB)
- Server handles rapid connect/disconnect gracefully
- No panics or race conditions (fields protected by mutex)

---

## Phase 5: Golden files & verification

### What will be achieved
All golden test files will be updated to reflect the chunking changes, and end-to-end testing on mobile will verify iOS Safari works correctly.

### Steps

1. **Rebuild CLI with updated templates** - `make build-cli` to embed new main.go and terminal-ui.js
2. **Regenerate golden files** - `make golden-update` to update all 24 test variants
3. **Review golden diff** - `git diff --cached -- cmd/swe-swe/testdata/golden` to verify changes are as expected
4. **Run tests** - `go test ./...` to ensure no test failures
5. **Deploy to test container** - Use `./scripts/01-test-container-init.sh`, `./scripts/02-test-container-build.sh`, `./scripts/03-test-container-run.sh`
6. **Test on desktop browser** - Verify terminal loads, scrollback works, reconnection works
7. **Test on iOS Safari** - Verify no disconnect loop, chunks received, scrollback available
8. **Test large output** - Generate 500KB+ of terminal output, verify mobile can scroll through it

### Verification (TDD style)

**Red**: Before golden update:
```bash
# Templates modified but golden files stale
make build && git status  # Shows modified golden files
```

**Green**: After golden update:
```bash
# Golden files match templates
make build golden-update
git diff --cached -- cmd/swe-swe/testdata/golden  # Shows expected changes
grep -r "sendChunked\|handleChunk" cmd/swe-swe/testdata/golden/  # Finds chunking code
```

**Refactor/Regression**:
- All 24 golden variants updated consistently
- `go test ./...` passes
- Desktop browser: terminal works, no console errors
- iOS Safari: stable connection, chunks received, decompression works
- Mobile scrolling: can scroll through large terminal buffer

---

## Commits

After each phase, create a commit:

1. **Phase 1**: `feat(server): add 500KB VT buffer with gzip compression`
2. **Phase 2**: `feat(server): implement chunked snapshot delivery`
3. **Phase 3**: `feat(client): add chunk reassembly and decompression`
4. **Phase 4**: `feat(server): add adaptive chunk sizing per session`
5. **Phase 5**: `chore: update golden files for chunked snapshots`

Or group into fewer commits if phases are small.
