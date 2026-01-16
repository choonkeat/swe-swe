# Implement Server-Side Ring Buffer for Terminal Scrollback

**Date**: 2026-01-06
**Goal**: When a new client joins an existing session, give them scrollback history by storing raw PTY output in a ring buffer and replaying it to new joiners.

**Context**: Currently new joiners only see the visible viewport (vt10x doesn't support scrollback). The chunking infrastructure from ADR-015 can handle transmitting large buffers, but we need to actually store the raw output.

---

## Phase 1: Add ring buffer to Session struct ✅

### What will be achieved
A circular buffer (ring buffer) will be added to each Session that can store up to 512KB of raw terminal output. The buffer will automatically overwrite oldest data when full.

### Steps

1. **Add ring buffer constants** - Define `RingBufferSize = 512 * 1024` (512KB) at package level
2. **Add ring buffer fields to Session struct**:
   - `ringBuf []byte` - the actual buffer storage
   - `ringHead int` - write position (where next byte goes)
   - `ringLen int` - current number of bytes stored (0 to RingBufferSize)
3. **Initialize ring buffer in `getOrCreateSession()`** - Allocate `ringBuf: make([]byte, RingBufferSize)`
4. **Implement `writeToRing(data []byte)` method** - Writes data to ring buffer, wrapping around and updating head/len
5. **Implement `readRing() []byte` method** - Returns copy of ring buffer contents in correct order (oldest to newest)

### Verification (TDD style)

**Red**: Before changes:
```bash
# No ring buffer in Session struct
grep -c "ringBuf\|ringHead\|ringLen" cmd/swe-swe/templates/host/swe-swe-server/main.go
# Returns 0
```

**Green**: After changes:
```bash
# Ring buffer fields exist
grep -n "ringBuf\|ringHead\|ringLen" cmd/swe-swe/templates/host/swe-swe-server/main.go
# Shows struct fields and methods
go build ./cmd/swe-swe  # Compiles successfully
```

**Refactor/Regression**:
- `go build` succeeds
- `go vet` passes
- Existing functionality unchanged (buffer exists but isn't used yet)

---

## Phase 2: Capture PTY output to ring buffer ✅

### What will be achieved
All PTY output will be written to the ring buffer as it's read, so the buffer accumulates terminal history. This happens alongside the existing VT emulator write and client broadcast.

### Steps

1. **Locate PTY read loop** - Find `startPTYReader()` goroutine where `ptyFile.Read(buf)` happens
2. **Add ring buffer write after PTY read** - Call `s.writeToRing(buf[:n])` after successful read
3. **Protect with mutex** - Ring buffer writes need synchronization; reuse `vtMu` or add dedicated `ringMu`
4. **Also capture restart/exit messages** - The `[Process exited...]` and `[Process restarting...]` messages written to VT should also go to ring buffer

### Verification (TDD style)

**Red**: Before changes:
```bash
# No writeToRing call in PTY reader
grep -n "writeToRing" cmd/swe-swe/templates/host/swe-swe-server/main.go
# Returns nothing
```

**Green**: After changes:
```bash
# writeToRing called in PTY reader
grep -n "writeToRing" cmd/swe-swe/templates/host/swe-swe-server/main.go
# Shows call in startPTYReader and restart/exit message handling
go build ./cmd/swe-swe  # Compiles successfully
```

**Refactor/Regression**:
- `go build` succeeds
- Existing clients still receive real-time output (broadcast unchanged)
- VT snapshot still works for new joiners (Phase 3 adds ring buffer replay)

---

## Phase 3: Replay ring buffer to new joiners ✅

### What will be achieved
When a new client joins an existing session, they receive the ring buffer contents (scrollback history) before the VT snapshot. This gives them terminal history they can scroll back through.

### Steps

1. **Locate new joiner snapshot send** - Find in `handleWebSocket()` where `GenerateSnapshot()` is called for non-new sessions
2. **Read ring buffer contents** - Call `s.readRing()` to get buffered output
3. **Send ring buffer first** - Use existing `sendChunked()` to send compressed ring buffer data
4. **Then send VT snapshot** - Existing snapshot send follows (positions cursor correctly)
5. **Add logging** - Log ring buffer size sent: "Sending X bytes of scrollback history"
6. **Handle empty buffer gracefully** - Skip ring buffer send if `ringLen == 0`

### Verification (TDD style)

**Red**: Before changes:
```bash
# Only snapshot sent to new joiners
grep -n "GenerateSnapshot\|readRing" cmd/swe-swe/templates/host/swe-swe-server/main.go
# Shows GenerateSnapshot but no readRing
```

**Green**: After changes:
```bash
# Ring buffer read and sent before snapshot
grep -n "readRing" cmd/swe-swe/templates/host/swe-swe-server/main.go
# Shows readRing call in handleWebSocket
# Server logs show "Sending X bytes of scrollback history" when client joins existing session
```

**Refactor/Regression**:
- `go build` succeeds
- New session creation still works (empty ring buffer, just snapshot)
- Joining existing session: client receives scrollback + current screen
- Real-time output still broadcasts correctly
- iOS Safari still works (chunking handles large ring buffer)

---

## Phase 4: Golden files & verification

### What will be achieved
All golden test files will be updated to reflect the ring buffer changes, and manual testing will verify scrollback works correctly for new joiners.

### Steps

1. **Rebuild CLI with updated templates** - `make build` to embed new main.go
2. **Regenerate golden files** - `make golden-update` to update all test variants
3. **Review golden diff** - Verify only ring buffer additions appear in diff:
   - `RingBufferSize` constant
   - `ringBuf`, `ringHead`, `ringLen` fields
   - `writeToRing()`, `readRing()` methods
   - Ring buffer send in `handleWebSocket()`
4. **Run tests** - `go test ./...` passes
5. **Stage golden files** - `git add -A cmd/swe-swe/testdata/golden`
6. **Manual test: new session** - Start fresh session, verify works normally
7. **Manual test: join existing session** - Generate output, join from another browser, verify scrollback visible
8. **Manual test: buffer wraparound** - Generate >512KB output, verify old data dropped, new joiner gets recent history

### Verification (TDD style)

**Red**: Before golden update:
```bash
# Templates modified but golden files stale
make build && git status
# Shows modified golden files
```

**Green**: After golden update:
```bash
# Golden files match templates
make build golden-update
git diff --cached -- cmd/swe-swe/testdata/golden | head -50
# Shows ring buffer additions consistently across all variants
go test ./...  # Passes
```

**Refactor/Regression**:
- All golden variants updated consistently
- `go test ./...` passes
- New sessions work (no scrollback to replay)
- Existing sessions: new joiner sees scrollback history
- Real-time output unaffected
- iOS Safari works with large scrollback (chunking)

---

## Commits

After each phase, create a commit:

1. **Phase 1**: `feat(server): add ring buffer struct for terminal scrollback`
2. **Phase 2**: `feat(server): capture PTY output to ring buffer`
3. **Phase 3**: `feat(server): replay ring buffer to new session joiners`
4. **Phase 4**: `chore: update golden files for ring buffer scrollback`

Or combine into single commit if preferred.
