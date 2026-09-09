# Wildcard host-demux: one port, no tunnel

**Status**: phases 1-2 done 2026-09-10 on branch `feat/wildcard-host-demux`. Phases 3-4 pending.

## Goal

On a box whose `*.example.com` DNS points at it, swe-swe should serve every
pane through its ONE listening port, addressing each internal port as a
subdomain label -- exactly what tunnel mode already does, minus the tunnel:

```
http://1977.example.com:1977/    -> the swe-swe UI
http://23000.example.com:1977/   -> that session's preview proxy
http://27000.example.com:1977/   -> that session's VNC proxy
```

The connection port never changes. Only the leftmost label varies, and it
names the local port to reach. A wildcard box then needs **no swe-swe-tunnel
at all**, which is the whole point: today it is told to run one.

## Why this is small

The subdomain machinery already exists and is exercised in tunnel mode. All of
it hangs off ONE value, `liveTunnelHostname`
(`swe-swe-server/tunnel_supervisor.go`), which today only the in-process tunnel
supervisor ever sets, on the client's `register_ok` event:

- `resolveCookieDomain` emits `Domain=<apex>` so one login covers every
  per-port subdomain (`auth.go`).
- The frontend's `buildSubdomainPreviewUrl` / `buildSubdomainAgentChatUrl` /
  `getBrowserViewUrl` / `setPreviewURL` all branch on
  `tunnelStatus.publicHostname`, which `buildStatusPayload` reads from that
  same value.
- `requireAuthCookie`'s 401-not-302 contract for cross-origin iframes assumes
  that cookie domain, and works once it is set.
- The landing page renders `OPEN AT https://{port}.{publicHostname}/` from it.

So the missing piece is not the addressing. It is the thing that **receives**
`23000.example.com` and forwards it to `127.0.0.1:23000`. In tunnel mode the
tunnel server does that. Here swe-swe-server does it on its own listener.

## ADR-0043's six breaks, re-checked against THIS shape

ADR-0043 rejected a `SWE_PUBLIC_HOSTNAME` env fallback as "a partial fix
masquerading as a full one", because in the *sidecar tunnel* shape it fixed
break 1 and left 2, 5 and 6 broken. That reasoning does not carry over
unchanged, and the difference is the demux living inside swe-swe-server:

| # | Break | Status in this shape |
|---|-------|----------------------|
| 1 | Cookie `Domain` empty -> login bounce | **Fixed** by setting the same value |
| 2 | Frontend builds path URLs the wildcard can't route | **Fixed**: the frontend builds subdomain URLs from the same value, and unlike the sidecar, those URLs now resolve -- our own demuxer serves them |
| 3 | Per-port iframe 401 forever | **Fixed**: follows from 1 |
| 4 | Landing page has no usable URL | **Fixed**: follows from the same value |
| 5 | Traefik `Host()` rules don't match wildcards; TLS pinned to one domain | **REAL, and the reason this is host-runtime only.** Dockerless has no Traefik. Compose mode must reject the flag |
| 6 | Label rotation invalidates sessions, unobservable | **Moot**: a static wildcard domain never rotates |

Five of six either do not apply or fall out of the one value. Break 5 is the
constraint, not a blocker: scope this to `--runtime=host`.

## Design

### 1. The setting

`swe-swe up --public-hostname=example.com` (env `SWE_PUBLIC_HOSTNAME`), host
runtime only. Rejected with a clear message under compose (break 5). It sets
the same atomic the tunnel supervisor sets, so every consumer listed above
lights up unchanged.

Mutually exclusive with `--tunnel-server-url`: both want to own that value, and
if both are set the last writer would silently win.

### 2. The demux

In front of the main handler, before auth:

- Split the request Host. If the leftmost label is all digits AND the rest
  equals the configured hostname, reverse-proxy to `127.0.0.1:<label>`.
- Anything else falls through to today's handler untouched, so a box reached
  by IP or by the bare domain behaves exactly as it does now.

Reuse the `agent-reverse-proxy` `ResolveTarget` hook that ADR-0045 already
added for the preview listener rather than writing a second proxy.

### 3. The allowlist (security, do not skip)

A public hostname that forwards to any `127.0.0.1:<n>` is an open relay into
every local service on the box, swe-swe's own broker included. Only these are
routable:

- the server's own port
- `proxyPortOffset` + each configured range: preview, agent-chat, public, cdp,
  vnc, files (today 23000-23019, 24000-24019, 25000-25019, 26000-26019,
  27000-27039, 29000-29019)

Anything else: 404, logged once per distinct port so a misconfiguration is
visible without a log flood. The ranges are already variables in `main.go` and
move with `--preview-ports` / `--proxy-port-offset`, so derive, never hardcode.

### 4. TLS

Out of scope for phase 1, and say so plainly in the docs. `https://<port>.<domain>`
needs a certificate valid for `*.<domain>`, which Let's Encrypt only issues via
a DNS-01 challenge -- a different mechanism from the HTTP-01 the `--ssl` modes
use today. Phase 1 is plain http, or an admin-supplied wildcard certificate.

## Phases

1. **Setting + plumbing.** DONE. `-public-hostname` / `SWE_PUBLIC_HOSTNAME`,
   host runtime only, mutually exclusive with the tunnel flag, feeding the
   existing atomic (`public_hostname.go`). Unit tests: precedence,
   normalization (accepts `*.example.com`, a pasted URL, a trailing dot;
   rejects a port, an IP literal, a single label, bad labels), rejection under
   compose, and the tunnel interaction.

   Two things came out of building it that the plan did not have:

   - **How "host runtime only" is detected.** The server cannot infer it, so
     `swe-swe up` now exports `SWE_RUNTIME=host` for a dockerless project
     (`dockerlessServerInvocation`) and the server gates on that. Compose never
     sets it. An undeclared runtime is refused with the fix named in the
     message.
   - **A flag beats an inherited env var.** A live smoke on this box (which
     carries `SWE_TUNNEL_SERVER_URL` in its environment) died on a tunnel
     conflict nobody asked for. So the mutual exclusion is decided by WHERE
     each value came from: an explicit flag wins over an inherited variable,
     either direction, with a log line saying which lost; both explicit or
     both inherited stays a hard error. Same family as the browser-backend
     port bug (7b343826f).
2. **Demux + allowlist.** DONE (`public_hostname_demux.go`). Host parsing,
   target resolution, the port allowlist derived from the live range
   variables. Unit tests cover the parser (including `23000.evil.com`,
   `23000.example.com.evil.com`, `a.23000.example.com`, `23000x.example.com`,
   out-of-range and IP-literal Hosts), the allowlist in both directions, and
   the handler end to end (fallthrough, 404, forward-with-Host-preserved,
   502).

   Three things settled while building it:

   - **A request for the server's own port falls through instead of being
     proxied.** It has to: the landing line advertises
     `{server port}.{apex}`, and proxying that to ourselves would arrive with
     the same Host and match again, forever.
   - **The inbound Host is passed through unrewritten**, so the upstream sees
     exactly what tunneld would have delivered and per-port behaviour cannot
     drift between the two modes.
   - **Open question 1 answered, and it needed a frontend change.** The
     subdomain URL builders dropped the port
     (`https://23000.host`), which is right behind a tunnel on 443 and wrong
     on a wildcard box serving its own subdomains on :1977. Added
     `buildSubdomainOrigin` in `static/modules/url-builder.js`, routed the
     preview/agent-chat/files builders and the VNC URL through it, and it is
     a no-op in tunnel mode (`location.port` is empty there). Open question 2
     (whether `--public-hostname` should imply `SWE_PREVIEW_REACH_DOMAIN`) is
     still open and belongs to phase 3.
3. **Live proof.** Boot dockerless with `--public-hostname` on a wildcard that
   resolves here, log in once, and confirm the preview and Agent View panes
   load on their own subdomains with no second login -- the exact thing that
   bounced in ADR-0043's sidecar.
4. **Docs.** `docs/dockerless.md` (a third option beside co-located and tunnel),
   `docs/tunnel-explained.md` (when you do NOT need a tunnel), and an ADR
   recording that ADR-0043's rejection was re-examined and why this shape
   differs. CHANGELOG: one sentence.

## Open questions

1. Does the frontend's reach probe need to learn about this mode, or does
   setting `publicHostname` put it on the tunnel path already? Read
   `setPreviewURL` before phase 2.
2. Should `--public-hostname` also imply the preview reach domain
   (`SWE_PREVIEW_REACH_DOMAIN`)? Probably yes -- they are the same wildcard --
   but confirm the vhost-suffix rewrite still behaves.
