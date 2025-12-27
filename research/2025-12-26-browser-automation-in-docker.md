# Browser Automation in Docker: Observability & Architecture

## Design Decisions (Confirmed)

1. **Default enabled** - Browser automation is opt-out, not opt-in
2. **MCP Playwright in swe-swe-server** - MCP Playwright server runs inside swe-swe-server container, connects to chrome sidecar via CDP
3. **System prompt injection at `-shell`** - Browser tool instructions injected where agent instructions are configured
4. **Traefik subdomain routing** - Access noVNC at `chrome.{base_domain}` instead of raw port 6080
5. **Stateless sessions** - Cookies/state reset on container restart (acceptable for automation)

---

## Problem Statement

When running `swe-swe up`, the swe-swe-server and Claude process run inside Docker. To enable Claude to interact with browsers using MCP Playwright (or similar tooling), we need:
1. Claude (in Docker) to control a browser instance
2. Developer to observe the browser UI in real-time
3. Minimal setup complexity and cross-platform compatibility

## Solution Approaches

### Option 1a: Browser in Sidecar + Xvfb + noVNC ⭐ (Best Overall)

**Architecture:**
- Run Chrome in Docker container with Xvfb (X Virtual Framebuffer)
- Chrome renders into virtual display (not headless)
- Expose Chrome DevTools Protocol for CDP connection
- Run noVNC to expose framebuffer over WebSockets
- Developer opens browser tab to observe rendered Chrome window in real-time

**Implementation:**
```dockerfile
# chrome/Dockerfile
FROM ubuntu:22.04

RUN apt-get update && apt-get install -y \
    xvfb \
    novnc \
    websockify \
    chromium-browser

EXPOSE 9222 6080

CMD bash -c "Xvfb :99 -screen 0 1920x1080x24 & \
    sleep 1 && \
    DISPLAY=:99 chromium-browser --remote-debugging-port=9222 --no-sandbox & \
    vncserver :1 -geometry 1920x1080 -depth 24 & \
    websockify 6080 localhost:5901"
```

```yaml
# docker-compose.yml
services:
  swe-swe-server:
    # existing config
    depends_on:
      - chrome
    environment:
      - BROWSER_WS_ENDPOINT=ws://chrome:9222
    networks:
      - swe-swe

  chrome:
    build: ./chrome
    ports:
      - "9222:9222"    # Chrome DevTools Protocol (CDP)
      - "6080:6080"    # noVNC web UI
    networks:
      - swe-swe

networks:
  swe-swe:
```

**Developer Workflow:**
1. `swe-swe up`
2. Open `http://localhost:6080` in browser (noVNC)
3. See actual rendered Chrome window
4. Watch Claude fill fields, click buttons, navigate pages
5. Can screenshot/record the VNC stream for debugging

**Advantages:**
- ✅ **Full visual observability** (actual rendered pixels)
- ✅ Fully containerized and reproducible
- ✅ Works on all platforms (Linux, Mac, Windows)
- ✅ Easy to screenshot/record interactions for debugging
- ✅ Single container solution (no host setup needed)
- ✅ Same setup works locally and in CI
- ✅ DevTools + visual feedback combined

**Disadvantages:**
- X11/VNC adds ~50-100MB RAM overhead
- Slightly larger Docker image
- Rendering ~50-100ms slower than native browser

---

### Option 1b: Browser in Sidecar + DevTools Only (Lightweight)

**Architecture:**
- Run headless Chrome in Docker container
- Expose Chrome DevTools Protocol only
- Developer inspects DOM/logs/console via DevTools

**Implementation:**
```yaml
services:
  chrome:
    image: browserless/chrome:latest
    ports:
      - "9222:9222"
    environment:
      - HEADLESS=true
```

**Developer Workflow:**
1. `swe-swe up`
2. Open Chrome DevTools pointing to `localhost:9222`
3. See DOM changes, console logs, network requests
4. No visual observation of rendered page

**Advantages:**
- Minimal overhead
- Lightweight image
- Native DevTools inspection

**Disadvantages:**
- **No visual observability** (inspection only, not rendered pixels)
- Can't watch interactions happen
- Harder to debug UI issues visually

---

### Option 1c: Browser in Sidecar + Screenshot Polling (Alternative Lightweight)

**Architecture:**
- Chrome renders headless (no X server overhead)
- Claude periodically captures screenshots
- Sends to host API endpoint
- Frontend displays latest screenshot

**Simple Implementation:**
```javascript
// In Claude's browser automation
const screenshot = await page.screenshot({ path: '/tmp/latest.png' });
await fetch('http://host:3000/screenshot', {
  method: 'POST',
  body: screenshot
});
```

**Advantages:**
- Minimal container overhead
- No X11/VNC dependencies

**Disadvantages:**
- Not real-time (polling latency ~1-5s)
- Discrete screenshots, not continuous observation
- Claude overhead from screenshot operations

---

### Option 2: Browser on Host + Network Connection ⭐ (Best for Visual Observability)

**Architecture:**
- Developer runs Chrome locally with `--remote-debugging-port=9222`
- Claude in Docker connects via `host.docker.internal` (Docker Desktop) or host IP
- Browser UI visible directly on developer's machine

**Implementation:**
```bash
# Host machine (developer runs this)
chrome --remote-debugging-port=9222 --no-sandbox
```

**Playwright code in Docker:**
```javascript
const browser = await playwright.chromium.connectOverCDP('http://host.docker.internal:9222');
```

**Developer Experience:**
- Sees actual Chrome window on screen
- Watches Claude fill input fields in real-time
- Can take screenshots showing the interaction sequence
- See form submissions, page navigation, element interactions
- Full visual feedback of browser state changes

**Advantages:**
- Simpler setup (no extra container)
- **Full visual observability** (actual browser window, not just DevTools logs)
- Developer sees UI changes in familiar browser
- Can manually interact with browser while Claude runs
- Easy to take screenshots and debug UI issues visually

**Disadvantages:**
- Less reproducible (relies on host setup)
- `host.docker.internal` only works on Docker Desktop (not Linux native)
- Harder to standardize across team
- Requires manual host browser startup
- Not suitable for CI/remote deployments

---

### Option 3: MCP Playwright Proxy (Network-based)

**Architecture:**
- Run MCP Playwright server on host
- Expose via HTTP/WebSocket bridge
- Claude in Docker makes network requests
- Requires custom transport layer

**Advantages:**
- Reuses existing MCP Playwright setup

**Disadvantages:**
- MCP spec doesn't natively support network transport
- Requires significant custom bridging code
- No clear observability path
- More complexity for minimal benefit

---

## Recommendation: Option 1a (Confirmed)

**Use Option 1a (Xvfb + noVNC)** as the default:
- ✅ Full visual observability (watch Claude interact with rendered page)
- ✅ Fully containerized (reproducible, works on Linux/Mac/Windows)
- ✅ Works locally and in CI/deployment pipelines
- ✅ Developers see actual browser window + DevTools inspection
- ✅ Only trade-off: ~50-100MB RAM + ~50-100ms render latency (negligible for automation)
- ✅ Default enabled, users can opt-out if not needed

## Integration with `swe-swe init`

### Default Enabled (Opt-Out)

Browser automation is **enabled by default**. Users can disable if not needed.

**Default behavior:**
- `chrome` service included in docker-compose.yml
- `BROWSER_WS_ENDPOINT=ws://chrome:9222` set in swe-swe-server environment
- MCP Playwright server configured in swe-swe-server
- `docs/BROWSER_AUTOMATION.md` included with setup instructions
- Traefik routes `chrome.{base_domain}` to noVNC

**To disable:**
- Remove `chrome` service from docker-compose.yml
- Remove `BROWSER_WS_ENDPOINT` env var
- Claude gracefully skips browser-based tasks

### Traefik Routing for noVNC

Instead of exposing raw port 6080, route through Traefik for consistency:

```yaml
# docker-compose.yml
services:
  chrome:
    build: ./chrome
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.chrome.rule=HostRegexp(`chrome.{domain:.+}`)"
      - "traefik.http.routers.chrome.entrypoints=web"
      - "traefik.http.services.chrome.loadbalancer.server.port=6080"
    networks:
      - swe-swe
```

**Benefits:**
- Consistent URL scheme: `chrome.localhost`, `vscode.localhost`, etc.
- No port numbers to remember
- Future TLS/auth handled by Traefik
- WebSocket support works by default (noVNC uses WS)

**Note:** CDP port 9222 stays internal-only (swe-swe-server → chrome). Not exposed via Traefik.

### Instructing Claude to Use MCP Playwright

System prompt injection at `-shell` (when BROWSER_WS_ENDPOINT is set):

```
You have browser automation via MCP Playwright tools:
- mcp__playwright__browser_navigate, mcp__playwright__browser_click,
  mcp__playwright__browser_type, mcp__playwright__browser_snapshot, etc.
- Browser renders to a virtual display visible at chrome.{base_domain}
- Session/cookies reset on container restart (stateless)
- Use browser tools for web scraping, form filling, UI testing, debugging web apps
```

**Supporting Documentation:**
- Include `docs/BROWSER_AUTOMATION.md` in generated project (see sample below)
- Document:
  - How to access noVNC at http://localhost:6080
  - Playwright connection example
  - Common patterns (waiting for elements, handling timeouts)
  - Observability tips (taking screenshots at key points)
  - Debugging (checking browser logs, network requests)

---

## Technical Unknowns (Need Verification)

### 1. MCP Playwright Remote CDP Support ⚠️ CRITICAL
**Question:** Does MCP Playwright support connecting to a remote browser via CDP endpoint?

- Standard Playwright supports `connectOverCDP(wsEndpoint)`
- MCP Playwright may have different configuration mechanism
- Need to check: environment variable? Config file? Command-line arg?

**Verification:** Check MCP Playwright docs/source for remote browser connection support.

### 2. MCP Playwright Bundling in swe-swe-server
**Question:** Is MCP Playwright already in the swe-swe-server container, or needs to be added?

- Check current Dockerfile/dependencies
- May need to add `@anthropic/mcp-playwright` or similar package
- Need to configure MCP server list

**Verification:** Check swe-swe-server Dockerfile and MCP config.

### 3. Xvfb + noVNC Container Stability
**Question:** Does the proposed Dockerfile actually work reliably?

- Race conditions between Xvfb, Chrome, and noVNC startup
- May need process supervisor (supervisord) instead of bash `&`
- Chrome sandbox issues in container

**Verification:** Build and test the chrome container in isolation.

### 4. Traefik WebSocket Routing for noVNC
**Question:** Does Traefik properly handle noVNC WebSocket upgrade?

- noVNC uses WebSockets for VNC-over-web
- Traefik should handle WS by default, but worth confirming
- May need specific middleware config

**Verification:** Quick test with Traefik + simple WS server, or check existing Traefik config.

---

## Implementation Roadmap (Option 1a: Xvfb + noVNC)

### Phase 1: Docker Setup
- Create `chrome/Dockerfile` with Xvfb + noVNC + chromium-browser
- Build and test container in isolation
- Expose ports 9222 (CDP) and 6080 (noVNC web UI)
- Verify Xvfb framebuffer initialization (may need sleep delays)

### Phase 2: docker-compose.yml Integration
- Add chrome service to docker-compose.yml
- Configure networking between swe-swe-server and chrome containers
- Set `BROWSER_WS_ENDPOINT=ws://chrome:9222` env var
- Test `docker-compose up` brings up both services

### Phase 3: Playwright Integration
- Update Claude browser initialization to use `browser.connect()`
- Handle graceful fallback if browser unavailable
- Test CDP connection from swe-swe-server → chrome container

### Phase 4: Developer Documentation
- Document how to access noVNC at `http://localhost:6080`
- Explain port mappings (9222, 6080)
- Add screenshot guide for observing Claude interactions
- Include VNC keyboard shortcuts (copy/paste, etc.)
- Troubleshooting: Xvfb not starting, websockify connection issues

### Phase 5: Optional Enhancements
- Add VNC password protection if exposing over network
- Pre-record macros for common debugging tasks
- Add recording option to noVNC (if available)
- Consider headless fallback if Xvfb unavailable (graceful degradation)

## Expected Results

**Developer Experience:**
```
$ docker-compose up
# Opens terminal
# In browser, go to http://localhost:6080
# Sees Chrome window being controlled by Claude
# Watches interactions in real-time
# Can inspect Chrome DevTools separately on localhost:9222
```

**Benefits:**
- Single `docker-compose up` starts everything
- Works identically on Linux, Mac, Windows
- Reproducible setup for the entire team
- Full visual debugging capability
- No manual host setup required
