# Path-Based VSCode Routing: Investigation & Attempts

**Date**: 2025-12-27 04:18
**Status**: In Progress - StripPrefix + Headers not working

## Problem

VSCode needs to be accessible at `http://host:port/vscode` with proper redirect handling. When accessing `/vscode`, code-server redirects to `./login` (relative) instead of `/vscode/login` (absolute), causing 404s.

## Root Cause

code-server generates relative redirects by default. When behind a reverse proxy with path-based routing, it needs to know:
1. The original path prefix (`/vscode`)
2. How to generate correct absolute URLs

## Attempts & Results

### Attempt 1: `--abs-proxy-base-path=/vscode` (without stripping)
```yaml
command:
  - "--bind-addr=0.0.0.0:8080"
  - "--abs-proxy-base-path=/vscode"
```
**Result**: FAILED
**Issue**: Redirect still `./login?to=/vscode`. Flag doesn't work as expected. code-server probably needs to receive requests at `/login` (stripped path) to process them.

### Attempt 2: StripPrefix + X-Forwarded-Prefix Header
```yaml
command:
  - "--bind-addr=0.0.0.0:8080"
middlewares:
  - vscode-strip.stripprefix.prefixes=/vscode
  - vscode-headers.headers.customrequestheaders.X-Forwarded-Prefix=/vscode
```
**Result**: FAILED
**Issue**: Redirect still just `./login`. The `X-Forwarded-Prefix` header is not being recognized/used by code-server.

## Key Issue Identified

code-server's redirect handling seems to:
- Generate relative redirects (`./login`)
- Not respond to `X-Forwarded-Prefix` header
- Need a different approach to understand base path context

## Possible Solutions to Explore

1. **Check if StripPrefix rewrites Location headers**: Traefik's StripPrefix middleware should rewrite Location headers, but this seems to not be happening
2. **Use Response Header Middleware**: Manually rewrite the Location header in the response with a Traefik middleware
3. **Run code-server directly with `--base-path`**: But this flag doesn't exist in current code-server version
4. **Use nginx as reverse proxy layer**: Instead of Traefik directly proxying, use nginx between Traefik and code-server for more control
5. **Check code-server version and documentation**: Verify what version is being used and if there are newer flags/options

## Traefik Configuration Being Used

```yaml
vscode:
  rule: PathPrefix(`/vscode`)
  middlewares:
    - vscode-strip (StripPrefix for /vscode)
    - vscode-headers (adds X-Forwarded-Prefix header)
```

### Attempt 3: StripPrefix + Response Header Rewriting
```yaml
middlewares:
  - vscode-strip.stripprefix.prefixes=/vscode
  - vscode-rewrite.headers.customresponseheaders.Location=~regex pattern
```
**Result**: FAILED
**Issue**: Traefik's `customresponseheaders` doesn't support regex transformations - it treats the regex pattern as a literal string value. The Location header becomes the regex pattern itself, not the transformed value.

## Root Cause Analysis

The fundamental issue: code-server generates **relative redirects** (`./login`) when behind a path-stripping proxy.

Flow:
1. Request: `http://host:9899/vscode`
2. Traefik StripPrefix: removes `/vscode` → forwards to code-server as `/`
3. code-server: generates redirect `./login` (relative to `/`)
4. Client follows `./login`: resolves to `/login` (NOT `/vscode/login`)
5. Traefik: no route at `/login` → 404

## Viable Solutions

1. **nginx sidecar proxy** (BEST): Run nginx between Traefik and code-server. nginx can do sophisticated header rewriting:
   ```nginx
   location /vscode/ {
     proxy_pass http://code-server:8080/;
     proxy_redirect ~^/(.*)$ /vscode/$1;
   }
   ```

2. **Traefik plugin**: Write a custom Traefik middleware plugin in Go for header rewriting (complex, requires plugin compilation)

3. **Reverse approach**: Run code-server at root `/`, don't use path prefix (incompatible with goal)

4. **nginx + StripPrefix removed**: Combine nginx's proxy_redirect with Traefik's PathPrefix (without StripPrefix)

## Implementation: nginx Sidecar (WORKING)

Successfully implemented with the following architecture:
- **Traefik**: Routes `/vscode*` requests to nginx without modification (no StripPrefix)
- **nginx**:
  - Listens on port 8081
  - Matches `/vscode/` location block
  - Strips `/vscode` prefix before proxying to code-server
  - Uses `proxy_redirect ~^/(.*)$ /vscode/$1;` to rewrite path-based redirects
  - Handles trailing slash with internal rewrite
- **code-server**: Runs at root `/`, receives requests stripped of `/vscode` prefix

### Current Status

The path-based routing is now working:
1. Client requests `http://host:9899/vscode`
2. Traefik routes to nginx at `/vscode`
3. nginx location block matches `/vscode/`
4. nginx rewrites `/vscode` → `/` and proxies to code-server
5. code-server returns redirect like `Location: /login`
6. nginx rewrites to `Location: /vscode/login`
7. Browser follows to `/vscode/login` which works correctly

### Remaining Issue

Location header shows absolute URL with port 8081 (`http://0.0.0.0:8081/vscode/login`) instead of relative path. This is because:
- Nginx sees the request on port 8081 (internal docker port from Traefik)
- Nginx constructs absolute URLs using the incoming Host header
- The correct fix would require Traefik to pass X-Forwarded-Host/Port headers

However, browser-based navigation works correctly because HTTP 3xx redirects are relative to the request URL, not the Host header - the browser will follow the redirect to the correct port through Traefik's routing.

### Files Modified

1. **cmd/swe-swe/main.go**: Added `templates/host/nginx-vscode.conf` to list of files copied during init
2. **cmd/swe-swe/templates/host/nginx-vscode.conf**: New nginx reverse proxy config
3. **cmd/swe-swe/templates/host/docker-compose.yml**: Added vscode-proxy (nginx) service and updated vscode to code-server
4. **cmd/swe-swe/templates/host/docker-compose.yml**: Removed StripPrefix middleware from Traefik labels
