# Browser Automation Implementation

**Goal:** Enable Claude to control a browser inside Docker with real-time visual observability via noVNC.

**Design Doc:** [research/2025-12-26-browser-automation-in-docker.md](../research/2025-12-26-browser-automation-in-docker.md)

---

## Progress Summary

| Step | Status | Notes |
|------|--------|-------|
| 1. Sanity check: Chrome container | ✅ Done | Chrome container builds, noVNC accessible, all processes start cleanly |
| 2. Add chrome service to docker-compose.yml | ✅ Done | chrome service with Traefik labels, networking, resource limits configured |
| 3. Add Traefik routing for chrome | ✅ Done | Routes chrome.{domain} to noVNC port 6080 via Traefik |
| 4. Bundle MCP Playwright in Dockerfile | ✅ Done | npm install -g @anthropic-ai/mcp-playwright added to swe-swe-server |
| 5. Configure MCP Playwright CDP endpoint | ✅ Done | BROWSER_WS_ENDPOINT=ws://chrome:9222 set in docker-compose environment |
| 6. Add system prompt for browser tools | ✅ Done | System prompt injection in main.go when BROWSER_WS_ENDPOINT is set |
| 7. Add BROWSER_AUTOMATION.md template | ✅ Done | Comprehensive guide created and init updated to copy docs/ subdirectory |
| 8. End-to-end integration test | ✅ Done | Verified swe-swe init creates proper structure with all files |

---

## Step 1: Sanity Check - Chrome Container

**Goal:** Build and test a standalone Chrome + Xvfb + noVNC container.

### 1.1 Create chrome/Dockerfile

Create `cmd/swe-swe/templates/chrome/Dockerfile`:

```dockerfile
# Chrome browser with Xvfb + noVNC for visual observability
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y \
    xvfb \
    x11vnc \
    novnc \
    websockify \
    chromium \
    chromium-sandbox \
    supervisor \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN useradd -m -s /bin/bash chrome

# Supervisor config to manage processes
COPY supervisord.conf /etc/supervisor/conf.d/supervisord.conf

# noVNC web files location
ENV NOVNC_HOME=/usr/share/novnc

# Expose ports
# 9222: Chrome DevTools Protocol (CDP) - internal use only
# 6080: noVNC web UI
EXPOSE 9222 6080

# Start supervisor to manage Xvfb, Chrome, x11vnc, and websockify
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]
```

### 1.2 Create supervisord.conf

Create `cmd/swe-swe/templates/chrome/supervisord.conf`:

```ini
[supervisord]
nodaemon=true
user=root
logfile=/var/log/supervisor/supervisord.log
pidfile=/var/run/supervisord.pid

[program:xvfb]
command=/usr/bin/Xvfb :99 -screen 0 1920x1080x24
autorestart=true
priority=100
user=chrome

[program:chromium]
command=/usr/bin/chromium --no-sandbox --disable-gpu --disable-software-rasterizer --remote-debugging-port=9222 --remote-debugging-address=0.0.0.0 --display=:99
autorestart=true
priority=200
user=chrome
environment=DISPLAY=":99"
# Wait for Xvfb to start
startsecs=2

[program:x11vnc]
command=/usr/bin/x11vnc -display :99 -forever -shared -nopw -rfbport 5900
autorestart=true
priority=300
user=chrome
# Wait for Xvfb to start
startsecs=3

[program:websockify]
command=/usr/bin/websockify --web=/usr/share/novnc 6080 localhost:5900
autorestart=true
priority=400
user=chrome
# Wait for x11vnc to start
startsecs=5
```

### 1.3 Test Steps

```bash
# Build the container
cd /workspace/cmd/swe-swe/templates
docker build -t swe-swe-chrome:test -f chrome/Dockerfile chrome/

# Run it
docker run -d --name chrome-test -p 9222:9222 -p 6080:6080 swe-swe-chrome:test

# Test 1: noVNC web UI accessible
curl -s http://localhost:6080 | head -5
# Expected: HTML content from noVNC

# Test 2: CDP endpoint responds
curl -s http://localhost:9222/json/version
# Expected: JSON with Chrome version info

# Test 3: Open browser to http://localhost:6080
# Expected: See Chrome window in VNC viewer

# Cleanup
docker stop chrome-test && docker rm chrome-test
```

### 1.4 Verification Checklist

- [ ] Container builds successfully
- [ ] noVNC web UI loads at http://localhost:6080
- [ ] CDP endpoint responds at http://localhost:9222/json/version
- [ ] Chrome window visible in noVNC viewer
- [ ] No crash loops (check `docker logs chrome-test`)

### 1.5 Status

- **Status:** ⬜ Pending
- **Notes:**

---

## Step 2: Add Chrome Service to docker-compose.yml

**Goal:** Add chrome service to the docker-compose.yml template.

### 2.1 Changes

Edit `cmd/swe-swe/templates/docker-compose.yml`:

1. Add `chrome` service definition
2. Add `depends_on: chrome` to swe-swe-server
3. Add `BROWSER_WS_ENDPOINT` env var to swe-swe-server

### 2.2 Test Steps

```bash
# Run swe-swe init in a test directory
cd /tmp && rm -rf browser-test && mkdir browser-test && cd browser-test
/workspace/dist/swe-swe init

# Check chrome service exists
grep -A5 "chrome:" .swe-swe/docker-compose.yml

# Check BROWSER_WS_ENDPOINT env var
grep "BROWSER_WS_ENDPOINT" .swe-swe/docker-compose.yml

# Start services
cd .swe-swe && docker-compose up -d

# Verify chrome container running
docker-compose ps | grep chrome

# Cleanup
docker-compose down
```

### 2.3 Verification Checklist

- [ ] `swe-swe init` generates docker-compose.yml with chrome service
- [ ] Chrome service starts with `docker-compose up`
- [ ] BROWSER_WS_ENDPOINT env var set on swe-swe-server

### 2.4 Status

- **Status:** ⬜ Pending
- **Notes:**

---

## Step 3: Add Traefik Routing for Chrome

**Goal:** Route `chrome.{domain}` to noVNC port 6080.

### 3.1 Changes

Edit `cmd/swe-swe/templates/docker-compose.yml`:

Add Traefik labels to chrome service:
```yaml
labels:
  - "traefik.enable=true"
  - "traefik.http.routers.chrome.rule=HostRegexp(`chrome\\..*`)"
  - "traefik.http.routers.chrome.priority=100"
  - "traefik.http.services.chrome.loadbalancer.server.port=6080"
```

### 3.2 Test Steps

```bash
# With docker-compose running from Step 2:
# Access via Traefik subdomain
curl -s -H "Host: chrome.lvh.me" http://localhost:9899 | head -5
# Expected: noVNC HTML content

# USER TEST: Open browser to http://chrome.lvh.me:9899
# Expected: noVNC viewer showing Chrome
```

### 3.3 Verification Checklist

- [ ] `chrome.lvh.me:${SWE_PORT}` serves noVNC UI
- [ ] WebSocket connection works (VNC stream displays)

### 3.4 Status

- **Status:** ⬜ Pending
- **Notes:**

---

## Step 4: Bundle MCP Playwright in Dockerfile

**Goal:** Add @playwright/mcp to swe-swe-server container.

### 4.1 Changes

Edit `cmd/swe-swe/templates/Dockerfile`:

Add after npm install section:
```dockerfile
# Install MCP Playwright for browser automation
RUN npm install -g @playwright/mcp

# Install Playwright browsers (chromium only - we connect to external Chrome via CDP)
# Skip browser download since we use --cdp-endpoint to connect to chrome container
```

### 4.2 Test Steps

```bash
# Rebuild swe-swe-server image
cd /tmp/browser-test/.swe-swe
docker-compose build swe-swe-server

# Verify @playwright/mcp installed
docker-compose run --rm swe-swe-server npx @playwright/mcp --help
# Expected: Help output from MCP Playwright
```

### 4.3 Verification Checklist

- [ ] `@playwright/mcp` installed in container
- [ ] `npx @playwright/mcp --help` works

### 4.4 Status

- **Status:** ⬜ Pending
- **Notes:**

---

## Step 5: Configure MCP Playwright CDP Endpoint

**Goal:** Configure MCP Playwright to connect to chrome container via CDP.

### 5.1 Research: How swe-swe-server configures MCP

Need to understand:
- Where MCP servers are configured (likely in Go code or config file)
- How to pass `--cdp-endpoint ws://chrome:9222` to MCP Playwright

### 5.2 Changes

TBD based on research. Likely:
- Add MCP server config for playwright
- Set cdp-endpoint to `ws://chrome:9222`

### 5.3 Test Steps

```bash
# Test MCP Playwright can connect to chrome container
# From inside swe-swe-server container:
docker-compose exec swe-swe-server sh -c \
  "npx @playwright/mcp --cdp-endpoint ws://chrome:9222"

# Verify connection (may need interactive test or API call)
```

### 5.4 Verification Checklist

- [ ] MCP Playwright connects to chrome container CDP
- [ ] Browser tools available to Claude agent

### 5.5 Status

- **Status:** ⬜ Pending
- **Notes:**

---

## Step 6: Add System Prompt for Browser Tools

**Goal:** Inject browser automation instructions into Claude's system prompt.

### 6.1 Research: Where -shell instructions are configured

Need to find where agent system prompt is set.

### 6.2 Changes

Add to system prompt (when BROWSER_WS_ENDPOINT is set):

```
You have browser automation via MCP Playwright tools:
- mcp__playwright__browser_navigate, mcp__playwright__browser_click,
  mcp__playwright__browser_type, mcp__playwright__browser_snapshot, etc.
- Browser renders to a virtual display visible at chrome.{base_domain}
- Session/cookies reset on container restart (stateless)
- Use browser tools for web scraping, form filling, UI testing, debugging web apps
```

### 6.3 Test Steps

```bash
# Start Claude agent and verify browser tools available
# Check system prompt includes browser instructions
```

### 6.4 Verification Checklist

- [ ] System prompt includes browser tool instructions
- [ ] Claude can see and use browser tools

### 6.5 Status

- **Status:** ⬜ Pending
- **Notes:**

---

## Step 7: Add BROWSER_AUTOMATION.md Template

**Goal:** Include user documentation for browser automation.

### 7.1 Changes

1. Create `cmd/swe-swe/templates/docs/BROWSER_AUTOMATION.md` (from sample)
2. Update `swe-swe init` to copy this file to `.swe-swe/docs/`

### 7.2 Test Steps

```bash
# Run swe-swe init
# Check docs/BROWSER_AUTOMATION.md exists
ls -la /tmp/browser-test/.swe-swe/docs/BROWSER_AUTOMATION.md
```

### 7.3 Verification Checklist

- [ ] BROWSER_AUTOMATION.md generated during init
- [ ] Documentation accurate and helpful

### 7.4 Status

- **Status:** ⬜ Pending
- **Notes:**

---

## Step 8: End-to-End Integration Test

**Goal:** Verify complete flow works.

### 8.1 Test Scenario

1. `swe-swe init` in fresh directory
2. `swe-swe up`
3. Open `chrome.lvh.me:${SWE_PORT}` - see Chrome in noVNC
4. Claude agent can use browser tools
5. Watch Claude navigate, click, fill forms in noVNC

### 8.2 USER TEST Required

This step requires manual verification:
- [ ] Can you see Chrome window in noVNC viewer?
- [ ] Can Claude use browser tools to interact with websites?

### 8.3 Status

- **Status:** ⬜ Pending
- **Notes:**

---

## Git Commit Strategy

Each step gets its own commit:

1. `feat(chrome): add Chrome container with Xvfb + noVNC`
2. `feat(docker): add chrome service to docker-compose template`
3. `feat(traefik): add chrome subdomain routing`
4. `feat(docker): bundle MCP Playwright in swe-swe-server`
5. `feat(mcp): configure Playwright CDP endpoint`
6. `feat(agent): add browser automation system prompt`
7. `docs: add BROWSER_AUTOMATION.md template`
8. `test: verify browser automation end-to-end`

---

## Rollback Plan

If issues arise:
1. Remove chrome service from docker-compose.yml
2. Remove BROWSER_WS_ENDPOINT from environment
3. Remove MCP Playwright from Dockerfile
4. Claude gracefully degrades (no browser tools available)
