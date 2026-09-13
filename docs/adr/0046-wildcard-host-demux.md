# ADR-0046: Wildcard host-demux (one port, no tunnel)

**Status**: Accepted
**Date**: 2026-09-12
**Research**: [ADR-0043](0043-tunnel-mode-not-a-sidecar.md), [ADR-0045](0045-preview-host-demux.md), [tasks/2026-09-09-wildcard-host-demux.md](../../tasks/2026-09-09-wildcard-host-demux.md)

## Context

A box reachable on exactly one port was told to run a tunnel. That is a real
cost: `swe-swe-tunnel` needs a server somewhere that stays up, a domain whose
subdomains point at it, and your key authorized on it.

But a box whose own `*.example.com` DNS already points at it has everything the
tunnel would have supplied -- wildcard subdomains -- and was still being sent
down the tunnel path.

The addressing for this already existed and was exercised daily in tunnel mode.
All of it hangs off ONE value, `liveTunnelHostname`, which only the tunnel
supervisor ever set on `register_ok`:

- `resolveCookieDomain` emits `Domain=<apex>`, so one login covers every
  per-port subdomain.
- `buildSubdomainPreviewUrl` / `buildSubdomainAgentChatUrl` /
  `buildSubdomainFilesUrl` / `getBrowserViewUrl` branch on
  `tunnelStatus.publicHostname`.
- `requireAuthCookie`'s 401-not-302 contract for cross-origin iframes assumes
  that cookie domain.

So the missing piece was never the addressing. It was the thing that RECEIVES
`23000.example.com` and forwards it to `127.0.0.1:23000`. In tunnel mode the
tunnel server does that job. Here swe-swe-server does it on its own listener.

### ADR-0043 said no to this once

ADR-0043 rejected a `SWE_PUBLIC_HOSTNAME` env fallback as "a partial fix
masquerading as a full one". That rejection stands **for what it was
rejecting**: bolting an external tunnel onto a running swe-swe as a sidecar,
where it fixed break 1 and left 2, 5 and 6 broken.

This shape is different in one decisive way: the demuxer lives inside
swe-swe-server, so the subdomain URLs the frontend builds are URLs this server
itself answers.

| # | ADR-0043's break | Status here |
|---|---|---|
| 1 | Cookie `Domain` empty -> login bounce | Fixed by setting the same value |
| 2 | Frontend builds URLs the wildcard cannot route | Fixed: our own demuxer serves them |
| 3 | Per-port iframe 401 forever | Follows from 1 |
| 4 | Landing page has no usable URL | Follows from 1 |
| 5 | Traefik `Host()` rules don't match wildcards; TLS pinned to one domain | **Half false -- see below** |
| 6 | Label rotation invalidates sessions | Moot: a static wildcard domain never rotates |

Break 5 was tested rather than believed, on 2026-09-12:

- **The Host() half is false.** Every router rule swe-swe generates is
  `PathPrefix` (or `Path`) plus an entrypoint. There is no `Host()` matcher
  anywhere -- not in the compose labels, not in `traefik-dynamic.yml`, not in
  the ~300 per-session routers `templates.go` emits. Verified against a real
  `traefik:v2.11` wired with that exact rule shape: `23000.example.com`,
  `a.b.23000.example.com` and even `23000.evil.com` were all forwarded to the
  backend with the `Host` header intact.
- **The default compose setup has no Traefik at all.** `ssl=no` with no tunnel
  is Dockerfile-only: one container, 38 lines of compose, nothing in front.
- **The TLS half is true.** A TLS-terminating proxy in front holds a
  certificate for one exact name, so a padlocked `{port}.{apex}` fails the
  certificate check until a wildcard certificate is installed there.

## Decision

### 1. The setting

`-public-hostname=example.com` (env `SWE_PUBLIC_HOSTNAME`) sets the same atomic
the tunnel supervisor sets, so every consumer above lights up unchanged. It
works in **every runtime**; the TLS caveat is a log line, not a refusal.

Mutually exclusive with `-tunnel-server-url`, because both own that value and
the last writer would silently win. Which one gives way follows one rule: an
explicitly passed flag beats a value merely inherited from the environment,
either direction, with a log line naming the loser. Both explicit, or both
inherited, is a hard error. Boxes really do carry a stale
`SWE_TUNNEL_SERVER_URL`, and `swe-swe up --public-hostname=...` there must mean
what it says.

The flag name has been used before: an earlier `--public-hostname` was dropped
in favour of the tunnel's subprocess event stream (ADR-0042), because it was a
boot-time guess at what the tunnel would assign. This one is not a guess about
the tunnel; it is an assertion about DNS the operator controls, and it is
mutually exclusive with tunnel mode.

### 2. The demux

In front of the main handler, **before auth**:

- Match the request Host against `<all digits>.<configured apex>`, exactly.
  Anything else falls through to today's handler untouched, so a box reached by
  IP or by the bare domain behaves exactly as it does now.
- A request for the server's **own** port also falls through. It has to: the
  landing line advertises `{server port}.{apex}`, and proxying that to
  ourselves would arrive with the same Host and match again, forever.
- The inbound Host is passed through **unrewritten**, so the upstream sees
  exactly what tunneld would have delivered and per-port behaviour cannot drift
  between the two modes.

Before auth is safe for the same reason it is safe under tunneld: every
per-port destination wraps itself in `requireAuthCookie`.

### 3. The allowlist

A public hostname that forwards to any `127.0.0.1:<n>` is a door into every
service on the box, swe-swe's own broker included. Only the per-session proxy
bands are routable -- `proxyPortOffset` plus each configured range -- derived
at call time from the live variables so they move with `--preview-ports` and
`--proxy-port-offset`. Anything else is a 404, logged once per distinct port.

The raw target ports behind those proxies are deliberately NOT routable:
reaching an app means reaching its proxy, which is the thing that checks the
login cookie.

### 4. The port survives in the frontend

`buildSubdomainOrigin` carries `location.port` into every subdomain URL. Behind
a tunnel the page is served on 443, so `location.port` is empty and nothing
changes; on a wildcard box swe-swe serves its own subdomains on its own
listener (`:1977`), and dropping the port would send the browser to `:80`.

## Consequences

**Good:**

- A wildcard box needs no tunnel: no server to run, no key to authorize, no
  once-per-boot browser steps.
- Under compose it also collapses ~100 published ports into one.
- Nothing changes for anyone who does not set it: the demuxer is a passthrough
  when the apex is empty.

**Bad:**

- Plain http only, unless whatever terminates TLS in front holds a wildcard
  certificate. swe-swe-server never terminates TLS itself.
- The apex is fixed at boot. Changing it is a restart.
- The allowlist has to be kept in step with any new per-session port band. It
  derives from the live range variables, so adding a band means adding it to
  `publicHostnameRoutablePorts` too.

## Verification

Both runtimes, driven rather than reasoned about:

- **Host-native** (2026-09-10): a real chromium against `-public-hostname=lvh.me`.
  One login at `19771.lvh.me:19771` issued `Domain=.lvh.me`; with only that
  cookie, `23000.lvh.me:19771` served a real app through the session's preview
  proxy and `29000.lvh.me:19771` served the Files pane, neither asking to log in
  again. From a context with no cookie, both returned 401.
- **Compose + Traefik** (2026-09-12): `make e2e-up-compose` with
  `SWE_PUBLIC_HOSTNAME=lvh.me`. Login at `9770.lvh.me` issued `Domain=lvh.me`;
  `23100.lvh.me` reached the demuxer and dialled `127.0.0.1:23100`; `22.lvh.me`
  and a raw app port were refused with one log line each; a foreign domain fell
  through to the UI. The refusal log named the e2e stack's own bands
  (`preview 23100-23129, ...`), confirming the allowlist follows
  `--preview-ports`.

That run also found the setting was never passed into the container: the
generated compose now carries `SWE_PUBLIC_HOSTNAME` through, empty by default.
