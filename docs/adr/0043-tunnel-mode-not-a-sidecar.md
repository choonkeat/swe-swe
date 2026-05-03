# ADR-0043: Tunnel mode is not a runtime sidecar

**Status**: Accepted
**Date**: 2026-05-03
**Research**: [ADR-0042](0042-tunnel-subprocess-supervisor.md), [docs/tunnel-explained.md](../tunnel-explained.md), [tasks/2026-04-29-tunnel-subprocess-pivot.md](../../tasks/2026-04-29-tunnel-subprocess-pivot.md)

## Context

Tunnel mode is enabled at image build time via `swe-swe init --tunnel-server-url=...`. The flag rebuilds the Dockerfile to drop Traefik + the per-port label scheme (commit `5c3f96256`), bake the `swe-swe-tunnel` client into the image (commit `5e1916a5c`), and have `swe-swe-server` supervise the tunnel client as an in-process child (ADR-0042).

A natural-looking shortcut presents itself: run plain `swe-swe up` (compose mode with Traefik) and bolt an external `swe-swe-tunnel` client onto it as a sidecar -- the tunnel client dispatches `{port}.{hostname}` requests to local ports, and `swe-swe up` is already running on those ports. At the TCP layer this works; the swe-swe login form even renders correctly. But after a successful password submit, the user is bounced back to the login screen.

This ADR documents *why* the sidecar shape cannot work, so that future readers (and our own future selves) don't relitigate it.

## Decision

Tunnel mode requires `swe-swe init --tunnel-server-url=...` to bake the tunnel client into the image and the supervisor into `swe-swe-server`. Running an external `swe-swe-tunnel` against a `swe-swe up` stack is **not supported** and cannot work without code changes to swe-swe-server, swe-swe's frontend, and the compose/Traefik wiring.

The single load-bearing piece is the `liveTunnelHostname` atomic at `cmd/swe-swe/templates/host/swe-swe-server/tunnel_supervisor.go:127`. It is set *only* by `setLiveTunnelHostname`, called by the in-process supervisor on the tunnel child's `register_ok` event. There is no env var, flag, file, or HTTP endpoint that populates it. With the supervisor absent, `getLiveTunnelHostname()` returns `""` for the lifetime of the process, and that empty string ripples through every part of the system that needs to know the public hostname.

## Why a sidecar tunnel against `swe-swe up` cannot work

### 1. Cookie Domain -- the bounce-back

`auth.go:312-314, 360`. After a successful password POST, swe-swe-server sets `swe_swe_session` with:

```go
Domain: resolveCookieDomain(getLiveTunnelHostname())  // "" -> host-only
```

With `Domain=""`, the cookie is host-only on `1977.{u}-tunnel.{apex}`. The login redirect (`Location: /`) lands on the same host, so a single-page session would *appear* to work -- but the swe-swe UI immediately starts loading per-port subdomain iframes (`{port}.{u}-tunnel.{apex}` for preview / Agent View / VNC), and those subdomains have no cookie. Each iframe gets 401, the parent UI's reachability probe interprets the 401 as auth-not-yet, and the user is bounced back to login.

The in-process path fixes this by emitting `Domain={u}-tunnel.{apex}` so the cookie is sent across all per-port subdomains (commit `0d226e8f6`). Without the supervisor, that domain string is unknowable at cookie-set time.

### 2. Frontend subdomain URL construction

`buildSubdomainPreviewUrl`, `buildSubdomainAgentChatUrl`, `getBrowserViewUrl`, `setPreviewURL` (frontend) all branch on `tunnelStatus.publicHostname` from the WebSocket status frame. The WS frame is built by `buildStatusPayload` from `getLiveTunnelHostname()`. Empty -> `publicHostname=""` -> frontend falls back to path-base URLs (`/preview/...`, `/agent-chat/...`).

The tunnel routes by **leftmost-label port** (`PortDispatchHandler` in the swe-swe-tunnel client at `internal/tunnelclient/client.go:387`), not by path. Path-base URLs do not resolve through the tunnel's wildcard. Iframes load `about:blank` or the apex landing page instead of the intended target.

### 3. Per-port iframe auth returns 401, not 302

`requireAuthCookie` at `auth.go:458` deliberately returns 401 (not a 302 to login) for the per-session proxy ports because cross-origin iframes cannot follow a login redirect -- the user would have to log in inside the iframe, which the parent app can't read. The 401-instead-of-redirect contract assumes the cookie has already been set with `Domain={apex}` on the apex login. With `Domain=""`, the iframe never has the cookie, gets 401 forever, and the parent UI shows broken panes.

### 4. Landing page rendering

`templates/host/swe-swe-server/main.go` (the `$PORT` landing handler, when bound) renders `OPEN AT https://{port}.{publicHostname}/` from the live atomic. Empty hostname -> no usable URL, just a stub. The PaaS health check still returns 200 (so the container looks healthy), but the operator has no click-through link.

### 5. Traefik incompatibility

`swe-swe up` runs Traefik with router rules pinned to `Host(\`{{DOMAIN}}\`)` and a `forwardauth` middleware on `/swe-swe-auth/verify`. Two failure modes:

- Wildcard subdomains like `4500.{u}-tunnel.{apex}` don't match the configured `Host(...)` rule. They fall through to the catchall `PathPrefix(\`/\`)` router, which then runs `forwardauth`, which builds login redirects from `X-Forwarded-Host`. Those redirects span subdomains the cookie isn't valid on (per #1).
- The TLS config in compose mode points at Let's Encrypt for `{{DOMAIN}}`, not at the tunnel's wildcard apex. TLS termination is duplicated and inconsistent.

The tunnel-mode template drops Traefik entirely (commit `5c3f96256`) precisely because of this. Putting Traefik back in front of swe-swe-server in tunnel mode would require either re-deriving the tunnel apex into Traefik's router rules at runtime (no mechanism for that) or running a separate Traefik instance per tenant.

### 6. Cookie scope on auth flow restart

When the tunnel client reconnects with a new label (the supervisor handles this transparently in tunnel mode -- see ADR-0042), the cookie's `Domain={apex}` keeps existing sessions alive across the rebind. With `Domain=""` and a host-only cookie, every label rotation invalidates every active session. In a sidecar shape there is no observation channel for the rotation event in the first place -- swe-swe-server doesn't know it happened.

## What is *not* the problem

`X-Forwarded-Proto` is correctly propagated end-to-end and is **not** the failure mode:

- `swe-swe-tunneld` terminates TLS and calls `req.SetXForwarded()` at `cmd/swe-swe-tunneld/tunnel.go:821`, which sets `X-Forwarded-Proto: https` from `req.In.TLS != nil`.
- The tunnel client preserves `X-Forwarded-{For,Host,Proto}` when reverse-proxying to the local upstream at `internal/tunnelclient/client.go:397`.
- swe-swe-server's `resolveCookieSecure` (`auth.go:291`) honors the header and emits `Secure: true`.

The `Secure` flag is correctly set in the sidecar shape. It is a red herring during diagnosis -- both the symptom and the user's first instinct point at it, but the cookie's `Secure` attribute is fine. The bug is `Domain`.

## Consequences

**Good**: tunnel-mode users get a single coherent flow. Cookie domain, subdomain URLs, landing page, iframe auth, supervisor lifecycle, and WS status all stay consistent and are tested end-to-end via `make tunnel-up-manual` and the `swe-swe-tunnel` repo's e2e suite (`cmd/swe-swe-tunneld/e2e_test.go`).

**Bad**: users running `swe-swe up` for local dev cannot trivially expose it through an external tunnel for a quick public preview. The supported alternatives are:

- Build a tunnel-mode image (`swe-swe init --tunnel-server-url=...`) and run that instead -- it can run on the same machine, identity reused via the named docker volume.
- Use a path-routed tunnel (cloudflared, ngrok with custom-domain) that doesn't require subdomain-per-port -- the swe-swe UI in legacy mode already addresses sub-services by path.
- Add a partial workaround inside swe-swe-server (e.g. an env-var fallback in `getLiveTunnelHostname()` such as `SWE_PUBLIC_HOSTNAME`) to fix #1 only. This would still leave Traefik routing (#5), iframe URL construction (#2), and rotation observability (#6) broken. We have not done this; it would create a half-working mode that produces confusing failure reports.

## Alternatives Considered

- **Env-var fallback for `liveTunnelHostname` (e.g. `SWE_PUBLIC_HOSTNAME`).** Cheap, fixes the cookie Domain. But it only addresses one of six failure modes above and would mask the others, leading to "I set the env var, why is it still broken" reports. Rejected as a partial fix masquerading as a full one.
- **Separate `tunnel-mode` runtime flag (no rebuild).** Would require dropping Traefik at runtime, swapping listen addresses, and ferrying the public hostname in via the env. Possible but doubles the supported surface area for compose mode and conflicts with the existing Traefik wiring. Rejected.
- **Document the sidecar pattern as supported and invest in making it work.** Would mean rewriting the per-port iframe auth (currently 401-only), making the frontend tolerate path-base in tunnel-routed mode, and either dropping Traefik or making it tunnel-aware. Large multi-component change to support a use case adequately served by `swe-swe init --tunnel-server-url=...`. Rejected.
- **Status quo (chosen): single supported tunnel path, document the failure mode loud and clear.** Eliminates ambiguity and points users at the working pattern. This ADR is half of that.
