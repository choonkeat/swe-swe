# Per-Session Chrome/VNC for Playwright

## Goal

Replace the single always-on Chrome container with on-demand, per-session Chrome+Xvfb+VNC processes inside the swe-swe container. Each session gets its own isolated browser instance.

## Motivation

- **Session isolation**: Currently all sessions share one Chrome. Playwright actions in one session affect all others.
- **Resource efficiency**: Chrome runs even when no session needs a browser. Per-session means Chrome only runs when needed.
- **Simpler architecture**: Eliminates a separate Docker container, Docker DNS hop, supervisord, and nginx CDP proxy.

## Architecture Decision

**In-process (chosen)** over per-session containers:
- Lighter: no container creation overhead (~50-100MB + 3-5s startup per container)
- Follows existing pattern: sessions already manage multiple processes (PTY, preview proxy, agent-chat)
- Port allocation pattern already exists for preview/agentchat/public ports
- Trade-off: weaker isolation than separate containers, but acceptable given existing session model

See ADR-0034 for full decision record.

---

## Phase 1: Per-Session Port Allocation for Browser ✅

### What
Extend the port triple (preview/agentchat/public) to include CDP and VNC ports per session. No Chrome changes yet — just the plumbing.

### Steps

1. Add `CDPPort` and `VNCPort` fields to the `Session` struct in `main.go`
2. Rename `findAvailablePortTriple()` to `findAvailablePortQuintuple()` (or similar), adding two derived ports:
   - CDP port = preview port + 3000 (range 6000-6019)
   - VNC port = preview port + 4000 (range 7000-7019)
3. Add `BROWSER_CDP_PORT` and `BROWSER_VNC_PORT` to `buildSessionEnv()`
4. Update `templates.go` to generate Traefik entrypoints and routers for VNC ports (for browser tab routing)
5. Update port cleanup in session teardown to include CDP and VNC ports

### Verification

- **Unit test**: Create a session, assert all 5 ports are allocated correctly and freed on session end
- **Regression**: Existing port allocation tests pass unchanged
- `make test` passes
- `make build golden-update` — golden files show new port ranges in docker-compose/traefik config

---

## Phase 2: Per-Session Chrome Process Spawning

### What
Each session launches its own Chrome, Xvfb, x11vnc, and noVNC when it starts, and tears them down when it ends.

### Steps

1. Install Chrome, Xvfb, x11vnc, noVNC in the main swe-swe `Dockerfile` template (packages: `chromium`, `xvfb`, `x11vnc`, `novnc`)
2. Create `startSessionBrowser(session)` function that:
   - Starts Xvfb on a unique DISPLAY (`:N` derived from preview port offset, e.g., port 3000 → `:1`)
   - Uses `-nolisten tcp` to avoid X11 TCP port conflicts
   - Starts Chromium with `--remote-debugging-port={CDPPort}` and `--display=:N`
   - Starts x11vnc on the VNC port, connected to the session's DISPLAY
   - Starts noVNC proxy connecting to the session's x11vnc
3. Track all browser process PIDs in the Session struct (`BrowserPIDs []int` or similar)
4. Create `stopSessionBrowser(session)` that kills all browser processes
5. Call `startSessionBrowser` in session creation flow
6. Call `stopSessionBrowser` in session cleanup flow (same pattern as existing process cleanup)
7. Handle display numbering: derive from `(previewPort - previewPortStart) + 1`

### Verification

- **Unit test**: Start a session browser, check processes are running and ports are listening, stop and verify cleanup
- **Regression**: Existing session lifecycle tests pass
- `make test` passes

---

## Phase 3: Dynamic Playwright MCP Configuration

### What
Make `BROWSER_WS_ENDPOINT` per-session instead of a single shared `ws://chrome:9223`.

### Steps

1. Change `buildSessionEnv()` to set `BROWSER_WS_ENDPOINT=ws://localhost:{CDPPort}` per session
2. Update `entrypoint.sh`: remove global `BROWSER_WS_ENDPOINT=ws://chrome:9223` from docker-compose env
3. For Playwright MCP config in `entrypoint.sh`:
   - Use shell variable expansion (existing pattern used by other MCP tools) so `--cdp-endpoint` references `$BROWSER_WS_ENDPOINT` or `$BROWSER_CDP_PORT`
   - This way MCP config is written once at container start, but the variable resolves per-session at runtime
4. Apply the same pattern for all agents: Claude, Gemini, Codex, Goose, Aider, OpenCode
5. Handle the nginx Host header trick for CDP:
   - Consider adding a Host header override option to `agent-reverse-proxy` library
   - Or implement a lightweight CDP proxy in swe-swe-server that sets `Host: localhost` when proxying to Chrome's CDP port

### Verification

- **Integration test**: Start two sessions, navigate to different URLs via Playwright in each, confirm they see different pages
- **Regression**: `make test` passes
- Verify each agent type's MCP config resolves correctly

---

## Phase 4: Per-Session Browser Tab in UI

### What
Route the "Agent View" tab to the session's own VNC endpoint instead of the shared `/chrome/`.

### Steps

1. Update `terminal-ui.js`: change browser tab URL from `${baseUrl}/chrome/` to `${baseUrl}/proxy/${sessionUUID}/browser/`
2. Add a proxy handler in swe-swe-server for `/proxy/{uuid}/browser/*` that routes to the session's noVNC port
3. Update noVNC wrapper HTML to use the correct WebSocket path for per-session routing
4. When session has no browser running (e.g., shell sessions), show a placeholder message in the browser tab
5. Handle WebSocket upgrade for noVNC connections through the proxy

### Verification

- **Manual test**: Open two sessions in different browser tabs, confirm each shows its own independent Chrome view
- **Regression**: Existing UI functionality unaffected
- Browser tab loads and VNC connects successfully

---

## Phase 5: Remove Chrome Container

### What
Delete the old shared Chrome container setup now that per-session browsers work.

### Steps

1. Remove `chrome` service block from `docker-compose.yml` template (lines 109-140)
2. Remove `depends_on: chrome` from swe-swe service
3. Remove `BROWSER_WS_ENDPOINT=ws://chrome:9223` from docker-compose env
4. Delete `chrome/` directory under `templates/host/`:
   - `chrome/Dockerfile`
   - `chrome/supervisord.conf`
   - `chrome/nginx-cdp.conf`
   - `chrome/entrypoint.sh`
   - `chrome/novnc-wrapper.html`
5. Remove Traefik labels/routing for `/chrome/` path
6. Update `--host-resolver-rules` Chrome flag: change `MAP localhost swe-swe` since Chrome is now in the same container (may not need this flag at all)
7. Remove embedded chrome template files from Go binary (`templates.go` / `embed` directives)

### Verification

- `make build` succeeds (no missing embedded files)
- `make test` passes
- `make build golden-update` — diff shows chrome service removal, Dockerfile additions
- Integration test via test container: boot container, create session, browser tab works, Playwright navigates, no chrome container exists

---

## Phase 6: Cleanup & Golden Tests

### What
Final verification, documentation updates, and golden test updates.

### Steps

1. `make build golden-update` — review full diff
2. Update tdspec:
   - `Topology.elm`: Remove chrome container, show browser processes inside swe-swe container
   - `McpTools.elm`: Update CDP endpoint documentation for per-session
3. Add CHANGELOG entry for per-session browser isolation
4. Run full `make test`
5. Integration test via test container workflow:
   - Boot container
   - Create two sessions
   - Verify each has its own browser
   - Run Playwright in each, confirm isolation
   - End sessions, verify browser processes cleaned up
6. **Do NOT modify historical ADRs or docs** — they record decisions at their point in time

### Verification

- `make test` passes
- Golden diff is clean and expected
- Integration test confirms end-to-end functionality
- No references to `chrome:9223` remain in active code (old ADRs are fine)

---

## Port Layout (After Implementation)

| Port Range | Purpose | Derived From |
|------------|---------|--------------|
| 3000-3019 | Preview (app) | Base range (configurable) |
| 4000-4019 | Agent Chat | Preview + 1000 |
| 5000-5019 | Public (no auth) | Preview + 2000 |
| 6000-6019 | CDP (Chrome DevTools) | Preview + 3000 |
| 7000-7019 | VNC (browser view) | Preview + 4000 |

External (via Traefik with proxy port offset 20000):
| 23000-23019 | Preview proxy |
| 24000-24019 | Agent Chat proxy |
| 25000-25019 | Public proxy |
| 26000-26019 | CDP proxy |
| 27000-27019 | VNC proxy |

## X Display Numbering

Each session gets a unique X11 display derived from its preview port:
- Display number = `(previewPort - previewPortStart) + 1`
- Preview port 3000 → DISPLAY=:1
- Preview port 3001 → DISPLAY=:2
- Uses `-nolisten tcp` so no TCP port conflicts (Unix sockets only)
