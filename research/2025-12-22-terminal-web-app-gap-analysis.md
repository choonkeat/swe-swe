# Terminal Web App - Gap Analysis Research

**Date:** 2025-12-22

## Overview

Research into existing solutions for building a Go web app that serves a terminal UI via xterm.js with shared multiplayer sessions.

## Requirements

1. **Go web app** using `go:embed` to serve all static assets (single binary)
2. **xterm.js** served from Go server (not CDN)
3. **UUID-based sessions**: visiting `/` redirects to `/session/{uuid}`
4. **PTY execution**: configurable shell command via `-shell` flag
5. **Multiplayer**: multiple browsers with same UUID share the session
6. **Mobile-first**: iOS/Android friendly with extra keys (ESC, TAB, arrows)
7. **WebSocket I/O**: terminal data piped over WebSocket

## Existing Solutions Analyzed

### ttyd (C)

- **URL:** https://github.com/tsl0922/ttyd
- **Status:** Actively maintained
- **Pros:**
  - Fast (libwebsockets + libuv)
  - Uses xterm.js
  - CJK and IME support
  - ZMODEM file transfer
- **Cons:**
  - Written in C (not Go)
  - No native shared sessions (requires tmux)
  - No mobile-specific features
  - No UUID-based session routing

### GoTTY (Go)

- **URL:** https://github.com/yudai/gotty
- **Status:** Unmaintained since 2017
- **Pros:**
  - Written in Go
  - Similar architecture to our needs
  - Uses xterm.js (via hterm)
- **Cons:**
  - Unmaintained
  - No native shared sessions (requires tmux)
  - No mobile support
  - No UUID-based routing

### tty2web (Go)

- **URL:** https://pkg.go.dev/github.com/kost/tty2web
- **Status:** Maintained (fork of GoTTY)
- **Pros:**
  - Written in Go
  - Improved over GoTTY
  - Bidirectional file transfer
- **Cons:**
  - Still requires tmux for shared sessions
  - No mobile-specific features
  - External build for frontend

## Gap Analysis

| Requirement | ttyd | GoTTY | tty2web | Our Design |
|-------------|------|-------|---------|------------|
| Go + `go:embed` | ❌ C | ✅ Go | ✅ Go | ✅ Native |
| UUID session routing | ❌ | ❌ | ❌ | ✅ `/session/{uuid}` |
| Native multiplayer | ❌ tmux | ❌ tmux | ❌ tmux | ✅ Direct PTY broadcast |
| Mobile-first UI | ❌ | ❌ | ❌ | ✅ Extra keys row |
| Configurable shell | ✅ | ✅ | ✅ | ✅ `-shell` flag |
| Single binary | ❌ | ❌ | ❌ | ✅ `go:embed` |

## Architecture Decision

**Build from scratch** rather than fork existing tools because:

1. **Native Go** - Clean `go:embed`, single binary deployment
2. **Native multiplayer** - Direct PTY broadcast without tmux dependency
3. **Custom session model** - UUID routing is first-class, not afterthought
4. **Mobile-first** - Extra keys built into web component
5. **Simpler codebase** - ~400 lines Go + ~100 lines JS vs forking complex projects

## Design Decisions

### Session Lifecycle

- Sessions persist even when all clients disconnect
- Configurable TTL via `-session-ttl` flag (default: 1h)
- Session expires after TTL of no client connections
- No maximum concurrent session limit

### Process Death Handling

- If the shell process exits, restart it automatically
- Default restart command: same as `-shell` value
- Override with `-shell-restart` flag (e.g., `claude --continue`)
- Session remains alive; clients see new process output

### Terminal Resize

- Multiple clients may have different terminal sizes
- Latest resize wins (simplest approach)
- Mobile devices will send their viewport dimensions
- Fixed size fallback: 80x24 if no resize received

### Input Conflict Resolution

- All client inputs are written directly to PTY
- No coordination, locking, or queuing
- Chaotic but simple - characters may interleave
- Natural turn-taking expected in practice (conversational CLI)

## Security Considerations

### xterm.js Security Surface

- xterm.js does NOT execute code - purely a renderer
- Interprets ANSI escape sequences (colors, cursor movement)
- OSC sequences can create hyperlinks, change titles
- Self-hosting is MORE secure than CDN (supply chain control)
- Past vulnerabilities (2019 DCS) have been patched

### Attack Vectors to Mitigate

1. **XSS via terminal output** - xterm.js handles escaping
2. **Session hijacking** - UUID provides minimal auth (sufficient for internal use)
3. **Resource exhaustion** - No max sessions (acceptable for trusted environments)

## Mobile Support Strategy

### iOS/Android Challenges

1. Keyboard only appears when input field focused
2. No physical ESC, TAB, CTRL, arrow keys
3. Predictive text can corrupt terminal input
4. Viewport changes when keyboard appears

### Solutions

1. **Extra keys row** - Persistent buttons above/below terminal
2. **Focus management** - Tap terminal to trigger keyboard
3. **Touch targets** - Minimum 44px for iOS compliance
4. **VirtualKeyboard API** - Handle viewport changes gracefully

## Key Dependencies

```go
import (
    "embed"                         // Static assets
    "github.com/creack/pty"         // PTY management
    "github.com/gorilla/websocket"  // WebSocket
    "github.com/google/uuid"        // Session IDs
)
```

## References

- [ttyd GitHub](https://github.com/tsl0922/ttyd)
- [GoTTY GitHub](https://github.com/yudai/gotty)
- [tty2web Go Package](https://pkg.go.dev/github.com/kost/tty2web)
- [xterm.js Security Guide](https://xtermjs.org/docs/guides/security/)
- [xterm.js Supported Sequences](https://xtermjs.org/docs/api/vtfeatures/)
- [creack/pty](https://github.com/creack/pty)
- [VirtualKeyboard API - MDN](https://developer.mozilla.org/en-US/docs/Web/API/VirtualKeyboard_API)
