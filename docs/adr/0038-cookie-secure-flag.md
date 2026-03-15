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

Use a hybrid approach with explicit override and proxy auto-detection:

```go
if v := os.Getenv("SWE_COOKIE_SECURE"); v != "" {
    isSecure = v == "true"
} else {
    isSecure = r.Header.Get("X-Forwarded-Proto") == "https"
}
```

### How each mode sets the flag

| Mode | `SWE_COOKIE_SECURE` | `X-Forwarded-Proto` | Cookie Secure |
|------|---------------------|---------------------|---------------|
| Compose with SSL | `true` (set in compose env) | `https` (Traefik) | Yes |
| PaaS (Fly/Railway) | not set | `https` (platform proxy) | Yes (auto) |
| Local no-SSL | not set | absent | No |

### Init-time configuration

- `docker-compose.yml` template sets `SWE_COOKIE_SECURE=true` inside `{{IF SSL}}` blocks
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
