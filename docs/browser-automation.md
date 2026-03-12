# Browser Automation

This document describes the browser automation capabilities in swe-swe, enabling AI assistants to interact with web pages through per-session Chromium instances.

## Overview

Each swe-swe session runs its own isolated browser stack:

- **Chromium** browser for automated web interactions
- **Chrome DevTools Protocol (CDP)** access on a per-session port
- **VNC** via noVNC for visual observation of browser activity
- **MCP Playwright** integration for AI-driven browser automation

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     swe-swe Container                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Per Session:                                                    │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Xvfb (:N)  →  Chromium (CDP port)  →  x11vnc  →  noVNC │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  Port Ranges:                                                    │
│  ├── CDP:  6000-6019 (preview port + 3000)                      │
│  ├── VNC:  7000-7019 (preview port + 4000)                      │
│  └── External: 26000-26019, 27000-27019 (via Traefik)           │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Per-Session Browser

Browser processes start on-demand when the first Playwright MCP tool is used (not at session creation). There is a ~2-3 second one-time delay on the first tool call. Each session gets its own browser processes:

| Process | Port | Purpose |
|---------|------|---------|
| Xvfb | Unix socket (:N) | Virtual X11 display (no TCP) |
| Chromium | CDPPort (6000+) | Browser with remote debugging |
| x11vnc | VNCPort+100 (internal) | Raw VNC server |
| noVNC/websockify | VNCPort (7000+) | WebSocket bridge for browser viewing |

Display numbering derives from preview port: port 3000 → `:1`, port 3001 → `:2`, etc.

### Environment Variables

Each session receives:

```bash
BROWSER_CDP_PORT=6000   # Per-session CDP port
BROWSER_VNC_PORT=7000   # Per-session VNC port
```

MCP Playwright connects via `--cdp-endpoint http://localhost:$BROWSER_CDP_PORT`.

## Using Browser Automation

### Visual Observation

Watch the browser in the **Agent View** tab in the swe-swe UI. Each session shows its own independent browser view via noVNC. The Agent View will show a connection waiting screen until the first Playwright tool is used, which triggers browser startup.

### MCP Playwright Integration

The swe-swe container includes `@playwright/mcp` which provides browser automation tools to AI assistants:

```javascript
// Navigate to a URL
mcp__swe-swe-playwright__browser_navigate({ url: "https://example.com" })

// Take a screenshot
mcp__swe-swe-playwright__browser_take_screenshot({ filename: "screenshot.png" })

// Click an element
mcp__swe-swe-playwright__browser_click({ element: "Login button", ref: "e5" })

// Get page snapshot (accessibility tree)
mcp__swe-swe-playwright__browser_snapshot()
```

### Available MCP Tools

| Tool | Description |
|------|-------------|
| `browser_navigate` | Navigate to a URL |
| `browser_snapshot` | Capture accessibility tree (better than screenshot) |
| `browser_take_screenshot` | Take a visual screenshot |
| `browser_click` | Click on an element |
| `browser_type` | Type text into an element |
| `browser_fill_form` | Fill multiple form fields |
| `browser_select_option` | Select dropdown option |
| `browser_hover` | Hover over an element |
| `browser_drag` | Drag and drop |
| `browser_press_key` | Press keyboard key |
| `browser_evaluate` | Execute JavaScript |
| `browser_console_messages` | Get console messages |
| `browser_network_requests` | Get network requests |
| `browser_tabs` | List/create/close tabs |
| `browser_close` | Close the browser page |

## Enterprise SSL Certificates

Chromium in the swe-swe container uses certificates from the system CA store, which are installed at container startup by the entrypoint script.

### How It Works

1. During `swe-swe init`, certificates are detected from:
   - `NODE_EXTRA_CA_CERTS`
   - `SSL_CERT_FILE`
   - `NODE_EXTRA_CA_CERTS_BUNDLE`

2. Certificates are copied to `$HOME/.swe-swe/projects/{path}/certs/`

3. At container startup, the entrypoint script installs certs to the system CA store

## Troubleshooting

### CDP Connection Failed

**Error**: MCP Playwright cannot connect to browser

**Solution**:
1. Check browser processes are running: `ps aux | grep -E 'Xvfb|chromium'`
2. Verify CDP port is accessible: `curl -s http://localhost:$BROWSER_CDP_PORT/json/version`
3. Check server logs for browser startup errors

### Browser Crashes

**Error**: Chromium crashes or becomes unresponsive

**Solution**: Check server logs for crash details. The session will need to be ended and recreated.

## Files

| File | Purpose |
|------|---------|
| `cmd/swe-swe/templates/host/Dockerfile` | Main container with browser packages |
| `cmd/swe-swe/templates/host/entrypoint.sh` | MCP configuration with per-session CDP |
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | Browser process management |
