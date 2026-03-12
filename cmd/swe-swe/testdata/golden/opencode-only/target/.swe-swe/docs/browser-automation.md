# Browser Automation in swe-swe

## Overview
Browser automation uses MCP Playwright connected to a per-session Chromium instance. When the user asks to "use browser tool" or similar, use the `mcp__swe-swe-playwright__*` tools.

## Architecture
```
swe-swe container (per session)
   ├── Xvfb (virtual X11 display, unique per session)
   ├── Chromium (CDP on $BROWSER_CDP_PORT)
   ├── x11vnc (raw VNC, internal port)
   └── noVNC/websockify (WebSocket bridge, $BROWSER_VNC_PORT)
```

- Each session gets its own isolated browser instance (Xvfb + Chromium + VNC)
- Browser processes start on-demand: they are launched on the first Playwright MCP tool call, not at session creation. There is a ~2-3 second one-time delay on the first tool call.
- User can watch and interact via the "Agent View" tab in the UI (appears after first Playwright tool use)
- CDP (Chrome DevTools Protocol) is available on a per-session port via `$BROWSER_CDP_PORT`
- noVNC provides a web-based VNC client for interactive browser access

## Available Tools
- `mcp__swe-swe-playwright__browser_navigate` - Navigate to URL
- `mcp__swe-swe-playwright__browser_snapshot` - Get page accessibility snapshot
- `mcp__swe-swe-playwright__browser_click` - Click element
- `mcp__swe-swe-playwright__browser_type` - Type text
- `mcp__swe-swe-playwright__browser_take_screenshot` - Capture screenshot
- `mcp__swe-swe-playwright__browser_console_messages` - Get console logs
- `mcp__swe-swe-playwright__browser_network_requests` - Get network activity
- `mcp__swe-swe-playwright__browser_close` - Close browser
- And more: `browser_press_key`, `browser_hover`, `browser_wait_for`, `browser_tabs`, `browser_resize`, `browser_evaluate`, `browser_run_code`, `browser_handle_dialog`

## Troubleshooting

If browser tools are unavailable, check in order:

### 1. Is MCP config present?
```bash
claude mcp list
```
Should show `swe-swe-playwright` with `--cdp-endpoint http://localhost:$BROWSER_CDP_PORT` in args

### 2. Is CDP port accessible?
```bash
curl -s http://localhost:$BROWSER_CDP_PORT/json/version
```
Should return JSON with Chrome version info.

### 3. Are browser processes running?
```bash
ps aux | grep -E 'Xvfb|chromium|x11vnc|websockify'
```
Should show processes for your session's display number.

### Common Issues
- **Browser not started**: Browser processes start on-demand when the first Playwright MCP tool is used. If browser tools fail, try using a Playwright tool first (e.g., `browser_navigate`). Check server logs for startup errors.
- **Container needs rebuild**: After config changes, run `swe-swe stop && swe-swe build && swe-swe up`
