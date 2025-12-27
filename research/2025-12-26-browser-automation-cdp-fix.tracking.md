# Browser Automation CDP Fix

## Problem
Chrome's `--remote-debugging-address=0.0.0.0` flag is ignored by Chromium in the Docker container. Chrome binds to `127.0.0.1:9222` instead of `0.0.0.0:9222`, making CDP inaccessible from other containers (like the claude-code container).

## Diagnosis Steps
1. MCP Playwright tools unavailable in claude-code session
2. `curl -v http://chrome:9222/json/version` returns "Connection refused"
3. DNS resolves correctly (chrome -> 172.22.0.2)
4. VNC works from host (http://chrome.lvh.me:9899/vnc_auto.html) - container is running
5. `netstat -tlnp | grep 9222` inside chrome container shows `127.0.0.1:9222` not `0.0.0.0:9222`

## Root Cause
Modern Chromium versions ignore `--remote-debugging-address` for security reasons and always bind to localhost.

## Solution
Use `socat` to forward port 9223 (bound to 0.0.0.0) to localhost:9222.

### Files Changed
1. `cmd/swe-swe/templates/chrome/Dockerfile` - add `socat` package
2. `cmd/swe-swe/templates/chrome/supervisord.conf` - add `cdp-forwarder` program
3. `cmd/swe-swe/templates/.claude/mcp.json` - use port 9223
4. `cmd/swe-swe/templates/docker-compose.yml` - update BROWSER_WS_ENDPOINT to port 9223
5. `cmd/swe-swe/templates/.claude/browser-automation.md` - troubleshooting guide for browser tools
6. `cmd/swe-swe-server/main.go` - updated browser prompt to reference troubleshooting guide

### supervisord.conf addition
```ini
[program:cdp-forwarder]
command=/usr/bin/socat TCP-LISTEN:9223,fork,reuseaddr,bind=0.0.0.0 TCP:127.0.0.1:9222
autorestart=true
priority=250
stdout_logfile=/var/log/supervisor/cdp-forwarder.log
stderr_logfile=/var/log/supervisor/cdp-forwarder.err
startsecs=5
```

## Status
- [x] Identified problem (Chrome binds to localhost only)
- [x] Implemented socat workaround
- [x] Added `.claude/browser-automation.md` to templates for troubleshooting guide
- [x] Updated browser prompt in main.go to reference the guide
- [ ] User to rebuild and test: `swe-swe stop && swe-swe build && swe-swe up`
- [ ] Verify `netstat -tlnp | grep 9223` shows `0.0.0.0:9223`
- [ ] Verify MCP Playwright tools work in claude-code session
- [ ] Commit changes

## Verification Commands
From host:
```bash
# Check socat is listening on 0.0.0.0:9223
docker exec <chrome-container> netstat -tlnp | grep 9223

# Test CDP endpoint
docker exec <chrome-container> curl -s http://localhost:9223/json/version
```

From inside claude-code container:
```bash
curl -s http://chrome:9223/json/version
```
