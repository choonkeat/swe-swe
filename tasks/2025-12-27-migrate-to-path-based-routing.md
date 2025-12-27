# Migrate from Subdomain to Path-Based Routing

**Date**: 2025-12-27
**Status**: Planning
**Goal**: Replace all subdomain-based routing with path-based routing to enable ngrok/cloudflared support

## Architecture

```
Traefik (port 9899)
├── PathPrefix(/vscode) → priority 100 → nginx-vscode → code-server
├── PathPrefix(/chrome) → priority 100 → chrome (VNC/noVNC at /chrome)
└── PathPrefix(/)       → priority 10  → swe-swe (catch-all)
    ├── / - terminal UI
    ├── /session - WebSocket sessions
    └── /api/* - APIs
```

## Implementation Plan

### Step 1: Configure noVNC base path
- [ ] Update `chrome/supervisord.conf`: Add `--base_path=/chrome` to websockify command
- [ ] Test: Access `http://lvh.me:9899/chrome` and verify VNC viewer loads
- [ ] Verify: WebSocket connections work with `/chrome` path prefix
- [ ] **Commit**: "refactor(chrome): configure websockify to serve from /chrome base path"

### Step 2: Update Traefik routing rules
- [ ] Modify `docker-compose.yml`:
  - Remove all `HostRegexp` rules from swe-swe, chrome, vscode
  - Update swe-swe rule to `PathPrefix(/)` with priority 10
  - Update chrome rule to `PathPrefix(/chrome)` with priority 100
  - Keep vscode as `PathPrefix(/vscode)` with priority 100
- [ ] Test: `curl http://lvh.me:9899/` (swe-swe), `/chrome` (chrome), `/vscode` (vscode)
- [ ] Verify: All three services respond correctly with proper priorities
- [ ] **Commit**: "refactor(traefik): migrate from subdomain to path-based routing"

### Step 3: Update terminal UI links
- [ ] Modify `cmd/swe-swe-server/static/terminal-ui.js`:
  - Update chrome link from `buildUrl('chrome')` to `${baseUrl}/chrome`
  - Ensure vscode link is `${baseUrl}/vscode` (already done)
- [ ] Test: Click links in terminal UI, verify they navigate correctly
- [ ] **Commit**: "refactor(ui): update service links to use path-based URLs"

### Step 4: Remove domain-related code
- [ ] Search `cmd/swe-swe/main.go` for:
  - `--domain` CLI flag
  - `.swe-swe/.domain` file handling
  - Domain-related help text and variables
- [ ] Delete any domain-related code found
- [ ] Test: `./dist/swe-swe.darwin-arm64 init` completes without domain logic
- [ ] **Commit**: "refactor(cli): remove subdomain/domain-related code"

### Step 5: Update documentation
- [ ] Update `README.md`:
  - Change VSCode URL from subdomain to `http://lvh.me:9899/vscode`
  - Change Chrome VNC URL from subdomain to `http://lvh.me:9899/chrome`
  - Keep swe-swe as `http://lvh.me:9899`
  - Remove `traefik.lvh.me` references or update to path-based
  - Add note about ngrok/cloudflared compatibility
- [ ] Update routing architecture diagram if present
- [ ] Test: README is accurate and clear
- [ ] **Commit**: "docs(README): update for path-based routing architecture"

### Step 6: Full integration test
- [ ] Build and deploy: `make build && ./dist/swe-swe.darwin-arm64 down && ./dist/swe-swe.darwin-arm64 init && ./dist/swe-swe.darwin-arm64 up`
- [ ] Test each service:
  - swe-swe terminal: `http://lvh.me:9899` works
  - VSCode: `http://lvh.me:9899/vscode` loads correctly
  - Chrome VNC: `http://lvh.me:9899/chrome` loads noVNC viewer
  - Links in terminal UI navigate correctly
- [ ] Test priority (request `/` should go to swe-swe, not chrome/vscode)
- [ ] Verify no console errors or 404s
- [ ] **Commit**: "test(integration): verify path-based routing works end-to-end"

## Current Status

- [x] Research complete (chrome setup identified)
- [ ] Step 1: Configure noVNC base path
- [ ] Step 2: Update Traefik routing
- [ ] Step 3: Update terminal UI
- [ ] Step 4: Remove domain code
- [ ] Step 5: Update docs
- [ ] Step 6: Full integration test

## Notes

- websockify supports `--base_path` flag for noVNC base path
- No nginx sidecar needed for chrome (unlike vscode which needs redirect rewriting)
- Keep nginx-cdp.conf as-is (it's for Chrome DevTools Protocol, internal proxy)
- All other paths fall through to swe-swe, no special handling needed
