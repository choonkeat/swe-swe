# ADR-0038: Hybrid Cookie Secure Flag

**Status**: Accepted
**Date**: 2026-03-15

## Context

With embedded auth in swe-swe-server (ADR-0037), the server sets a session cookie after login. The cookie's `Secure` flag determines whether browsers send it only over HTTPS connections.

swe-swe runs in three deployment modes:

1. **Compose with SSL** (Traefik terminates TLS): Server receives plain HTTP from Traefik, `r.TLS` is nil
2. **Dockerfile-only on PaaS** (Fly, Railway terminate TLS): Server receives plain HTTP from the platform proxy, `r.TLS` is nil
3. **Dockerfile-only local** (no TLS anywhere): Plain HTTP end-to-end

In all three cases, the Go server's `r.TLS` is nil because TLS is terminated upstream. The original approach of checking `r.TLS != nil` never worked for any real deployment.

Additionally, browsers enforce HSTS across ports — visiting `https://example.com:1977` (compose with SSL) can cause the browser to also force HTTPS on `https://example.com:1978` (dockerfile-only test), making plain HTTP cookies fail silently.

## Decision

Prefer per-request proxy detection, fall back to an env-var override when no proxy header is present:

```go
if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
    return p == "https"
}
if v := os.Getenv("SWE_COOKIE_SECURE"); v != "" {
    return v == "true"
}
return false
```

The precedence was inverted on 2026-04-19: the env-var override previously won unconditionally, which broke direct Tailscale hits on the swe-swe-server HTTP port (bypassing Traefik). With the header taking priority, each request gets the correct Secure flag for its actual transport; the env var is a fallback for deployments where no proxy sets the header.

### How each mode sets the flag

| Mode | `SWE_COOKIE_SECURE` | `X-Forwarded-Proto` | Cookie Secure |
|------|---------------------|---------------------|---------------|
| Compose with SSL, via Traefik | `true` (set in compose env, now redundant) | `https` (Traefik) | Yes |
| Compose with SSL, direct HTTP to swe-swe-server (e.g. Tailscale) | `true` (ignored, header is absent → falls through) | absent | No (correct for plain HTTP) |
| PaaS (Fly/Railway) | not set | `https` (platform proxy) | Yes (auto) |
| Local no-SSL | not set | absent | No |

### Init-time configuration

- `docker-compose.yml` template sets `SWE_COOKIE_SECURE=true` inside `{{IF SSL}}` blocks -- now mostly redundant since Traefik always sets `X-Forwarded-Proto`, but retained as a belt-and-braces signal for custom proxy setups that drop the header
- Dockerfile-only mode does not set the env var (relies on proxy auto-detection or explicit user override)

## Consequences

### Positive
- Compose with SSL works deterministically via explicit env var
- PaaS deployments work automatically via `X-Forwarded-Proto` without user configuration
- Local development works without Secure cookies blocking login
- Users can override with `-e SWE_COOKIE_SECURE=true` if they add their own TLS termination

### Negative
- If a PaaS doesn't set `X-Forwarded-Proto`, the user must set `SWE_COOKIE_SECURE=true` manually
- HSTS from same-domain SSL deployments can still interfere with HTTP testing on different ports (browser behavior, not fixable server-side)

### Neutral
- `SWE_COOKIE_SECURE` is a new env var that users may need to know about for custom setups
