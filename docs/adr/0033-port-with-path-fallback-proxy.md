# ADR-033: Port-based proxy with path-based fallback

**Status**: Accepted
**Date**: 2026-02-22

## Context

ADR-025 introduced per-session preview ports with dedicated Traefik entrypoints. Each session's preview proxy listened on a derived port (e.g., 3000 → 23000), and Traefik routed host:23000 to the container with forwardAuth. This required 40 Traefik entrypoints, 40 host port bindings, and per-session router labels — about 120 lines of generated docker-compose config.

This worked well for local Docker setups where all ports are reachable. But some deployment environments (cloud VMs behind firewalls, corporate networks, container orchestrators) can't expose 40 extra ports. The port-based approach was an all-or-nothing proposition.

We then moved to path-based routing: both preview and agent chat proxies are hosted inside swe-swe-server at `/proxy/{uuid}/preview/` and `/proxy/{uuid}/agentchat/` on the main port (:9898). This eliminated all extra ports but gave up the per-origin isolation that port-based routing provided (separate origins mean separate cookie jars, localStorage, service workers).

We want both: port-based when the infrastructure supports it (better isolation), path-based as an automatic fallback when it doesn't.

## Decision

Always generate port-based infrastructure. Always register path-based handlers. Let the browser discover which mode works at runtime.

### Infrastructure (init-time)

`swe-swe init` generates docker-compose.yml with all Traefik entrypoints, port bindings, and router labels for the preview port range — exactly as ADR-025 described. The `--proxy-port-offset` flag (default 20000) controls the derived port formula. This is static config, identical for every deployment.

### Server (runtime)

swe-swe-server does both:

1. **Path-based handlers** — registers `/proxy/{uuid}/preview/` and `/proxy/{uuid}/agentchat/` on the main HTTP mux (port 9898), using the embedded `agent-reverse-proxy` Go library. This is the current implementation, unchanged.

2. **Per-port listeners** — when a session is created with previewPort=3000, starts additional HTTP listeners on the proxy ports (23000 for preview, 24000 for agent chat). These listeners delegate to the same embedded proxy instance — no separate processes, no code duplication. The per-port listener is a thin `http.ListenAndServe` wrapper around the existing handler.

Both coexist. The proxy logic runs once (embedded Go library); the routing is just two ways to reach it.

### Agent bridge (runtime)

The stdio bridge always uses the internal path-based URL:

```
npx @choonkeat/agent-reverse-proxy --bridge http://swe-swe:3000/proxy/$SESSION_UUID/preview/mcp
```

This is container-internal (never goes through Traefik), so it always works regardless of which mode the browser uses. No change from the current implementation.

### Browser probe (runtime)

The browser discovers which mode to use via a two-phase probe:

1. **Phase 1: Path probe** — `probeUntilReady(pathBasedUrl)`. This checks whether the target app is running at all. If this fails, neither mode will work — the app isn't up yet, so keep retrying.

2. **Phase 2: Port probe** — once the path probe succeeds (app is up), the browser makes a quick probe to the port-based URL (e.g., `https://hostname:23000/`). If reachable, use port-based URLs for the session. If not (blocked port, firewall, etc.), stay on path-based URLs.

The decided mode is stored for the session. All subsequent URL construction (iframe src, debug WebSocket, agent chat) follows the chosen mode consistently.

### URL construction

`url-builder.js` exports both styles:

- `buildPreviewUrl(baseUrl, sessionUUID)` → `/proxy/{uuid}/preview` (path-based, existing)
- `buildPortBasedPreviewUrl(location, previewProxyPort)` → `protocol://hostname:{proxyPort}` (port-based, restored)

The caller (terminal-ui.js) picks based on the probe result. Same pattern for agent chat URLs.

### Status message

The WebSocket status message includes everything the browser needs for both modes:

- `sessionUUID` — for path-based URL construction
- `previewPort` — the app port (for display in the URL bar: `localhost:3000`)
- `previewProxyPort` — the derived proxy port (for port-based URL construction)
- `agentChatPort` — the agent chat app port
- `agentChatProxyPort` — the derived agent chat proxy port

## Why this works well

**No init-time branching.** Templates are always the same — no `{{IF PATH_PROXY}}` conditionals. `swe-swe init` generates the full port-based infrastructure every time. Environments that can't use the ports simply ignore them.

**No configuration.** Users don't need to know whether their infrastructure supports extra ports. The browser figures it out automatically. No `--proxy-mode` flag.

**One proxy implementation.** The embedded `agent-reverse-proxy` Go library is the single source of truth for proxying. Per-port listeners are one-line wrappers that delegate to the same handler. No process management, no IPC, no code duplication.

**Graceful degradation.** If ports are blocked, the user gets path-based routing with zero friction. If ports are available, they get per-origin isolation automatically.

**The bridge is immune.** Agent-to-proxy communication uses the internal path-based URL and never goes through Traefik. Changing the browser's routing mode doesn't affect the agent at all.

## Consequences

**Good:**
- Works on both port-open and port-restricted infrastructure without configuration.
- Port-based mode provides per-origin isolation (separate cookie jars, localStorage, service workers).
- Single proxy implementation regardless of mode.
- No init-time template branching — one set of templates, one set of golden tests for proxy config.
- Bridge always works via internal path-based URL.
- The browser probe adds negligible latency (one extra fetch after the path probe succeeds).

**Bad:**
- docker-compose.yml is larger (includes all port bindings even when they'll be unused in path-based fallback).
- `url-builder.js` has two URL construction paths (port-based and path-based).
- The browser must track which mode was chosen per-session for consistent URL construction.
- Port-based mode still requires the Traefik entrypoints to be generated at init time, so the port range is fixed (same limitation as ADR-025).
