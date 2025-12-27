# Terminal Web App Implementation Plan

**Created:** 2025-12-22
**Status:** In Progress
**Research:** See `research/2025-12-22-terminal-web-app-gap-analysis.md`

## Overview

Build a Go web app that serves a terminal UI (xterm.js) with multiplayer session support.

## CLI Flags

```
-addr           string   Listen address (default ":9898")
-shell          string   Command to execute (default "bash")
-shell-restart  string   Command to restart on process death (default: same as -shell)
-session-ttl    duration Session keepalive after disconnect (default 1h)
```

## Implementation Steps

---

### Step 1: Project Scaffold ✅

**Goal:** Basic Go project structure with `go:embed` for static files.

**Files to create:**
- `go.mod`
- `main.go` - minimal HTTP server
- `static/index.html` - placeholder page
- `Makefile` - build and run targets

**Test:**
- `make run` starts server on :9898
- `curl http://localhost:9898/` returns HTML

**Commit:** `feat: scaffold terminal web app with go:embed`

---

### Step 2: xterm.js Integration ✅

**Goal:** Serve xterm.js and render a terminal (no backend yet).

**Files to create/modify:**
- `static/xterm.js` - download xterm.js library
- `static/xterm.css` - download xterm.js styles
- `static/index.html` - integrate xterm.js, show terminal

**Test:**
- Browser shows xterm.js terminal
- Can type characters (echoed locally or no-op)

**Commit:** `feat: integrate xterm.js terminal rendering`

---

### Step 3: UUID Session Routing ✅

**Goal:** `/` redirects to `/session/{uuid}`, session pages served correctly.

**Files to modify:**
- `main.go` - add route handlers for `/` and `/session/{uuid}`

**Test:**
- `curl -I http://localhost:8080/` returns 302 redirect to `/session/{uuid}`
- `curl http://localhost:8080/session/test-uuid` returns HTML
- UUID is extracted and passed to template

**Commit:** `feat: add UUID-based session routing`

---

### Step 4: WebSocket Endpoint ✅

**Goal:** WebSocket endpoint at `/ws/{uuid}` that echoes messages.

**Dependencies to add:**
- `github.com/gorilla/websocket`

**Files to modify:**
- `main.go` - add WebSocket handler
- `static/index.html` - connect xterm.js to WebSocket

**Test:**
- Browser connects to WebSocket
- Typed characters sent to server, echoed back
- xterm.js displays echoed characters

**Commit:** `feat: add WebSocket endpoint with echo`

---

### Step 5: PTY Session Management ✅

**Goal:** Create PTY running shell command, pipe I/O over WebSocket.

**Dependencies to add:**
- `github.com/creack/pty`
- `github.com/google/uuid`

**Files to modify:**
- `main.go` - add Session struct, session map, PTY creation
- Add `-shell` CLI flag

**Test:**
- Connect to session, run `echo hello`, see output
- Run `ls`, see file listing
- Multiple commands work

**Commit:** `feat: add PTY session with configurable shell`

---

### Step 6: Multiplayer Session Sharing ✅

**Goal:** Multiple clients on same UUID share the session.

**Files to modify:**
- `main.go` - broadcast PTY output to all clients, accept input from all

**Test:**
- Open two browser tabs with same UUID
- Type in one tab, both tabs see output
- Type in other tab, both tabs see output
- Characters may interleave (expected)

**Commit:** `feat: add multiplayer session broadcasting`

---

### Step 7: Session TTL and Cleanup ✅

**Goal:** Sessions persist for TTL after last client disconnects.

**Files to modify:**
- `main.go` - add session reaper goroutine, `-session-ttl` flag

**Test:**
- Connect to session, disconnect
- Session still exists (check server logs)
- Reconnect within TTL, session still works
- Wait for TTL to expire, session cleaned up (check logs)

**Commit:** `feat: add session TTL and cleanup reaper`

---

### Step 8: Process Restart on Death ✅

**Goal:** If shell process exits, restart it automatically.

**Files to modify:**
- `main.go` - detect process exit, restart with `-shell-restart` command

**Test:**
- Run `exit` in terminal
- Process restarts (see new shell prompt or claude output)
- `-shell-restart` flag overrides restart command

**Commit:** `feat: add automatic process restart on death`

---

### Step 9: Terminal Resize Handling ✅

**Goal:** Handle terminal resize from clients using smallest screen size among all connected clients.

**Files to modify:**
- `main.go` - add resize message handling, track each client's size, compute minimum
- `static/index.html` - send resize events via WebSocket on connect and window resize

**Test:**
- Resize browser window
- PTY receives new size (test with `stty size` or observe line wrapping)
- Open second client with smaller window - both should see smaller size applied
- Close smaller client - remaining client's size should be used

**Commit:** `feat: add terminal resize handling (smallest wins)`

---

### Step 10: Mobile-Friendly UI ✅

**Goal:** Extra keys row for mobile (ESC, TAB, arrows), touch-friendly. Status bar with connection state and reconnection.

**Files to modify:**
- `static/index.html` - add extra keys row, status bar, reconnection logic

**Test:**
- Open on mobile viewport - extra keys visible
- Tap ESC, TAB, arrows - correct sequences sent
- Status bar shows "Connected" with uptime timer
- Disconnect - shows "Reconnecting in Xs..." with countdown
- Reconnect - status returns to "Connected"

**Commit:** `feat: add mobile UI with status bar and auto-reconnect`

---

### Step 11: Web Component Wrapper ✅

**Goal:** Wrap terminal UI in `<terminal-ui>` web component.

**Files to create/modify:**
- `static/terminal-ui.js` - web component definition
- `static/index.html` - use `<terminal-ui uuid="...">` element

**Test:**
- Page renders using web component
- All functionality preserved
- Component encapsulates all terminal logic

**Commit:** `refactor: wrap terminal in web component`

---

### Step 12: Final Polish ✅

**Goal:** Error handling, logging, edge cases.

**Tasks:**
- Add proper error messages for WebSocket failures
- Add connection status indicator
- Handle rapid reconnects gracefully
- Add `-addr` flag for listen address
- Add startup banner with configuration

**Test:**
- Server logs show meaningful information
- Client shows connection status
- Disconnect/reconnect works smoothly

**Commit:** `chore: add error handling and polish`

---

## Progress Tracking

| Step | Description | Status | Commit |
|------|-------------|--------|--------|
| 1 | Project Scaffold | ✅ | `feat: scaffold terminal web app with go:embed` |
| 2 | xterm.js Integration | ✅ | `feat: integrate xterm.js terminal rendering` |
| 3 | UUID Session Routing | ✅ | `feat: add UUID-based session routing` |
| 4 | WebSocket Endpoint | ✅ | `feat: add WebSocket endpoint with echo` |
| 5 | PTY Session Management | ✅ | `feat: add PTY session with configurable shell` |
| 6 | Multiplayer Session Sharing | ✅ | `feat: add multiplayer session broadcasting` |
| 7 | Session TTL and Cleanup | ✅ | `feat: add session TTL and cleanup reaper` |
| 8 | Process Restart on Death | ✅ | `feat: add automatic process restart on death` |
| 9 | Terminal Resize Handling | ✅ | `feat: add terminal resize handling (smallest wins)` |
| 10 | Mobile-Friendly UI | ✅ | `feat: add mobile UI with status bar and auto-reconnect` |
| 11 | Web Component Wrapper | ✅ | `refactor: wrap terminal in web component` |
| 12 | Final Polish | ✅ | `chore: add error handling and polish` |

**Legend:** ⬜ Not started | 🔄 In progress | ✅ Complete | ❌ Blocked
