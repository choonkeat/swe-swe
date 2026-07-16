# Tunnel mode explained

For copy-paste runbooks, pick your target:

- [tunnel-laptop.md](tunnel-laptop.md) -- run on your laptop / local docker
- [tunnel-fly.md](tunnel-fly.md) -- deploy to Fly.io

This document covers the *why*: what tunnel mode is, why it has to be
a build-time choice, what the env vars mean, how to read the
verification logs, and what to do when something breaks.

## What tunnel mode is

Tunnel mode lets a swe-swe container be reachable from the public
internet without owning a public IP, opening ports, configuring DNS,
or provisioning a TLS cert. The tunnel client inside the container
dials a `swe-swe-tunnel` server outbound; that server fronts traffic
to your container over the same connection.

The container can sit on a residential network, a PaaS (Fly.io,
Railway, Render, Cloud Run), or anywhere with outbound HTTPS. There
is nothing for an attacker on the public internet to scan or
fingerprint at the container side. The only thing facing the internet
is the tunnel server, which is run by your `swe-swe-tunnel` admin and
authenticates clients by Ed25519 pubkey.

For background, see `tasks/2026-04-29-tunnel-subprocess-pivot.md`
(swe-swe side) and the `swe-swe-tunnel` repo for the wire protocol.

## Two sides: client (this doc) vs server (the admin)

There are two halves, and these docs cover only the **client** half --
the swe-swe container that dials out:

- **Client (you):** the swe-swe container, built with
  `init --tunnel-server-url=...`. Generating an identity, getting your
  pubkey authorized, and the run/verify steps are all in the runbooks
  ([tunnel-laptop.md](tunnel-laptop.md), [tunnel-fly.md](tunnel-fly.md),
  [tunnel-paas.md](tunnel-paas.md)).
- **Server (the admin):** the public-facing `swe-swe-tunnel` daemon
  (`tunneld`) that terminates TLS, authorizes client pubkeys, optionally
  enforces mTLS, and demuxes `{port}.{unique}-tunnel.<suffix>` to the
  right session. **Running the server is out of scope here** -- see the
  [`swe-swe-tunnel` repo](https://github.com/choonkeat/swe-swe-tunnel)
  for how to stand up `tunneld`, authorize pubkeys, and enable
  `--mtls-ca`. If you are self-hosting both ends (e.g. testing locally),
  start there for the server, then return to the runbooks for the client.

## Why it requires `init --tunnel-server-url` (not a sidecar)

A common shortcut -- running plain `swe-swe up` (compose mode with
Traefik) and pointing an external `swe-swe-tunnel` client at it --
**does not work**. The cookie domain, per-port iframe auth, frontend
subdomain URLs, and Traefik routing all key off a public hostname
populated only by the in-process tunnel supervisor that
`init --tunnel-server-url` bakes into the image. Without it, login
appears to succeed but the UI bounces you back to the login screen as
soon as the per-port iframes try to load.

See [ADR-0043](adr/0043-tunnel-mode-not-a-sidecar.md) for the full
failure mode (six distinct breaks, in order of how soon they bite).
The short version: tunnel mode is a build-time choice, not a
runtime sidecar.

If you set `SWE_TUNNEL_*` env vars but your image was built with
plain `swe-swe init` (no `--tunnel-server-url`), the env vars are
read by nothing -- the image has no tunnel client. The container
boots normally with no `tunnel-supervisor` log lines at all. That is
the most common "why isn't it working" cause; check the logs first
(see "Verifying the tunnel came up" below).

## The env var bundle

```
SWE_TUNNEL_SERVER_URL=https://tunnel.example.com           # required to enter tunnel mode
SWE_TUNNEL_UNIQUE=<short label, e.g. myproject123>         # REQUIRED -- public hostname: <unique>-tunnel.<server-suffix>
SWE_TUNNEL_IDENTITY_KEY=<base64-PEM>                       # OPTIONAL, SECRET -- omit to auto-generate on first boot; do not log
SWE_BIND=127.0.0.1:1977                                    # tunnel mode: keep swe-swe-server off public interfaces
PORT=<8080 or PaaS-assigned>                               # landing/health server binds this
SWE_SWE_PASSWORD=<strong password>                         # SECRET -- swe-swe auth (still required behind the tunnel)
```

`SWE_TUNNEL_IDENTITY_KEY` and `SWE_TUNNEL_UNIQUE` are a **bound
pair**: re-deploys with the same pair keep the same public hostname;
a mismatched pair gets `Deny{key_mismatch, kind=fatal}` and the
supervisor stops with no retry. Save them somewhere safe.

### Setting the unique label: prefer `--tunnel-unique`

You do not have to manage `SWE_TUNNEL_UNIQUE` as a raw runtime env var.
Pass the label at build time and it is baked into the generated
compose:

```sh
swe-swe init \
  --tunnel-server-url=https://tunnel.example.com \
  --tunnel-unique=myproject123
```

The generated compose emits

```
SWE_TUNNEL_UNIQUE=${SWE_TUNNEL_UNIQUE:-myproject123}
```

so the init-time value is the default **and** a runtime
`SWE_TUNNEL_UNIQUE` env var still overrides it if present -- existing
env-based deployments keep working unchanged. If you set neither, the
tunnel refuses to start (see "`SWE_TUNNEL_UNIQUE` is required" below).

### Why `SWE_BIND=127.0.0.1:1977`

Internally, swe-swe-server binds `127.0.0.1:1977` (the `SWE_PORT`
default; was `9898` before commit `34c9cff61`) and the tunnel client
dials it from inside the same container. The image built by
`swe-swe init --tunnel-server-url=...` already wires this default in
its `CMD` line; setting `SWE_BIND=127.0.0.1:1977` explicitly is
belt-and-suspenders against an image baked from an older revision.

The reason to keep it nailed to localhost: anything else in the
container's network namespace (sidecars, internal cluster mesh,
accidentally-routed `$PORT=1977`) reaching `swe-swe-server` directly
would bypass the tunnel's identity gate.

### Why `$PORT` exists

The container exposes one port to the host / PaaS: `$PORT`. That port
serves the landing page (a small static doc with the live tunnel
hostname linked through). A health probe should hit `GET /` on
`$PORT`; a 200 means the container is up and the landing render
succeeded.

## The identity model

Tunnel mode authenticates your container to the tunnel server with an
Ed25519 keypair. The pubkey half must be authorized (allowlisted) by
the `swe-swe-tunnel` admin before your first connection will be
accepted -- this is a wait-on-human step, do it early.

### Default: let the container generate the key (zero setup)

Set `SWE_TUNNEL_SERVER_URL` and `SWE_TUNNEL_UNIQUE`, leave
`SWE_TUNNEL_IDENTITY_KEY` unset, and start swe-swe. On the **first
boot** with no key on disk, the tunnel client:

1. generates an Ed25519 key at `~/.swe-swe-tunnel/identity.key`,
2. prints the public key and that path to the logs, and
3. **stops the tunnel** (it does not try to connect) so it never
   burns a rate-limited registration attempt with a pubkey the server
   has never seen. The rest of swe-swe keeps running.

The log line to look for:

```
Generated a new tunnel identity key.
  path:   /home/app/.swe-swe-tunnel/identity.key
  pubkey: uLClVZEOZsI8F+SSU8TZUxZL4tUtne+Ow9Ofv4BXRrI
```

Send that pubkey to the admin to allowlist, then **restart**. The
second boot finds the key on disk and connects normally.

Because the key lives at `~/.swe-swe-tunnel/identity.key`, this path
assumes that directory **persists across restarts** (a persistent
volume). On ephemeral-disk PaaS, either mount a volume or switch to
the bring-your-own path below so you are not regenerating (and
re-allowlisting) on every boot.

### Optional: bring your own key

Already have a key (or want it off any disk)? Supply it and the
first-boot generate/stop step never fires -- the client uses yours:

- `SWE_TUNNEL_IDENTITY_KEY=<base64 PKCS8 PEM>` -- inline, never touches
  disk (`base64 -w0 < identity.key`). Best for ephemeral-disk PaaS.
- or `--identity-key <path>` (env `SWE_TUNNEL_KEY`) to point at an
  existing key file / mount.

Precedence inside the container: `SWE_TUNNEL_IDENTITY_KEY` (inline)
beats the on-disk file at `~/.swe-swe-tunnel/identity.key`. If the
inline env var is set but malformed, the client errors out instead of
silently falling back to the file (which would burn a fresh `unique`
on the tunnel server and confuse the operator). The runbooks have the
exact `openssl` + `base64 -w0` commands. See
`internal/tunnelclient/identity.go:LoadIdentity` in the
`swe-swe-tunnel` repo.

### `SWE_TUNNEL_UNIQUE` is required

Tunnel mode will not start without a unique label. If
`SWE_TUNNEL_SERVER_URL` is set but the label resolves to empty (no
`--tunnel-unique` baked in and no runtime `SWE_TUNNEL_UNIQUE`),
swe-swe-server refuses to launch the tunnel and logs a
`unique_required` fatal tunnel status (the rest of swe-swe still
runs). This replaces the older failure mode where an empty unique made
the tunnel child exit and the supervisor restart-loop forever. Set it
with `--tunnel-unique` at init (recommended) or `SWE_TUNNEL_UNIQUE` at
runtime.

## mTLS (when the tunnel server requires client certificates)

By default the tunnel server authenticates clients by Ed25519 pubkey
alone (the identity model above). A hardened server can *additionally*
require a TLS client certificate -- the admin runs `tunneld` with
`--mtls-ca <ca.pem>`, and any client that doesn't present a cert signed
by that CA is refused at the TLS layer, before identity auth.

**When you need it:** only when your admin tells you the server enforces
`--mtls-ca`. Against a server without it, a client cert is simply
ignored, so there is no harm in omitting the flag unless asked.

**How to enable it (client side):** pass `--tunnel-client-cert` at init,
alongside `--tunnel-server-url`:

```sh
swe-swe init \
  --tunnel-server-url=https://tunnel.example.com \
  --tunnel-client-cert=/path/to/client.crt
```

What this wires into the generated compose:

- The cert path is bind-mounted **read-only** into the container at
  `/home/app/.swe-swe-tunnel/client.crt`.
- `SWE_TUNNEL_CLIENT_CERT=/home/app/.swe-swe-tunnel/client.crt` is set so
  the tunnel client presents it during the TLS handshake.
- **No private key is mounted.** The cert's key half is your existing
  identity key (`SWE_TUNNEL_IDENTITY_KEY` / `~/.swe-swe-tunnel/identity.key`).
  So the client cert must be an X.509 cert issued for *that same*
  Ed25519 identity keypair, signed by the server's mTLS CA.

**Getting the cert:** the admin (who holds the mTLS CA) signs a cert for
the pubkey you already sent them for identity authorization, and hands
back the `.crt`. It is not secret on its own (the private key never
leaves you), so it can travel over normal channels.

## Verifying the tunnel came up

Look at the container logs for, in order:

```
[tunnel-supervisor] event kind=starting
[tunnel-supervisor] event kind=connecting attempt=1
[tunnel-client] identity loaded source=env fingerprint=ab12cd34ef56
[tunnel-client] registered hostname=myproject123-tunnel.example.com
[tunnel-supervisor] OPEN AT https://1977.myproject123-tunnel.example.com/
```

`OPEN AT` is the operator-friendly URL. The same hostname is also
broadcast on the WS status frame as `tunnelStatus.publicHostname` and
rendered on the landing page (`$PORT`), which updates per request so
it picks up label rotations live.

The `identity loaded` fingerprint should be **stable across
re-deploys** of the same `SWE_TUNNEL_IDENTITY_KEY`. The fingerprint
is `sha256(pubkey)[:6]` hex -- the same value the admin sees in their
`register ok` / `register denied` log lines, so quoting it is a fast
way to ask "did my registration land?"

`source=env` confirms the env var was read. `source=file` means the
client fell back to the on-disk identity (`SWE_TUNNEL_IDENTITY_KEY`
not exported, blank, or stripped by compose).

## Operating

**Re-deploy.** Keep `SWE_TUNNEL_IDENTITY_KEY` and `SWE_TUNNEL_UNIQUE`
identical and the public hostname is unchanged. Old container exits
-> tunnel server closes its session -> new container connects and
re-binds. There is a short outage window during the swap; for
zero-downtime, run two containers behind separate uniques and swap
DNS / front-end routing.

**Key rotation.** Generate a new identity, send the new pubkey to the
admin, wait for them to authorize it, set both
`SWE_TUNNEL_IDENTITY_KEY` and a new `SWE_TUNNEL_UNIQUE`, redeploy.
The old `unique`/key binding on the tunnel server is leaked until the
admin GCs it.

**Migrating between PaaS providers.** Copy `SWE_TUNNEL_IDENTITY_KEY`
and `SWE_TUNNEL_UNIQUE` to the new provider's secrets, deploy,
decommission the old. Same hostname, no DNS work needed, no need to
re-authorize with the admin.

## Reaching the container directly (`--tunnel-local-ports`)

By default tunnel mode publishes **no host ports**: swe-swe-server binds
`127.0.0.1:1977` *inside* the container and the only way in is through
the tunnel. That is the right default for a PaaS/remote box, but it
means that on the machine actually running the container (typically your
laptop, `swe-swe up`) you cannot `curl localhost:1977` or open a preview
port directly -- and if the tunnel hostname is hard to reach from where
you sit, the UI's preview/agent-chat iframes (which target
`{port}.{publicHostname}`) break.

`init --tunnel-local-ports` (alongside `--tunnel-server-url`) widens the
bind to all interfaces and publishes, **on the host's `127.0.0.1` only**,
`SWE_PORT` plus the preview / agent-chat / vnc / public ranges. The
tunnel is unaffected (the client still dials `127.0.0.1` internally), and
nothing is exposed beyond the host's own loopback. The frontend also
picks its URL mode by how the page was loaded, so over `localhost` it
uses the reachable port-based / same-origin URLs instead of the tunnel
subdomain. Full steps + the published-port table are in
[tunnel-laptop.md](tunnel-laptop.md#optional-reach-the-containers-directly-from-your-laptop).

This is primarily a **local/laptop** convenience. On a PaaS/Fly deploy
it has no benefit (the host loopback isn't something you can reach), so
leave it off there.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Container boots but no `tunnel-supervisor` lines at all | Image was built without `--tunnel-server-url`. Tunnel mode is a build-time choice -- re-run `swe-swe init --tunnel-server-url=...` and rebuild. The `SWE_TUNNEL_*` env vars are inert without a tunnel-mode image. |
| `identity loaded source=file` instead of `source=env` | `SWE_TUNNEL_IDENTITY_KEY` not actually exported into the container. In compose mode, check the generated `docker-compose.yml`'s `environment:` block forwards it. The client falls back to file-based auto-gen, which burns a fresh `unique` each time. |
| `kind=fatal reason=not_authorized` | Your pubkey isn't authorized on the tunnel server. Re-extract the pubkey (see runbook step 1) and resend to your admin. |
| `kind=fatal reason=key_mismatch` | `SWE_TUNNEL_IDENTITY_KEY` doesn't match the key originally bound to `SWE_TUNNEL_UNIQUE` on the tunnel server. Either change the unique, or restore the original key. The supervisor stops on fatal; container will idle without a tunnel. |
| `kind=fatal reason=signature invalid` | Your `SWE_TUNNEL_IDENTITY_KEY` is corrupt or doesn't match the pubkey on file. Regenerate per runbook step 1, resend pubkey to admin. |
| `SWE_TUNNEL_IDENTITY_KEY: parse PKCS8: ...` or `identity is ed25519.PublicKey, want ed25519.PrivateKey` at startup | You base64'd the **public** key half. The env var wants the private key (`-----BEGIN PRIVATE KEY-----`). Verify with `head -1 identity.key`. |
| `kind=reconnecting retryAfterMs=300000` | Rate-limited by the tunnel server (typically pubkey churn). The frontend banner shows a countdown; wait it out. |
| Landing 200 but no `OPEN AT` line | Tunnel server unreachable or DNS not yet resolving. Check `SWE_TUNNEL_SERVER_URL` and outbound egress. |
| Identity fingerprint changes between deploys | `SWE_TUNNEL_IDENTITY_KEY` not actually set, or set with embedded newlines (use `base64 -w0`). The client falls back to file-based auto-gen each time. |

If your session was working and suddenly drops with `not_authorized`
on reconnect, your admin has revoked your pubkey. Talk to them.

## Known limitations

(none currently tracked here -- `swe-swe tunnel-identity create` is
the next planned ergonomic upstream change; until it lands, the
`openssl` recipe in the runbooks is the manual path.)
