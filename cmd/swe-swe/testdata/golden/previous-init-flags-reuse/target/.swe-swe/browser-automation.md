# Browser Automation in swe-swe

## Overview
Browser automation uses MCP Playwright connected to a Chrome sidecar container. When the user asks to "use browser tool" or similar, use the `mcp__playwright__*` tools.

## Architecture
```
claude-code container  -->  chrome container
   (MCP Playwright)         (Chromium + socat)
        |                        |
        +--- http://chrome:9223 -+
                  (CDP)
```

- Chrome runs in a separate container with Xvfb (virtual display)
- User can watch via VNC at http://chrome.lvh.me:9899/vnc_auto.html
- CDP (Chrome DevTools Protocol) is exposed on port 9223 via socat forwarder

## Available Tools
- `mcp__playwright__browser_navigate` - Navigate to URL
- `mcp__playwright__browser_snapshot` - Get page accessibility snapshot
- `mcp__playwright__browser_click` - Click element
- `mcp__playwright__browser_type` - Type text
- `mcp__playwright__browser_take_screenshot` - Capture screenshot
- `mcp__playwright__browser_console_messages` - Get console logs
- `mcp__playwright__browser_network_requests` - Get network activity
- `mcp__playwright__browser_close` - Close browser
- And more: `browser_press_key`, `browser_hover`, `browser_wait_for`, `browser_tabs`, `browser_resize`, `browser_evaluate`, `browser_run_code`, `browser_handle_dialog`

## Troubleshooting

If browser tools are unavailable, check in order:

### 1. Is MCP config present?
```bash
cat .claude/mcp.json
```
Should show playwright config with `PLAYWRIGHT_CDP_ENDPOINT: http://chrome:9223`

### 2. Is Chrome container running?
From host: VNC should work at http://chrome.lvh.me:9899/vnc_auto.html

### 3. Is CDP port accessible?
From inside claude-code container:
```bash
curl -s http://chrome:9223/json/version
```
Should return JSON with Chrome version info.

### 4. Is socat forwarder running?
From host:
```bash
docker exec <chrome-container> netstat -tlnp | grep 9223
```
Should show `0.0.0.0:9223` (not 127.0.0.1).

### Common Issues
- **Chrome binds to localhost only**: Chromium ignores `--remote-debugging-address=0.0.0.0`. We use socat on port 9223 to forward to localhost:9222.
- **Container needs rebuild**: After config changes, run `swe-swe stop && swe-swe build && swe-swe up`

## Configuration Files
- `.claude/mcp.json` - MCP Playwright config (in project root)
- `cmd/swe-swe/templates/chrome/Dockerfile` - Chrome container image
- `cmd/swe-swe/templates/chrome/supervisord.conf` - Process manager (xvfb, chromium, vnc, socat)
- `cmd/swe-swe/templates/docker-compose.yml` - Container orchestration
