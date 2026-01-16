# Browser Automation

This document describes the browser automation capabilities in swe-swe, enabling AI assistants to interact with web pages through a headless Chromium browser.

## Overview

swe-swe includes a dedicated Chrome container that provides:

- **Headless Chromium** browser for automated web interactions
- **Chrome DevTools Protocol (CDP)** access for programmatic control
- **VNC/noVNC** for visual observation of browser activity
- **MCP Playwright** integration for AI-driven browser automation

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        swe-swe Environment                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────┐      ┌──────────────┐      ┌──────────────┐  │
│  │  swe-swe     │      │    chrome    │      │   traefik    │  │
│  │  container   │─────▶│  container   │◀─────│    proxy     │  │
│  │              │ CDP  │              │ HTTP │              │  │
│  │ MCP Playwright      │  Chromium    │      │              │  │
│  └──────────────┘      │  + VNC       │      └──────────────┘  │
│                        └──────────────┘                         │
│                              │                                   │
│                              ▼                                   │
│                     localhost:1977/chrome                       │
│                     (VNC web viewer)                            │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Chrome Container

### Services Running

The Chrome container runs multiple services via supervisord:

| Service | Port | Purpose |
|---------|------|---------|
| Xvfb | :99 | Virtual X11 display (1920x1080) |
| Chromium | 9222 (localhost) | Browser with remote debugging |
| nginx | 9223 | CDP reverse proxy (fixes Host header) |
| x11vnc | 5900 | VNC server for X11 |
| websockify | 6080 | noVNC web interface |

### Ports

| External | Internal | Description |
|----------|----------|-------------|
| `localhost:1977/chrome` | 6080 | noVNC web interface (via Traefik path-based routing) |
| - | 9223 | CDP WebSocket (internal network only) |

### Environment Variables

The swe-swe container is pre-configured with:

```yaml
BROWSER_WS_ENDPOINT=ws://chrome:9223
```

This allows MCP Playwright to automatically connect to the Chrome container.

## Using Browser Automation

### Visual Observation

Watch the browser in real-time at: **http://localhost:1977/chrome** (or **https://localhost:1977/chrome** if using `--ssl=selfsign`)

The noVNC interface shows exactly what the AI assistant sees when automating the browser.

### MCP Playwright Integration

The swe-swe container includes `@playwright/mcp` which provides browser automation tools to AI assistants. When using Claude Code or similar tools:

```javascript
// Example: Navigate to a URL
mcp__playwright__browser_navigate({ url: "https://example.com" })

// Example: Take a screenshot
mcp__playwright__browser_take_screenshot({ filename: "screenshot.png" })

// Example: Click an element
mcp__playwright__browser_click({ element: "Login button", ref: "e5" })

// Example: Get page snapshot (accessibility tree)
mcp__playwright__browser_snapshot()
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

The Chrome container supports enterprise SSL certificates for accessing internal sites behind corporate proxies.

### How It Works

1. During `swe-swe init`, certificates are detected from:
   - `NODE_EXTRA_CA_CERTS`
   - `SSL_CERT_FILE`
   - `NODE_EXTRA_CA_CERTS_BUNDLE`

2. Certificates are copied to `$HOME/.swe-swe/projects/{path}/certs/`

3. At container startup, the entrypoint script:
   - Installs certs to system CA store (for curl, wget)
   - Installs certs to NSS database (for Chromium)

### NSS Database

Chromium uses the NSS (Network Security Services) database instead of the system CA store. The entrypoint script handles this:

```bash
# Create NSS database
certutil -d sql:/home/chrome/.pki/nssdb -N --empty-password

# Add certificate
certutil -d sql:/home/chrome/.pki/nssdb -A -t "C,," -n "enterprise-ca" -i /swe-swe/certs/cert.pem
```

### Verifying Certificate Installation

```bash
# List certificates in NSS database
docker exec <chrome-container> certutil -d sql:/home/chrome/.pki/nssdb -L

# Expected output:
# Certificate Nickname                Trust Attributes
# enterprise-ca                       C,,
```

## Configuration

### Docker Compose

The Chrome service is defined in `docker-compose.yml`:

```yaml
chrome:
  build:
    context: .
    dockerfile: chrome/Dockerfile
  labels:
    - "traefik.enable=true"
    - "traefik.http.routers.chrome.rule=PathPrefix(`/chrome`)"
    - "traefik.http.routers.chrome.middlewares=strip-chrome"
    - "traefik.http.middlewares.strip-chrome.stripprefix.prefixes=/chrome"
    - "traefik.http.services.chrome.loadbalancer.server.port=6080"
  volumes:
    - ./certs:/swe-swe/certs:ro
  networks:
    - swe-network
  deploy:
    resources:
      limits:
        cpus: '1'
        memory: 1G
```

### Resource Limits

Default resource limits for the Chrome container:
- CPU: 1 core (0.5 reserved)
- Memory: 1GB (512MB reserved)

Adjust in `docker-compose.yml` if needed for resource-intensive automation tasks.

## Troubleshooting

### SSL Certificate Errors

**Error**: `net::ERR_CERT_AUTHORITY_INVALID`

**Solution**: Verify certificates are installed in NSS database:
```bash
docker exec <chrome-container> certutil -d sql:/home/chrome/.pki/nssdb -L
```

If empty, check:
1. Certificate files exist in `certs/` directory
2. Files have `.pem` extension
3. Rebuild container: `swe-swe build chrome`

### VNC Not Loading

**Error**: Directory listing instead of VNC interface

**Solution**: The Chrome container should have `index.html` symlinked to `vnc.html`. Rebuild:
```bash
swe-swe build chrome
swe-swe down chrome && swe-swe up chrome -- -d
```

### CDP Connection Failed

**Error**: MCP Playwright cannot connect to browser

**Solution**:
1. Verify Chrome container is running: `docker ps | grep chrome`
2. Check `BROWSER_WS_ENDPOINT` is set: should be `ws://chrome:9223`
3. Verify nginx CDP proxy is running inside container

### Browser Crashes

**Error**: Chromium crashes or becomes unresponsive

**Solution**: Check resource limits. Increase memory if needed:
```yaml
deploy:
  resources:
    limits:
      memory: 2G
```

## Files

| File | Purpose |
|------|---------|
| `chrome/Dockerfile` | Chrome container image definition |
| `chrome/supervisord.conf` | Process manager configuration |
| `chrome/entrypoint.sh` | Certificate installation script |
| `chrome/nginx-cdp.conf` | CDP reverse proxy (Host header fix) |
