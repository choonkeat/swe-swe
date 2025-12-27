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
- [x] Implemented socat workaround (partial fix - didn't fix Host header)
- [x] Added `.claude/browser-automation.md` to templates for troubleshooting guide
- [x] Updated browser prompt in main.go to reference the guide
- [x] Verified template files have the fix (2025-12-26):
  - `supervisord.conf` has `[program:cdp-forwarder]` with socat command
  - `Dockerfile` has `socat` package installed
- [x] **Discovered Host header issue** (2025-12-26):
  - socat forwards port but Chrome rejects `Host: chrome` header
  - Chrome only accepts localhost or IP address in Host header
  - `curl -H "Host: localhost" http://chrome:9223/json/version` works
  - `curl http://172.22.0.4:9223/json/version` works (IP address)
- [x] **Replaced socat with nginx reverse proxy** (2025-12-26):
  - nginx rewrites Host header to "localhost" before proxying to Chrome
  - Created `nginx-cdp.conf` with proxy_set_header Host localhost
  - Updated Dockerfile: replaced socat with nginx
  - Updated supervisord.conf: replaced cdp-forwarder (socat) with cdp-proxy (nginx)
- [x] **Added enterprise SSL certificate support** (2025-12-26):
  - Chrome needs enterprise certs to access HTTPS sites through corporate proxies
  - Created `chrome/entrypoint.sh` - installs certs from `/swe-swe/certs` to system trust store
  - Updated Dockerfile: added ca-certificates, entrypoint.sh, ENTRYPOINT directive
  - Updated docker-compose.yml: mount `./certs:/swe-swe/certs:ro` into chrome container
- [x] **User rebuilt chrome container** (2025-12-26)
- [x] CDP endpoint verified working via nginx proxy:
  - `curl http://chrome:9223/json/version` returns Chrome version JSON
  - Port 9222 correctly blocked from external access
- [x] Fixed MCP package name (`@playwright/mcp` not `@anthropic-ai/mcp-playwright`)
- [x] Removed "best-effort" silent failure patterns from Dockerfile:
  - All installs now fail fast (no `|| true`, `|| echo`, `2>/dev/null`)
  - Removed optional codex/gemini-cli (future: CLI flags)
  - Cleaned up stale comments
- [ ] **NEXT: Restart Claude Code session to activate MCP Playwright tools**
- [ ] Verify MCP Playwright tools work after session restart
- [x] Commit changes (e9d4ed8)

## Session Log (2025-12-26)
- Confirmed current chrome container only has `127.0.0.1:9222` (no 9223)
- Template files have correct fix, but container needs rebuild
- Container name: `users-choonkeatchew-git-choonkeat-swe-swe-a45e1b96-chrome-1`
- **UPDATE**: socat IS working on 0.0.0.0:9223, but CDP rejects non-localhost Host headers
- `curl http://chrome:9223/json/version` returns: "Host header is specified and is not an IP address or localhost"
- **FIX ATTEMPT 1**: Added `--remote-allow-origins=*` to Chrome startup flags (did not help with Host header)
- **FIX ATTEMPT 2**: Replaced socat with nginx reverse proxy
  - nginx rewrites Host header to "localhost" before proxying
  - Files changed:
    - `chrome/nginx-cdp.conf` - new file with nginx config
    - `chrome/Dockerfile` - replaced socat with nginx
    - `chrome/supervisord.conf` - replaced cdp-forwarder with cdp-proxy (nginx)
- **ENHANCEMENT**: Added enterprise SSL certificate support to chrome container
  - User reported Chrome cannot visit HTTPS sites (enterprise proxy/VPN)
  - Same pattern as swe-swe container: mount certs, install via entrypoint
  - Files changed:
    - `chrome/entrypoint.sh` - new file, installs certs before supervisord
    - `chrome/Dockerfile` - added ca-certificates, entrypoint.sh
    - `docker-compose.yml` - mount ./certs:/swe-swe/certs:ro into chrome

- **VERIFICATION PASSED** (session resumed):
  - Chrome container rebuilt with nginx proxy
  - `curl http://chrome:9223/json/version` returns valid JSON
  - MCP tools not available in current session (session predates config)
- **CRITICAL FIX: Wrong MCP package** (2025-12-26):
  - `@anthropic-ai/mcp-playwright` does NOT exist (npm 404)
  - Correct package: `@playwright/mcp` (official Microsoft/Playwright)
  - Config format: uses CLI args `--cdp-endpoint` not env vars
  - Files updated:
    - `cmd/swe-swe/templates/.claude/mcp.json`
    - `cmd/swe-swe/templates/Dockerfile`
    - `/workspace/.claude/mcp.json`

## Next Steps (for user on host)
```bash
# Rebuild and restart chrome container
docker-compose build chrome && docker-compose up -d chrome
```

Then verify nginx is proxying correctly:
```bash
# Check nginx is listening on port 9223
docker exec <chrome-container> netstat -tlnp | grep 9223
# Expected: 0.0.0.0:9223 LISTEN

# Test CDP endpoint from inside claude-code container
curl -s http://chrome:9223/json/version
# Should return Chrome version JSON (no Host header error)
```

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
