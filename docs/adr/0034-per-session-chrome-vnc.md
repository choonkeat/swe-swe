# ADR-0034: Per-Session Chrome/VNC

## Status

Accepted

## Context

swe-swe runs a single shared Chrome container (`chrome` service in docker-compose) for all sessions. This container runs Chromium, Xvfb, x11vnc, noVNC, and an nginx CDP proxy, managed by supervisord. All sessions connect to the same Chrome instance via `ws://chrome:9223` for Playwright MCP automation.

This creates several problems:
1. **No browser isolation**: Playwright actions in one session (navigating, clicking, typing) affect all other sessions viewing the same Chrome instance.
2. **Resource waste**: Chrome runs continuously even when no session needs a browser, consuming ~300-500MB RAM.
3. **Architectural complexity**: A separate Docker container with its own Dockerfile, supervisord, nginx, and Traefik routing adds moving parts.

## Decision

Replace the shared Chrome container with per-session Chrome+Xvfb+VNC processes running inside the main swe-swe container.

### Approach: In-Process (Not Per-Session Containers)

We considered two approaches:

**Per-session containers** (rejected):
- Stronger isolation (memory limits, independent crash handling)
- Higher overhead: ~50-100MB container runtime + 3-5s startup per container
- Requires docker-in-docker or sibling container management
- More complex networking

**Per-session processes** (chosen):
- Follows existing pattern — sessions already manage PTY processes, preview proxies, agent-chat servers
- Lighter: no container creation overhead, Chrome starts in ~2-3s
- Simpler networking: everything is localhost
- Port allocation extends the existing triple (preview/agentchat/public) to a quintuple (+CDP/+VNC)
- Trade-off: weaker isolation, but consistent with existing session model

### Port Allocation

Extend the derived port pattern:
- CDP port = preview port + 3000 (range 6000-6019)
- VNC port = preview port + 4000 (range 7000-7019)

### X11 Display Isolation

Each session gets a unique X11 display number derived from its port allocation. Xvfb runs with `-nolisten tcp` to avoid TCP port conflicts — only Unix domain sockets are used.

### CDP Host Header

Chrome's DevTools Protocol rejects non-localhost Host headers. Currently handled by an nginx reverse proxy in the Chrome container. The replacement approach is to add Host header override capability to the `agent-reverse-proxy` library (already used by swe-swe-server), eliminating the need for nginx.

### Playwright MCP Configuration

Currently `BROWSER_WS_ENDPOINT=ws://chrome:9223` is set globally at container start. The new approach uses shell variable expansion in entrypoint.sh (existing pattern used by other MCP tools) so the CDP endpoint resolves per-session at runtime via the session's environment variables.

## Consequences

### Positive
- Each session has fully isolated browser state (tabs, cookies, history)
- No browser runs when no session needs one — better resource usage
- Simpler architecture: one fewer Docker container, no supervisord, no nginx CDP proxy
- Consistent with existing per-session process model

### Negative
- Main container image grows (Chrome + X11 packages)
- If Chrome crashes badly, it could theoretically affect the swe-swe-server process (mitigated by process group isolation)
- More processes to manage per session (5 additional: Xvfb, Chrome, x11vnc, noVNC, optional CDP proxy)

### Neutral
- User-facing port configuration unchanged (only preview-ports flag matters)
- All derived ports shift together when preview port range changes
