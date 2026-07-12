# ADR-0045: Preview host-demux (vhost apps, two-hostname model)

**Status**: Accepted
**Date**: 2026-07-12
**Research**: [ADR-0033 reachability probe], [ADR-0043](0043-tunnel-mode-not-a-sidecar.md), [tasks/2026-07-04-preview-hostname-vhost.md](../../tasks/2026-07-04-preview-hostname-vhost.md), recording `14605713-38f6-48b8-9621-f131051276a6`

## Context

Users' compose stacks often serve virtual hosts -- `app1.lvh.me:3000` and
`app1.lvh.me:5000` -- on the swe-swe machine, where an internal router
(traefik/nginx) dispatches on the `Host` header. The App Preview iframe runs in
the user's browser, which is usually on a *different* machine. There,
`*.lvh.me` resolves to the *user's own* loopback, so typing `app1.lvh.me:3000`
can never reach swe-swe.

Agent View solved its copy of this (commit `d5266dfb4`) by remapping the
off-host chromium's resolver (`--host-resolver-rules`) so `lvh.me` points back
at the swe-swe host. That trick is only possible because that chromium runs
next to swe-swe. The Preview iframe runs in the user's browser; no resolver
tricks are available there.

Two concrete blockers existed on `main`:

1. Frontend `setPreviewURL()` bounced every non-`localhost` host to a new tab.
2. The preview proxy had a single fixed target and clobbered the upstream
   `Host` to that target (`outReq.Host = target.Host`), and unconditionally
   stripped `Set-Cookie` `Domain=`.

## Decision

### Two-hostname model

The browser-facing URL and the upstream (logical) `Host` are *different names*:

```
browser: http://app1-5000.<reach>:23000/...
   -> per-session listener :23000 demuxes leftmost label "app1-5000"
   -> proxies to 127.0.0.1:5000 with Host: app1.lvh.me:5000
   -> the user's compose traefik matches Host(`app1.lvh.me`) as on a laptop
```

- **`<reach>`** is a domain whose `*.` wildcard resolves to the swe-swe machine
  *from the user's browser*: `lvh.me` (same-machine only), `<ip>.sslip.io` /
  `nip.io` (bare-IP deployments, resolves anywhere), an admin-owned wildcard, or
  an explicit `SWE_PREVIEW_REACH_DOMAIN`.
- The upstream `Host` is **rewritten** to the logical vhost
  (`app1.lvh.me:5000`), never passed through -- no traefik rule matches
  `app1-5000.<ip>.sslip.io:23000`. Logical suffix defaults to `lvh.me`, override
  with `SWE_PREVIEW_VHOST_SUFFIX`.
- `Set-Cookie Domain=` is **rewritten** logical->reach (`.lvh.me` ->
  `.<reach>`), not stripped (breaks shared auth across the reach origins) and not
  preserved (the browser rejects a `.lvh.me` Domain set on an `.sslip.io` page).
  No-Domain cookies keep today's strip behavior.

The library (`agent-reverse-proxy` v0.2.11) grew two optional per-request hooks
to make this possible without disturbing existing behavior:
`ResolveTarget(inboundHost) (target, upstreamHost, ok)` and
`CookieDomainRewrite(inboundHost, domain) string`. A nil/zero hook reproduces
v0.2.9 byte-for-byte.

### Label grammar (listener resolution precedence)

1. `{name}-{port}`, port 1024-65535 -> target `127.0.0.1:{port}`, upstream Host
   `{name}.{suffix}:{port}`. Split on the *last* dash-number segment:
   `my-app-5000` -> (`my-app`, `5000`).
2. bare `{port}` (numeric) -> target `127.0.0.1:{port}`, upstream Host
   `localhost:{port}` (tunnel-style).
3. bare `{name}` -> the primary PreviewPort vhost (`{name}.{suffix}:{PreviewPort}`),
   unless the label equals the reach's own first label (a bare-reach browse).
   A session **pin** wins over this rule.
4. no label / single-label host / unrecognized / port out of range -> today's
   behavior: primary PreviewPort, Host clobbered to `localhost:{port}`. **Nothing
   existing breaks.** `localhost` and `127.0.0.1` naturally land here (single
   label, and `127` is a sub-1024 numeric label that is rejected).

Labels validate as `[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?`; targets are loopback
only; unknown labels never auto-allocate ports.

### Reach discovery + pinned (degraded) mode -- wildcard is NOT assumed

A swe-swe hostname does not always support wildcard subdomains (corporate DNS,
`/etc/hosts`, air-gapped LAN). Mirroring ADR-0033, the browser probes and
degrades *visibly*:

- The server sends ordered `previewReachCandidates` (explicit
  `SWE_PREVIEW_REACH_DOMAIN` if set, else `lvh.me`) and `previewVhostSuffix` in
  the WS status frame; the frontend appends its own `window.location.hostname`.
- The browser probes `http(s)://probe-<rand>.<candidate>:<proxyPort>/__probe__`
  (the auth-exempt path, so a password session still resolves) expecting the
  `X-Agent-Reverse-Proxy` response header. First success -> **wildcard mode**
  with that reach. All fail -> **pinned mode**.
- Pinned mode: a single origin `<hostname>:<proxyPort>`. The URL bar still
  accepts `app1.lvh.me:5000`; the frontend POSTs
  `/__agent-reverse-proxy-debug__/vhost-pin` `{"name":"app1","port":5000}` to the
  session listener; label-less requests then route to the pinned target with a
  rewritten Host. Trade-offs (accepted): one vhost at a time per session, shared
  cookie jar. This is the pre-ADR-0025 target-switch API resurrected
  *per-session* (the old sin was one global mutable proxy, not mutability).
- The active mode + reach are shown next to the URL bar. Degradation is visible,
  never silent.

### Tunnel mode degrades by design

`swe-swe-tunnel` demuxes the leftmost label as a *raw port number*. Named labels
(`app1-5000`) and probe labels (`probe-<rand>`) are not numeric, so over the
tunnel hostname the reach probe fails and the session lands in pinned mode --
correct, designed behavior here. Wildcard-over-tunnel is a follow-up in the
swe-swe-tunnel repo (make tunneld forward any non-numeric leftmost label
unchanged to the session's preview listener, where this ADR's grammar already
lives).

## Why the alternatives are wrong

- **Pass the browser-facing Host through unchanged.** No traefik rule matches
  `app1-5000.<ip>.sslip.io:23000`; the user's router 404s. The whole point is to
  present the *logical* `app1.lvh.me:5000` upstream.
- **Strip `Set-Cookie Domain=` (today's blanket behavior).** A shared-auth cookie
  scoped `Domain=.lvh.me` becomes host-only on one reach origin and is not sent
  to the sibling vhost origin, breaking cross-vhost SSO.
- **Preserve `Set-Cookie Domain=.lvh.me`.** The browser rejects a `.lvh.me`
  Domain on a page served from `.sslip.io` (domain-match failure). Rewriting to
  `.<reach>` is the only correct option.

## Known limitation: wildcard mode + password

The apex login cookie (`swe_swe_session`) is host-only in non-tunnel mode
(`resolveCookieDomain` returns `""`; see ADR-0043 section 1). Cookies ignore
ports, so **pinned mode works under a password** (the bare origin
`<hostname>:<proxyPort>` is the same host as the login page). But **wildcard
mode** loads `app1-5000.<reach>` -- a *different host* -- so the host-only cookie
is not sent and the iframe gets 401, exactly the bounce ADR-0043 documents.

Decision (option B): ship wildcard mode for password-free / same-machine (dev
`lvh.me`) use, and pinned mode for password-protected deployments; document the
limit rather than block. The tunnel-style fix (emit `Domain=<reach>` for the
auth cookie once the reach is known) is a clean follow-up mirroring
`0d226e8f6`, deferred until there is demand.

## Relationship to prior ADRs

- **ADR-0025 / 0028 / 0032** (one global proxy -> per-session): pinned mode's
  mutable target is safe because it is *per-session*, not the old global proxy.
- **ADR-0033** (reachability probe): the reach probe reuses that visible-degrade
  pattern.
- **ADR-0043** (tunnel cookie Domain): the wildcard+password limitation is the
  same cookie-scope mechanism, generalized to an arbitrary reach.
- **`d5266dfb4`** (Agent View resolver remap): solves the *off-host chromium*
  case; the Preview iframe cannot, hence this demux.
