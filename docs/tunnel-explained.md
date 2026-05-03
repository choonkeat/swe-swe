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
SWE_TUNNEL_IDENTITY_KEY=<base64-PEM>                       # SECRET -- do not log
SWE_TUNNEL_UNIQUE=<short label, e.g. myproject123>         # public hostname: <unique>-tunnel.<server-suffix>
SWE_BIND=127.0.0.1:1977                                    # tunnel mode: keep swe-swe-server off public interfaces
PORT=<8080 or PaaS-assigned>                               # landing/health server binds this
SWE_SWE_PASSWORD=<strong password>                         # SECRET -- swe-swe auth (still required behind the tunnel)
```

`SWE_TUNNEL_IDENTITY_KEY` and `SWE_TUNNEL_UNIQUE` are a **bound
pair**: re-deploys with the same pair keep the same public hostname;
a mismatched pair gets `Deny{key_mismatch, kind=fatal}` and the
supervisor stops with no retry. Save them somewhere safe.

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
Ed25519 keypair. The pubkey half must be authorized by the
`swe-swe-tunnel` admin before your first connection will be accepted
-- this is a wait-on-human step, do it early.

The private key is delivered as `SWE_TUNNEL_IDENTITY_KEY` (PaaS
secret or shell env var), base64-encoded PKCS8 PEM (i.e.
`base64 -w0 < identity.key`). On a PaaS you do **not** want this on a
persistent volume -- that's why env-var delivery exists. The runbooks
have the exact `openssl` + `base64 -w0` commands.

Precedence inside the container: `SWE_TUNNEL_IDENTITY_KEY` env var
beats the on-disk file at `~/.swe-swe-tunnel/identity.key`. If the
env var is set but malformed, the client errors out instead of
silently falling back to the file (which would burn a fresh `unique`
on the tunnel server and confuse the operator). See
`internal/tunnelclient/identity.go:LoadIdentity` in the
`swe-swe-tunnel` repo.

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
