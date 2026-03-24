# ADR-0040: Same-Origin VNC Readiness Probe

**Status**: Accepted
**Date**: 2026-03-24

## Context

The Agent View tab probes websockify readiness before loading the VNC iframe. The original implementation used `fetch()` with `mode: 'no-cors'` directly against the cross-origin VNC port (e.g., `:27000/vnc_lite.html`).

With `no-cors`, all server responses are opaque -- the browser cannot read the status code. When Traefik returned a 502 Bad Gateway (websockify not yet ready), the probe saw an opaque response and treated it as "ready". The iframe loaded, showed "Bad Gateway", and never retried.

## Decision

Add a same-origin readiness endpoint `GET /api/session/{uuid}/vnc-ready` on the main swe-swe-server. This endpoint performs a local TCP connect to the session's websockify port and returns:
- `200 {"ready":true}` if websockify is listening
- `503 {"ready":false}` if the connection fails

The client probes this same-origin endpoint instead of the cross-origin VNC port. Since it's same-origin, the browser can read the real HTTP status code and only proceed when the response is 200.

## Consequences

Good:
- Eliminates the "Bad Gateway" race condition entirely
- No CORS complications -- same-origin requests have full response visibility
- TCP connect is lightweight (~0.5ms) and doesn't load websockify
- Works regardless of reverse proxy behavior (Traefik, Caddy, etc.)

Bad:
- Adds a server-side endpoint (minimal complexity)
- Probe traffic goes through the main server instead of directly to the VNC port (negligible overhead since it's just a TCP connect check)
