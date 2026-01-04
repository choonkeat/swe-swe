# ADR-001: iOS Safari WebSocket with Self-Signed Certificates

## Status

Accepted

## Context

swe-swe provides browser-based terminal access to AI coding agents via WebSocket connections. For remote access (e.g., accessing a development server from an iPad), we support self-signed HTTPS certificates via `--ssl selfsign@<ip>`.

During development, we discovered that iOS Safari has fundamental limitations with WebSocket (`wss://`) connections to servers using self-signed certificates, even when those certificates are properly trusted by the user.

## Decision

**iOS Safari users should use Chrome (or another browser) instead of Safari when accessing swe-swe with self-signed certificates.**

We will:
1. Display a warning banner on the homepage when iOS Safari is detected
2. Document this limitation in the ADR
3. Not implement complex workarounds (HTTP fallback, polling, etc.)

## Investigation Summary

### What We Tried

1. **Proper Certificate Configuration**
   - Set `IsCA: true` for iOS Certificate Trust Settings toggle
   - Added external IP to Subject Alternative Names (SAN)
   - Set Common Name to access host
   - Result: HTTPS works, but WSS still fails

2. **HTTP Fallback Endpoint**
   - Added port `1${SWE_PORT}` (e.g., 11977) for plain HTTP
   - Created duplicate Traefik routers for HTTP entrypoint
   - Result: Works, but cookies fail on iOS Safari for IP addresses due to ITP

3. **HTTP Polling Fallback**
   - Implemented server-side polling endpoints
   - Client falls back from WebSocket to polling
   - Result: Added complexity, eventually removed

### Root Cause

iOS Safari silently blocks WebSocket (`wss://`) connections to self-signed certificates, even when:
- The certificate is installed as a profile
- Full trust is enabled in Certificate Trust Settings
- HTTPS page loads without warnings
- The exact same certificate works for HTTPS requests

**Symptoms:**
- `new WebSocket(url)` succeeds without exception
- `ws.readyState` stays at `0` (CONNECTING) forever
- No network request is ever made to the server
- `onopen`, `onerror`, `onclose` callbacks never fire

**The Web Inspector Paradox:**
When Safari Web Inspector (from a connected Mac) is attached, WebSocket connections work. This suggests the inspector bypasses certificate restrictions, which led us down debugging rabbit holes.

### Why Chrome Works

iOS Chrome uses its own networking stack for WebSocket connections and properly respects the system certificate trust store. When a self-signed certificate is trusted on iOS, Chrome's WebSocket connections work correctly.

## Consequences

### Positive
- Simple solution requiring no code changes to work around iOS Safari limitations
- No security compromises (no HTTP fallback needed)
- No added complexity (no polling fallback needed)
- Chrome on iOS provides a good user experience

### Negative
- iOS Safari users must switch browsers
- Users expecting Safari to work may be confused initially

### Neutral
- Production deployments should use Let's Encrypt certificates anyway
- This limitation only affects development/self-signed scenarios

## Alternatives Considered

### 1. HTTP Fallback Port
Expose HTTP on a second port for iOS Safari users.

**Rejected because:**
- iOS Safari's Intelligent Tracking Prevention (ITP) blocks cookies on IP addresses
- Authentication cookies don't persist after login redirect
- Security concerns with unencrypted traffic

### 2. HTTP Polling Fallback
Fall back to HTTP polling when WebSocket fails.

**Rejected because:**
- Added significant code complexity
- Higher latency and server load
- Still had issues with iOS Safari cookie handling
- Removed after implementation proved problematic

### 3. Require Domain Name + Let's Encrypt
Only support proper TLS certificates.

**Rejected because:**
- Many development scenarios use IP addresses
- Not everyone has a domain pointing to their dev server
- Self-signed certs are valuable for quick local/remote dev

## References

- iOS Safari WebSocket + self-signed cert issues are a known limitation
- Similar reports: developers often discover this when building real-time apps
- Apple has not documented this behavior officially
