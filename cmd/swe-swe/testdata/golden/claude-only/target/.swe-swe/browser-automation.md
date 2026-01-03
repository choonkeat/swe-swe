# Browser Automation in swe-swe

## Overview
Browser automation uses MCP Playwright connected to a Chrome sidecar container. When the user asks to "use browser tool" or similar, use the `mcp__playwright__*` tools.

## Architecture
```
claude-code container  -->  chrome container
   (MCP Playwright)         (Chromium + nginx)
        |                        |
        +--- http://chrome:9223 -+
                  (CDP)
```

- Chrome runs in a separate container with Xvfb (virtual display)
- User can watch via VNC at http://localhost:1977/chrome/
- CDP (Chrome DevTools Protocol) is exposed on port 9223 via nginx reverse proxy

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
cat .mcp.json
```
Should show playwright config with `--cdp-endpoint http://chrome:9223` in args

### 2. Is Chrome container running?
From host: VNC should work at http://localhost:1977/chrome/

### 3. Is CDP port accessible?
From inside claude-code container:
```bash
curl -s http://chrome:9223/json/version
```
Should return JSON with Chrome version info.

### 4. Is nginx CDP proxy running?
From host:
```bash
docker exec <chrome-container> netstat -tlnp | grep 9223
```
Should show `0.0.0.0:9223` (not 127.0.0.1).

### Common Issues
- **Chrome binds to localhost only**: Chromium ignores `--remote-debugging-address=0.0.0.0`. We use nginx on port 9223 to proxy to localhost:9222.
- **Container needs rebuild**: After config changes, run `swe-swe stop && swe-swe build && swe-swe up`

## Configuration Files
- `.mcp.json` - MCP Playwright config (in project root)
- `cmd/swe-swe/templates/host/chrome/Dockerfile` - Chrome container image
- `cmd/swe-swe/templates/host/chrome/supervisord.conf` - Process manager (xvfb, chromium, vnc, nginx)
- `cmd/swe-swe/templates/host/chrome/nginx-cdp.conf` - CDP reverse proxy config
- `cmd/swe-swe/templates/host/chrome/entrypoint.sh` - Enterprise certificate installation
- `cmd/swe-swe/templates/host/docker-compose.yml` - Container orchestration
