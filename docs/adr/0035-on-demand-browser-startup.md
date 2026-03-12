# ADR-0035: On-Demand Browser Startup

**Status**: Accepted
**Date**: 2026-03-12

## Context

ADR-0034 moved from a shared Chrome sidecar to per-session Chrome/Xvfb/VNC processes. This eliminated cross-session interference but still started the full browser stack at session creation, even for sessions that never use the browser.

Measured cost per session:

| Component | RSS |
|-----------|-----|
| Chromium (all processes) | ~1,100 MB |
| Playwright MCP (Node.js) | ~227 MB |
| VNC + websockify | ~112 MB |
| Xvfb | ~77 MB |
| **Total** | **~1,529 MB** |

Most sessions are code-only — they never touch the browser. Starting ~1.5 GB of browser processes unconditionally wastes significant memory, especially on hosts running multiple concurrent sessions.

## Decision

Defer browser startup until the first Playwright MCP tool call using a generic lazy-init proxy (`mcp-lazy-init`).

### How it works

1. **At session creation**: Instead of starting Playwright MCP directly, the entrypoint launches `mcp-lazy-init` as a stdio wrapper. This costs ~5 MB per session.

2. **On first MCP tool call**: `mcp-lazy-init` intercepts the first incoming JSON-RPC message, sends an HTTP POST to the swe-swe-server's `/api/session/{id}/browser/start` endpoint (which starts Xvfb, Chromium, x11vnc, and websockify), then spawns the real Playwright MCP process and forwards all traffic.

3. **After startup**: `mcp-lazy-init` becomes a transparent stdio passthrough. All subsequent MCP calls go directly to Playwright MCP with no overhead.

### mcp-lazy-init design

`mcp-lazy-init` is a generic tool — not browser-specific. It takes:

```
mcp-lazy-init --init-method POST --init-url <url> -- <command> [args...]
```

On first stdin message, it calls the init URL, waits for success, then execs the wrapped command. This pattern can be reused for any MCP server that needs expensive resource initialization.

### Browser start API

The server exposes `POST /api/session/{id}/browser/start` which:
- Starts Xvfb, Chromium, x11vnc, and websockify for the session
- Waits for CDP to become responsive (polls `/json/version`)
- Returns 200 on success, 500 on failure
- Is idempotent — subsequent calls are no-ops if already started

## Consequences

### Positive
- **~1.5 GB saved per code-only session** — the majority of sessions
- Startup delay (~2-3s) only incurred when browser is actually needed
- `mcp-lazy-init` is reusable for other expensive MCP servers
- No user-facing behavior change — browser tools work transparently

### Negative
- ~2-3 second delay on the first Playwright MCP tool call
- Additional process (`mcp-lazy-init`) in the stdio chain, though it exits after handoff
- Agent View tab shows a blank/waiting state until browser starts (Phase 5 will hide it entirely)

### Neutral
- Port allocation unchanged from ADR-0034
- VNC/noVNC still available for visual observation once started
