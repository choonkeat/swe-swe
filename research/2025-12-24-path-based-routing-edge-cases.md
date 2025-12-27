# Path-based /vscode Routing: Edge Cases & Revert Decision

**Date**: 2025-12-24 21:00
**Status**: Reverting to subdomain-based approach

## Problem Statement

The migration from subdomain-based routing to path-based `/vscode` routing (commit 737daa7) was intended to support services like ngrok without domain control. However, this approach has introduced multiple edge cases and complexity that make subdomain routing preferable.

## Edge Cases Discovered

1. **Web Component Link Resolution**: The `links` attribute in terminal-ui web component must resolve `/vscode` URLs correctly, but this fails in various contexts
2. **Traefik Path Prefix Complexity**: Properly implementing path prefixes in Traefik v3 requires careful quote escaping and middleware ordering
3. **Code-server Base Path Configuration**: Code-server's `base-path` option has limitations and doesn't fully solve the problem without proxy rewriting
4. **Cross-origin/CORS Issues**: When accessing `/vscode` from the main `/` domain, browser security restrictions may apply
5. **Session Management Conflicts**: UUID-based session routing (commit 5aaabe4) may conflict with path-based routing semantics

## Commits Involved in Migration

### Main Migration
- **737daa7**: Migrate from subdomain → path-based basepath
  - Removes `--domain` CLI flag
  - Removes `.swe-swe/.domain` file handling
  - Updates Traefik routing from HostRegexp to PathPrefix
  - Configures code-server with `/vscode` basepath

- **59ea241**: Documentation update for path-based architecture
  - Updates README URLs to use `/vscode` paths
  - Adds Makefile test targets

- **8a10351**: Fix docker-compose syntax errors from migration
  - Fixes Traefik routing rule quote escaping
  - Removes invalid basicAuth middleware
  - Removes unsupported base-path from code-server config

### Pre-migration (Independent, can be kept)
- **5aaabe4**: UUID-based session routing
- **e6e4f9e**: VSCode password configuration
- **2d2c5fe**: Simplify VSCode password management
- **9747d1e**: VSCode password documentation

### Post-migration (Depend on path-based)
- **021a71a**: Add links attribute to terminal-ui web component

## Revert Strategy

Using Option 1: Clean revert commits (preserves history, keeps VSCode password features)

### Steps
1. Revert commit 8a10351
2. Revert commit 59ea241
3. Revert commit 737daa7
4. Manually restore subdomain routing in:
   - `cmd/swe-swe/templates/docker-compose.yml`
   - `cmd/swe-swe/templates/traefik-dynamic.yml`
   - `cmd/swe-swe/main.go` (restore `--domain` flag)
5. Update `021a71a` (links feature) to work with subdomain URLs
6. Update documentation and tests
7. Full integration test

## Benefits of Reverting to Subdomain

1. **Simplicity**: Subdomain routing is simpler than path-based Traefik configuration
2. **Cleaner URLs**: `code.example.com` vs proxy rewriting `/vscode`
3. **Less Code Coupling**: Services don't need to know about basepaths
4. **Browser Security**: No cross-origin issues with separate subdomains
5. **ngrok Support**: ngrok supports subdomains with `--subdomain` flag anyway

## Why ngrok/Tunnel Support Isn't Actually Better with Path-based

- ngrok can use `--subdomain my-subdomain` for custom subdomains
- Cloudflare Tunnel supports wildcard DNS automatically
- Path-based approach requires proper Traefik configuration anyway
- The "simplicity" argument doesn't hold once Traefik complexity is factored in

## Keeping and Using VSCode Password Features

The VSCode password configuration from commits 5aaabe4, e6e4f9e, 2d2c5fe, 9747d1e can all be kept. These are independent of the routing approach and provide real value.
