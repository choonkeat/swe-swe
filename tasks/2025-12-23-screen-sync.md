# Screen Sync Implementation Plan

**Goal:** Fix multiplayer cursor/screen divergence by tracking terminal state server-side and sending snapshots to new clients.

**Library:** `github.com/hinshun/vt10x`

---

## Progress Tracking

| Step | Description | Status | Commit |
|------|-------------|--------|--------|
| 1 | Add vt10x dependency | ✅ | `feat: add vt10x dependency for screen sync` |
| 2 | Add VirtualTerminal to Session | ✅ | `feat: add vt10x terminal to Session struct` |
| 3 | Feed PTY output to VirtualTerminal | ✅ | `feat: feed PTY output to virtual terminal` |
| 4 | Implement snapshot generation | ✅ | `feat: implement screen snapshot generation` |
| 5 | Send snapshot to new clients | ✅ | `feat: send screen snapshot to late-joining clients` |
| 6 | Handle terminal resize in VT | ✅ | `feat: resize virtual terminal on client resize` |
| 7 | Test and polish | ✅ | (tested) |

---

## Step 1: Add vt10x Dependency ✅

**Goal:** Add the vt10x library to the project.

**Commands:**
```bash
go get github.com/hinshun/vt10x
```

**Test:**
- `make build` succeeds
- `go mod tidy` shows vt10x in go.sum

**Files changed:**
- `go.mod`
- `go.sum`

---

## Step 2: Add VirtualTerminal to Session ✅

**Goal:** Add a vt10x.VT instance to the Session struct.

**Changes to `main.go`:**
```go
import "github.com/hinshun/vt10x"

type Session struct {
    // ... existing fields
    vt *vt10x.VT
    vtMu sync.Mutex  // separate mutex for VT operations
}
```

**Test:**
- `make build` succeeds
- Server starts without errors

**Files changed:**
- `main.go`

---

## Step 3: Feed PTY Output to VirtualTerminal ✅

**Goal:** Write all PTY output to the VirtualTerminal so it tracks screen state.

**Changes to `main.go`:**
- In `getOrCreateSession()`: initialize `vt` with terminal size
- In `startPTYReader()`: write to `s.vt` before broadcasting

**Test:**
- Start server, connect one client
- Type commands, verify terminal works as before
- No regressions

**Files changed:**
- `main.go`

---

## Step 4: Implement Snapshot Generation ✅

**Goal:** Create a function that generates ANSI escape sequences to recreate the current screen state.

**New function:**
```go
func (s *Session) GenerateSnapshot() []byte {
    s.vtMu.Lock()
    defer s.vtMu.Unlock()

    var buf bytes.Buffer
    // Clear screen, render all cells, position cursor
    return buf.Bytes()
}
```

**Test:**
- Unit test or manual test that snapshot produces valid ANSI
- Connect client, type some commands, call snapshot, verify output looks correct

**Files changed:**
- `main.go`

---

## Step 5: Send Snapshot to New Clients ✅

**Goal:** When a client connects to an existing session, send them the snapshot before adding them to broadcast.

**Changes to `handleWebSocket()`:**
- If session already exists (not new), send snapshot to client first
- Then add client to session

**Test:**
- Start server, connect Client A
- Type commands in Client A
- Connect Client B to same session
- **Verify:** Client B sees the same screen content as Client A
- **Verify:** Cursor position is correct on both

**Files changed:**
- `main.go`

---

## Step 6: Handle Terminal Resize in VT ✅

**Goal:** When terminal is resized, also resize the VirtualTerminal.

**Changes:**
- In `UpdateClientSize()`: also call `s.vt.Resize(cols, rows)`

**Test:**
- Connect two clients with different window sizes
- Resize one client
- Verify both clients still work correctly
- New client connecting still gets correct snapshot

**Files changed:**
- `main.go`

---

## Step 7: Test and Polish ✅

**Goal:** End-to-end testing of multiplayer scenarios.

**Test scenarios:**
1. Client A types, Client B joins late → B sees same screen
2. Client A runs `vim`, Client B joins → B sees vim screen
3. Client A scrolls output, Client B joins → B sees same position
4. Resize scenarios
5. Process restart scenarios

**Polish:**
- Add logging for snapshot generation
- Handle edge cases (empty screen, very long lines)

**Files changed:**
- `main.go` (if needed)

---

## Notes

- vt10x handles most ANSI parsing
- Snapshot generation needs to handle:
  - Screen content (characters)
  - Cursor position
  - Text attributes (colors, bold, etc.) - may be limited in vt10x
- Alternative screen buffer (vim, less) may need special handling
