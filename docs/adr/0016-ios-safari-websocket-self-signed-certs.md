# ADR-016: iOS Safari WebSocket with Self-Signed Certificates

**Status**: Accepted
**Date**: 2026-01-05

## Context

swe-swe supports self-signed HTTPS certificates via `--ssl selfsign@<ip>` for remote access. iOS Safari has fundamental limitations with WebSocket (`wss://`) connections to self-signed certificates, even when properly trusted by the user.

Symptoms:
- `new WebSocket(url)` succeeds without exception
- `ws.readyState` stays at `0` (CONNECTING) forever
- No network request is ever made to the server
- `onopen`, `onerror`, `onclose` callbacks never fire

Paradoxically, connections work when Safari Web Inspector is attached, suggesting the inspector bypasses certificate restrictions.

## Decision

**iOS Safari users should use Chrome instead of Safari when accessing swe-swe with self-signed certificates.**

We display a warning banner on the homepage when iOS Safari is detected. No complex workarounds (HTTP fallback, polling) are implemented.

iOS Chrome uses its own networking stack and properly respects the system certificate trust store.

## Consequences

Good:
- Simple solution requiring no code changes
- No security compromises (no HTTP fallback)
- No added complexity (no polling fallback)
- Chrome on iOS provides a good user experience

Bad:
- iOS Safari users must switch browsers
- Users expecting Safari to work may be confused initially

Neutral:
- Production deployments should use Let's Encrypt certificates (now supported via `--ssl=letsencrypt@domain`)
- This limitation only affects development/self-signed scenarios
- Let's Encrypt certificates work correctly in iOS Safari

## Alternatives Considered

1. **HTTP Fallback Port**: Rejected - iOS Safari's ITP blocks cookies on IP addresses
2. **HTTP Polling Fallback**: Rejected - Added complexity, cookie handling issues, removed after implementation proved problematic
3. **Require Domain + Let's Encrypt**: Rejected - Many dev scenarios use IP addresses, self-signed certs are valuable for quick dev
