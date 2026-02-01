# ADR-025: Per-session preview ports

**Status**: Accepted
**Date**: 2026-02-01

## Context

Each swe-swe session can run a web app (e.g. `npm run dev`). A reverse proxy serves that app in a Preview iframe within the session UI, injecting a debug script for console/network forwarding to the agent.

Previously there was a single preview proxy on a fixed port. All sessions shared it. A `POST /__swe-swe-debug__/target` API switched the proxy's target URL, protected by a mutex. This meant:

- Only one session could use the preview at a time. Starting a dev server in session B silently stole the preview from session A.
- The debug WebSocket hub was global — console logs from session A's app could leak to session B's agent.
- There was no `PORT` injection, so the user had to manually configure their framework to match the hardcoded port.

We needed per-session isolation of the preview target, the reverse proxy, and the debug channel.

## Options considered

### Option A: Cookie-based routing

A single shared proxy reads a session-identifying cookie to look up each session's target URL.

**Pros:**
- Single port, single Traefik entrypoint — minimal infrastructure.
- No init-time configuration of port ranges.

**Cons:**
- The preview iframe and the proxied app share an origin. App cookies, localStorage, and service workers from session A are visible to session B. Clearing cookies in one session affects all sessions.
- The proxy must maintain a mutable map of session→target protected by a mutex — the same stateful design we were trying to eliminate.
- The debug WebSocket hub is still shared. Demuxing iframe↔agent messages by session adds complexity and error surface.
- Frameworks don't get a unique `PORT` — every session's app binds to the same port, so only one can run at a time anyway. The routing isolation is cosmetic if the apps still collide on `localhost:3000`.

This option solves the proxy-routing problem but not the port-collision problem, which is the actual user pain.

### Option B: Path-prefix routing

A single proxy uses a path prefix (e.g. `/_preview/{sessionID}/`) to route to different backends. The prefix is stripped before forwarding.

**Pros:**
- Single port, single entrypoint.
- No cookies needed for routing.

**Cons:**
- Path-prefix stripping breaks most web apps. SPAs emit absolute paths (`/assets/main.js`, `/api/data`) that bypass the prefix. Rewriting HTML, CSS, and JS to inject prefixes is fragile and framework-specific.
- Same origin-sharing problems as cookie routing (all previews on one origin).
- Same port-collision problem — apps still fight over `localhost:3000`.
- Debug endpoints need prefix-aware routing too.

We've seen path-prefix proxying break apps in the VSCode sidecar era (ADR-006) and chose to move away from it.

### Option C: Per-session preview ports (chosen)

Each session gets a unique app port (3000-3019). A per-session reverse proxy listens on `5{PORT}` (e.g. 3007 → 53007). The `PORT` env var is injected into the session process.

**Pros:**
- Full isolation: separate origin per preview, separate debug hub per session, no shared mutable state.
- `PORT` injection means frameworks bind to the right port automatically — the port-collision problem is solved at the source.
- Each proxy is stateless (fixed target = `localhost:{PORT}`), eliminating the target-switching API and its mutex.
- Debug hub is per-proxy, so console logs never leak between sessions.

**Cons:**
- Fixed port range limits concurrent preview-capable sessions (20 with default 3000-3019).
- Each port needs a Traefik entrypoint and router labels (~6 lines × 20 ports in docker-compose).
- The `5{PORT}` convention is arbitrary and must stay consistent across Go server, JS frontend, CLI debug defaults, and Traefik config.

## Decision

Option C: per-session preview ports.

The other options don't solve the actual problem (apps colliding on the same port). Once you accept that each session needs its own `PORT`, giving each session its own proxy on a derived port is the natural consequence.

### Sub-decisions

#### Port discovery: bind-probe, not file-based persistence

The original design persisted port assignments to a JSON file (`preview-port-assignments.json`) and reloaded on server restart. We dropped this in favor of bind-probing: for each port in the range, attempt `net.Listen` on both `:{PORT}` and `:5{PORT}`. First pair that succeeds wins.

Rationale: file persistence adds crash-recovery edge cases (stale entries from killed sessions, file corruption, concurrent access). Bind-probing is self-healing — if a port is genuinely in use, the OS tells you immediately.

#### Proxy lifecycle: ref-counted goroutine, not process

Each preview proxy is an `http.Server` in a goroutine, not a separate process. A global `map[int]*previewProxyServer` tracks active proxies with a `refCount`. Child (shell) sessions inherit the parent's port and increment the refcount. When the last session sharing a port closes, the proxy shuts down and the port is freed.

Rationale: goroutines are cheap and share the same address space (debug hub, logging, graceful shutdown come for free). A separate process would need IPC for debug forwarding and adds deployment complexity.

#### TLS: Traefik, not Go

Preview proxy ports are not exposed directly from the container. Traefik receives each `5{PORT}` as an entrypoint and routes it to the swe-swe-server container with `forwardAuth` middleware (same auth as the main UI).

The alternative — terminating TLS in the Go preview proxy — would require the app container to have access to the TLS private key. Currently only Traefik has the key, and we want to keep it that way. Routing through Traefik also gives us auth for free; a Go-side TLS proxy would need its own auth layer.

#### Subdomain routing: abandoned

The first design used wildcard subdomains (`{port}.https.local.swe-swe.com`) with a centralized cert server distributing wildcard certs via Let's Encrypt DNS-01 (DNSimple). This was abandoned because:

- External infrastructure dependency (DNS records, cert server, API credentials) for something that should work offline.
- Port-based Traefik entrypoints achieve the same per-origin isolation with zero external dependencies.
- The wildcard cert distribution problem (fetching, atomic file writes, Traefik hot-reload) adds operational surface for no functional gain.

#### `--preview-ports` flag

The port range is configurable at `swe-swe init` time via `--preview-ports=3000-3019`. This generates the Traefik entrypoints, port bindings, and router labels at init rather than dynamically.

Init-time generation (not runtime) is deliberate: docker-compose port mappings and Traefik entrypoints are static. You can't add entrypoints to a running Traefik without a restart. So the range must be known before `docker compose up`.

#### `PORT` env var injection

The session's allocated port is injected as `PORT={port}` in the process environment. Most frameworks (Next.js, Vite, Rails, Flask) respect `PORT` out of the box. This means the user doesn't need to configure anything — `npm run dev` just works on the right port.

The env var is set in both initial process creation and `RestartProcess`, so it survives session restarts.

#### Debug connection per proxy

Each preview proxy serves its own `/__swe-swe-debug__/ws` (iframe endpoint) and `/__swe-swe-debug__/agent` (agent endpoint) on its own `DebugHub`. The agent CLI defaults to `ws://localhost:5{PORT}/__swe-swe-debug__/agent` using the `PORT` env var already present in the session.

This replaced the old global debug hub where all sessions shared one WebSocket namespace.

#### UI: display URL vs. proxy URL

The preview URL bar shows the logical target (`http://localhost:3000/path`) while the iframe `src` uses the proxy URL (`https://host:53000/path`). These are tracked as separate values. The "open in new window" button uses the proxy URL.

This separation was a bug fix (the initial implementation conflated them), but it's an architectural constraint worth documenting: any proxy-based preview must maintain this distinction.

## Consequences

**Good:**
- Sessions run web apps concurrently without port collisions.
- `PORT` injection means frameworks work without configuration.
- Per-session debug hubs eliminate cross-session log leakage.
- No external infrastructure dependencies.
- Auth inherited from existing Traefik forwardAuth.

**Bad:**
- Fixed range (default 20 ports) caps concurrent preview-capable sessions.
- Docker-compose grows ~6 labels per preview port (120 lines for 20 ports).
- `5{PORT}` convention must stay consistent across 4 codepaths (Go, JS, CLI, Traefik config).
