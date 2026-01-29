# Per-Session Preview Ports

## Goal

Each session gets its own preview port so multiple sessions don't collide on port 3000. The Preview tab shows the assigned port with a copy button. Routing uses subdomains for clean isolation.

## Design Decisions (from discussion)

### Port Allocation
- Start from port 3000, increment +1
- Skip if already assigned (our file) or already bound (OS check via `net.Listen`)
- No arbitrary range ceiling
- Persist assignments to disk: `/workspace/.swe-swe/preview-port-assignments.json`
  ```json
  {
    "3000": "session-uuid-1",
    "3001": "session-uuid-2"
  }
  ```
  - Key = port (easy to find next open port)
  - Value = session UUID

### Port Lifecycle
- **New session** → allocate port, write to file
- **Session reconnects after server restart** → read file, reuse existing assignment
- **Session ends** → remove from file, port is free
- **Server starts** → load file, all listed ports are reserved (no probing — app may not be running yet)

### Subdomain-Based Routing
- Each session's preview iframe uses: `{port}.https.local.swe-swe.com:{previewPort}`
- Proxy reads port number from `Host` header subdomain
- Strips proxy cookie `__swe_preview_proxy_target` before forwarding (never leaked to app)
- No cookies needed for routing — subdomain IS the routing
- No path prefix issues — apps work unmodified
- Each iframe has its own origin — full cookie/storage isolation between sessions

### DNS & TLS for Subdomains
- DNS: wildcard records for `*.http.local.swe-swe.com` and `*.https.local.swe-swe.com` → `127.0.0.1`
- TLS: wildcard cert for `*.https.local.swe-swe.com` via Let's Encrypt DNS-01 challenge (DNSimple)
- Centralized cert server at `certs.swe-swe.com`:
  - Runs ACME client (certbot/lego) with DNSimple API credentials
  - Serves cert+key over HTTPS
  - Private key distribution is safe — cert only covers localhost-resolving domains
- swe-swe instances fetch certs:
  - On `swe-swe init` — download to shared volume
  - swe-swe-server runs background goroutine to re-fetch daily
  - Writes to shared Docker volume (`certs:/certs`)
  - Atomic write (write tmp, rename)
- Traefik:
  - Mounts certs volume read-only (`:ro`)
  - File provider with `watch: true` — hot-reloads on cert file change
  - No Traefik restart needed

### Preview Tab UX
- Placeholder message: "Start a hot-reload web app on port http://localhost:{port}"
- Copy button next to the URL for easy paste into agent terminal
- Port is session-specific, shown dynamically

### Agent Priming
- Agent docs (`app-preview.md`, `debug-with-app-preview.md`) updated to say "use the port shown in your Preview tab" instead of hardcoding 3000
- User copies the port from Preview tab and tells their agent — user is the coordinator
- `SWE_PREVIEW_TARGET_PORT` env var may still be set per-session for backwards compat

### WebSocket
- Debug WebSocket URL includes port in query string: `?__swe_target=3007`
- Proxy reads port from query string for WebSocket connections
- Subdomain routing also works for WebSocket (Host header present on upgrade)

## Phases

### Phase 1: DNS & Cert Infrastructure
- [ ] Set up wildcard DNS records at DNSimple for `*.http.local.swe-swe.com` and `*.https.local.swe-swe.com` → `127.0.0.1`
- [ ] Build/deploy cert server at `certs.swe-swe.com` (ACME + DNSimple + static file serve)
- [ ] Verify: `curl https://certs.swe-swe.com/cert.pem` returns valid wildcard cert

### Phase 2: Cert Fetching in swe-swe
- [ ] `swe-swe init` downloads cert+key to shared Docker volume
- [ ] swe-swe-server background goroutine re-fetches daily
- [ ] Atomic file writes (tmp + rename)
- [ ] Traefik config: mount certs volume `:ro`, file provider watches for changes

### Phase 3: Port Allocation & Persistence
- [ ] Add `PreviewTargetPort` to `Session` struct
- [ ] Implement port allocator: start at 3000, skip assigned (file) and bound (OS)
- [ ] Persist to `/workspace/.swe-swe/preview-port-assignments.json` (`{port: uuid}`)
- [ ] Load file on server start to reserve ports
- [ ] Remove entry on session end
- [ ] Session creation API returns `previewTargetPort` field

### Phase 4: Subdomain Proxy Routing
- [ ] Traefik rule: `HostRegexp` for `{port}.https.local.swe-swe.com` routes to preview proxy
- [ ] Preview proxy reads port from `Host` header subdomain
- [ ] Strip `__swe_preview_proxy_target` cookie before forwarding to app
- [ ] Remove stateful `POST/GET /__swe-swe-debug__/target` API and associated mutex/globals
- [ ] Validate parsed port is within reasonable range and matches an active assignment

### Phase 5: Frontend & UX
- [ ] Preview iframe `src` uses `{port}.https.local.swe-swe.com:{previewPort}`
- [ ] Preview placeholder shows session-specific port
- [ ] Add copy button for the localhost URL
- [ ] WebSocket debug URL includes port in query string or uses subdomain

### Phase 6: Documentation
- [ ] Update `app-preview.md` — remove hardcoded 3000, reference "port shown in Preview tab"
- [ ] Update `debug-with-app-preview.md` slash command
- [ ] Remove or update `SWE_PREVIEW_TARGET_PORT` references
