# ADR-027: WebSocket proxy relay

**Status**: Accepted
**Date**: 2026-02-02

## Context

The preview proxy in `handleProxyRequest` treats `Upgrade` and `Connection` as hop-by-hop headers and strips them. This breaks any user app that uses WebSockets:

- Hot-reload dev servers (Vite, webpack-dev-server, Next.js)
- Real-time applications (chat, live updates, collaborative editing)
- WebSocket-based APIs

Users see their app load but WebSocket connections fail silently, causing confusing behavior (no hot reload, no real-time updates).

## Decision

Detect WebSocket upgrade requests and relay them as raw TCP connections:

```go
// At the top of handleProxyRequest
if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
    // Hijack, dial backend, relay bidirectionally
}
```

### Implementation

1. Check for `Upgrade: websocket` header (case-insensitive)
2. Hijack the incoming connection via `http.Hijacker`
3. Dial the backend at `localhost:PORT`
4. Reconstruct and write the raw HTTP upgrade request to backend
5. Read the backend's 101 Switching Protocols response, forward to client
6. `io.Copy` bidirectionally in two goroutines
7. Close both sides when either closes

### Scope

- The `/__swe-swe-debug__/*` endpoints are matched by the mux before `/` and are unaffected
- Only the catch-all proxy handler gains WebSocket support
- Debug injection is not applied to WebSocket connections (they're binary frames, not HTML)

## Consequences

**Good:**
- Dev servers with hot reload work in preview
- Real-time apps function correctly
- No configuration required — automatic detection

**Bad:**
- Raw TCP relay bypasses HTTP-level logging/debugging
- Connection errors are harder to diagnose (no HTTP status codes after upgrade)
- Slightly more complex proxy code path
