# swe-swe ↔ swe-swe-tunnel integration

Companion to `/repos/swe-swe-tunnel/workspace/docs/design.md`. Tracks the swe-swe-side changes needed to run swe-swe behind a swe-swe-tunnel with no host port exposure.

## Context

`swe-swe-tunnel` (separate repo) provides a self-hosted reverse tunnel: a swe-swe instance makes one outbound connection to a public tunnel server, and all browser traffic to `{port}.{unique}-tunnel.example.com:443` flows back through it. The tunnel server terminates TLS using a per-session Let's Encrypt wildcard cert and demuxes by SNI; the client forwards each stream to the right `127.0.0.1:port`.

This unlocks:
- Hosting swe-swe on a PaaS (Fly, Render, Railway) that only exposes one port — outbound is enough.
- No DNS or LE setup on the swe-swe host itself.
- Per-session subdomain isolation matches swe-swe's existing per-port architecture.

## What changes for swe-swe

A single new input: **the public hostname swe-swe is reachable at**, e.g. `abc-tunnel.example.com`. From that, swe-swe derives:

1. **Cookie `Domain={hostname}`** so cookies span `1977.{hostname}`, `3000.{hostname}`, etc.
2. **Frontend URL templating**: leftmost-label swap. `1977.abc-tunnel.example.com` + target port 3000 → `3000.abc-tunnel.example.com`. Same shape as the existing `*.https.local.swe-swe.com` setup — likely already has helpers.
3. **Trust `X-Forwarded-Proto`** from the localhost-bound tunnel client (already done in `auth.go:resolveCookieSecure`).
4. **Recommended: bind localhost only** (`SWE_BIND=127.0.0.1`). Removes network attack surface; only the tunnel client (also localhost) reaches swe-swe.
5. **Skip own LE/Traefik cert handling** when tunnel mode is active. Tunnel terminates browser TLS; swe-swe speaks plain HTTP over localhost.

## How the hostname flows in

The tunnel client writes to a state file after successful registration:

```
/workspace/.swe-swe/tunnel-state.json
{
  "hostname": "abc-tunnel.example.com",
  "unique": "abc",
  "registered_at": "2026-04-26T10:00:00Z"
}
```

swe-swe resolves `SWE_PUBLIC_HOSTNAME` in this order on startup:
1. CLI flag `--public-hostname`
2. Env var `SWE_PUBLIC_HOSTNAME`
3. State file `/workspace/.swe-swe/tunnel-state.json` if it exists
4. Empty (legacy mode, no tunnel)

Hot-reload from the state file is not required for v1 — `unique` is stable per-server. Server restart picks up the current value.

## Phased plan

Each phase is independently mergeable.

### Phase A — audit existing label-swap code

Before designing, find what's already there. swe-swe's per-session preview ports use `{port}.https.local.swe-swe.com`. The frontend has to do the same label-swap there, so a helper likely exists.

Grep targets:
- `window.location.hostname` in `terminal-ui.js` and friends
- `${port}.` template strings
- `https.local.swe-swe.com` references
- `previewBaseUrl`, `fetchPreviewPort` (from per-session-preview-ports work)
- `Cookie.Domain` / cookie-domain settings in `auth.go`, `cookie.go`

Output: a list of touchpoints. Reuse existing helpers; don't invent parallel ones.

### Phase B — configuration surface

- New CLI flag: `--public-hostname` on `swe-swe-server` and `swe-swe init`.
- New env var: `SWE_PUBLIC_HOSTNAME`. Flag overrides env.
- Plumb into `Session` config and expose to the frontend via the existing config endpoint (whatever serves the frontend bootstrap JSON).
- When unset → current behavior unchanged (back-compat).

### Phase C — cookie domain

- `auth.go`: when `SWE_PUBLIC_HOSTNAME` is non-empty, set `Cookie.Domain = "." + SWE_PUBLIC_HOSTNAME` for all auth cookies.
- Test: cookie set at `1977.abc-tunnel.example.com` is sent on requests to `3000.abc-tunnel.example.com` (browser-level test or a cookie-jar simulation).
- Edge: dev mode without public hostname → cookie stays host-only.

### Phase D — frontend URL builder

Single helper, used everywhere that builds cross-port URLs:

```js
function urlForPort(port, path = "/") {
  const host = window.location.hostname;
  const labels = host.split(".");
  labels[0] = String(port);
  const newHost = labels.join(".");
  const protocol = window.location.protocol;
  return `${protocol}//${newHost}${path}`;
}

function wsUrlForPort(port, path = "/") {
  const wsProto = window.location.protocol === "https:" ? "wss:" : "ws:";
  // ...
}
```

Replace inline `${hostname}:${port}` constructions across:
- preview iframe `src` (terminal-ui.js, ~line 2620)
- agent-chat URL builder
- VNC URL builder
- public-port URL builder
- WebSocket debug URL (terminal-ui.js, ~line 776)

This helper should also work for the existing `*.https.local.swe-swe.com` setup (label-swap is identical) — likely deduplicates code.

### Phase E — trust proxy headers from localhost

- Tunnel client adds `X-Forwarded-Proto: https`, `X-Forwarded-Host`, `X-Forwarded-For` to forwarded HTTP requests.
- `auth.go:resolveCookieSecure` already reads `X-Forwarded-Proto`. Verify it doesn't gate on source IP; if it does, allow `127.0.0.1` by default.
- Document: tunnel mode requires localhost trust. Don't expose swe-swe to non-localhost without putting it behind a proxy you control.

### Phase F — bind localhost in tunnel mode

- Add `--bind` flag and `SWE_BIND` env. Default `0.0.0.0:1977` for back-compat.
- Document: when running behind tunnel, set `SWE_BIND=127.0.0.1`.
- Same change for compose mode: Traefik (when present) would also bind localhost-only. Or simpler — drop Traefik when in tunnel mode, since the tunnel handles routing.

### Phase G — disable swe-swe LE/Traefik when tunnel mode active

- swe-swe today uses Traefik + HTTP-01 for its own host cert.
- When `SWE_PUBLIC_HOSTNAME` is set: skip the `letsencrypt` cert resolver, skip Traefik altogether, run swe-swe-server directly on `127.0.0.1:1977`.
- Compose template branches: `if SWE_PUBLIC_HOSTNAME → minimal compose with no Traefik` else current behavior.
- Net result: a tunneled swe-swe host has no inbound exposure at all (no port 80, no port 443, no LE, no Traefik). Just outbound to the tunnel server.

### Phase H — docs page

`www/swe-swe-tunnel.md`, parallel to the existing `www/swe-swe-tailscale.md`. Sections:
- What it does, when to use it.
- Setup: register with tunnel server, run `swe-swe-tunnel daemon`, configure swe-swe with `SWE_PUBLIC_HOSTNAME`.
- Compared to: Tailscale Funnel, Cloudflare named tunnels, ngrok.
- Cost: $0 self-hosted; one VPS for the tunnel server.
- Caveats: LE rate limits, requires the tunnel-server operator to trust the swe-swe host (terminate-at-server model).

## File-level touchpoints (estimated)

Specific file/line numbers from the per-session-preview-ports work and recent transcripts. Verify by `git grep` before editing.

| File | What changes |
|---|---|
| `cmd/swe-swe/main.go` | Read `--public-hostname` / `SWE_PUBLIC_HOSTNAME`. Read tunnel state file. Branch compose template. |
| `cmd/swe-swe-server/main.go` (or equivalent) | Wire public hostname into session config, expose to frontend via bootstrap JSON. |
| `cmd/swe-swe/templates/host/swe-swe-server/auth.go` | Set `Cookie.Domain` when public hostname is set. |
| `cmd/swe-swe/templates/host/docker-compose.yml` | Branch on `SWE_PUBLIC_HOSTNAME`: minimal mode (no Traefik) when set. |
| Frontend (likely `terminal-ui.js`) | New `urlForPort` / `wsUrlForPort` helpers. Replace inline constructions. |
| `www/swe-swe-tunnel.md` | New page. |

## Audit checklist (Phase A deliverable)

- [ ] Grep for `window.location.hostname` — list every file building URLs from it.
- [ ] Grep for `https.local.swe-swe.com` — find the existing per-session subdomain code.
- [ ] Grep for `Cookie.Domain` / cookie domain handling.
- [ ] Identify the bootstrap config endpoint (where frontend gets server-side config).
- [ ] Check if `previewCertRefresher` and the cert-fetching goroutine in `main.go` need a tunnel-mode-skip branch.
- [ ] Verify `X-Forwarded-Proto` handling in `resolveCookieSecure` — does it gate on source IP?

## Open questions

1. **Coexistence with `*.https.local.swe-swe.com` setup.** Tunnel mode generalizes it. Options:
   - Keep both: tunnel for public, local-https for offline development. Probably right.
   - Deprecate local-https in favor of a "local tunnel" mode (tunnel server runs on localhost). More work, less benefit.
2. **Compose mode's per-session preview ports** (`SWE_PREVIEW_PORTS=3000-3019` etc.). Tunnel client needs to forward those too. Either:
   - Tunnel client reads the same env vars and forwards every port in the union of ranges.
   - Tunnel server allocates a stable range; client forwards by exact match.
   - Recommend: tunnel client forwards any port the server demuxes a stream for, default `127.0.0.1:dst_port`. swe-swe doesn't need to teach the tunnel about its port set.
3. **Multiple swe-swe instances per host** with one tunnel client? Not in v1. One tunnel client per swe-swe-server. If you need multiple, run multiple tunnel clients with different `unique`s.
4. **Should swe-swe `init` automatically launch the tunnel client subprocess?** Convenient but couples lifecycles. Default: keep them separate. User runs `swe-swe-tunnel daemon &` then `swe-swe init --public-hostname=...`. Power-user flag `--with-tunnel` later.

## Non-goals

- Replacing Tailscale support (`www/swe-swe-tailscale.md`). Tunnel and Tailscale are alternatives; users pick one.
- Replacing the local-dev subdomain setup. They serve different purposes.
- Building a UI for tunnel registration inside swe-swe. Run the tunnel client separately.
