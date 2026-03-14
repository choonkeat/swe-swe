# ADR-0037: `--dockerfile-only` Single-Container Mode

**Status**: Accepted
**Date**: 2026-03-14

## Context

Platforms like Fly.io, Railway, and Render support only single-container deployments — they cannot run docker-compose stacks. The current swe-swe architecture requires docker-compose to orchestrate multiple services (swe-swe container, Traefik reverse proxy, auth service, optional code-server).

Users on these platforms cannot use swe-swe at all, despite only needing a single container with embedded auth.

## Decision

Add a `--dockerfile-only` flag to `swe-swe init` that generates a single Dockerfile suitable for deployment on single-container platforms.

### How it works

1. **`swe-swe init --dockerfile-only`**: Generates only the files needed for a single-container build:
   - `Dockerfile` (with `EXPOSE` and `ENV` for port/auth)
   - `entrypoint.sh`
   - `swe-swe-server/` source
   - `home/` directory
   - Skips: `docker-compose.yml`, `traefik-dynamic.yml`, `auth/` directory

2. **Port**: Server listens on `SWE_PORT` (default 1977) — same env var convention as compose mode.

3. **Auth**: Embedded in swe-swe-server, activated by `SWE_SWE_PASSWORD` env var:
   - Cookie-based (HMAC-SHA256, 7-day expiry)
   - Rate limiting (10 attempts / 5 min per IP)
   - Login form at `/swe-swe-auth/login`
   - Exempt paths: `/swe-swe-auth/login`, `/ssl/*`, `/mcp`

4. **No TLS**: The container does not terminate TLS — the platform (Fly, Railway, Render) provides it.

5. **Build & run**:
   ```
   docker build -t my-swe -f <sweDir>/Dockerfile <sweDir>/
   docker run -p 1977:1977 -e SWE_SWE_PASSWORD=mypass -v $(pwd):/workspace my-swe
   ```

### Auth architecture

In compose mode, Traefik + a separate auth service handle authentication. In dockerfile-only mode, the same auth logic is embedded directly in swe-swe-server via `auth.go`:

- When `SWE_SWE_PASSWORD` is unset → no auth (compose mode, Traefik handles it)
- When `SWE_SWE_PASSWORD` is set → embedded auth wraps `http.DefaultServeMux`

This is a runtime switch, not a compile-time one — the same binary supports both modes.

## Consequences

### Positive
- Enables deployment on single-container platforms (Fly.io, Railway, Render)
- No changes to existing docker-compose workflow
- Auth logic reused from standalone auth service (same cookie format, same rate limiting)
- Flag persisted in `init.json` for `--previous-init-flags=reuse`

### Negative
- No built-in TLS (platform must provide it)
- No Traefik routing features (path-based routing, automatic HTTPS redirect)
- No separate auth service (auth and server share process)

### Neutral
- Dockerfile-only mode uses the same container image — just different entrypoint config
- Same env var conventions (`SWE_PORT`, `SWE_SWE_PASSWORD`) across both modes
