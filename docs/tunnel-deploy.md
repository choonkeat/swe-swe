# Deploying swe-swe with tunnel mode

## What this is

Tunnel mode lets a swe-swe container be reachable from the public
internet without owning a public IP, opening ports, configuring DNS,
or provisioning a TLS cert. The tunnel client inside the container
dials a `swe-swe-tunnel` server outbound; that server fronts traffic
to your container over the same connection.

The container can sit on a residential network, a PaaS (Fly.io,
Railway, Render, Cloud Run), or anywhere with outbound HTTPS — there
is nothing for an attacker on the public internet to scan or
fingerprint at the container side. The only thing facing the internet
is the tunnel server, which is run by your `swe-swe-tunnel` admin and
authenticates clients by Ed25519 pubkey.

For background, see `tasks/2026-04-29-tunnel-subprocess-pivot.md`
(swe-swe side) and the `swe-swe-tunnel` repo for the wire protocol.

## What you need

- A `swe-swe-tunnel` admin who runs a reachable server
  (e.g. `https://tunnel.example.com`) and can authorize your pubkey.
- Docker on your workstation to build the image.
- A PaaS account that can run a Linux container, accept env vars /
  secrets, and route a single HTTP port to the container.

## Build the image

From the host repo where you want swe-swe scaffolded:

```sh
cd /your/project/repo
swe-swe init --tunnel-server-url=https://tunnel.example.com --metadata-dir=./.swe-swe-tunnel
docker build -t my-org/swe-swe:tunnel ./.swe-swe-tunnel/
```

`--tunnel-server-url` flips the Dockerfile into tunnel mode: it
builds the `swe-swe-tunnel` client into the image and the
`swe-swe-server` supervisor exec's it on startup. The tunnel ref is
pinned via the `SWE_SWE_TUNNEL_REF` build arg — bump that to pick up
upstream tunnel changes.

`--metadata-dir=./.swe-swe-tunnel` keeps the generated `Dockerfile`
and supporting files inside the project (the default puts them under
`$HOME/.swe-swe/projects/<slug>/`, which is awkward to point `docker
build` at). The directory itself is the build context.

Push the tagged image to the registry your PaaS reads from.

## Deploy: env vars and ports

Tunnel mode authenticates your container to the tunnel server with an
Ed25519 keypair. On a PaaS you do **not** want this on a persistent
volume: deliver it as a secret env var instead. The pubkey half of
the keypair must be authorized by the `swe-swe-tunnel` admin before
your first connection will be accepted, so do this part early — it's
a wait-on-human step.

```sh
# 1. Generate identity locally, one-time. base64 the PEM as the env var
#    you'll paste into the PaaS. SWE_TUNNEL_IDENTITY_KEY is the secret —
#    treat it like an SSH private key.
openssl genpkey -algorithm Ed25519 -out identity.key
SWE_TUNNEL_IDENTITY_KEY=$(base64 -w0 < identity.key)
echo "$SWE_TUNNEL_IDENTITY_KEY"

# 2. Extract the matching pubkey (base64 RawStd, 32 bytes) and send the
#    one-line output to your swe-swe-tunnel admin out-of-band (chat,
#    email, ticket). They authorize it before your first deploy.
openssl pkey -in identity.key -pubout -outform DER | tail -c 32 | base64 -w0 | tr -d '='

# 3. Wipe the local key file. SWE_TUNNEL_IDENTITY_KEY has the only
#    copy you need; the PaaS holds it as a secret.
rm identity.key
```

Set these env vars on the PaaS (mark the SECRET ones as secrets):

```sh
SWE_TUNNEL_SERVER_URL=https://tunnel.example.com           # required to enter tunnel mode
SWE_TUNNEL_IDENTITY_KEY=<base64-PEM from step 1>           # SECRET — do not log
SWE_TUNNEL_UNIQUE=<short label, e.g. myproject123>         # public hostname: <unique>-tunnel.<server-suffix>
SWE_BIND=127.0.0.1:1977                                    # tunnel mode: keep swe-swe-server off public interfaces
PORT=<PaaS-assigned, e.g. 8080>                            # landing/health server binds this
SWE_SWE_PASSWORD=<strong password, e.g. correct-horse-battery-staple>   # SECRET — swe-swe auth (still required behind the tunnel)
```

Save `SWE_TUNNEL_IDENTITY_KEY` and `SWE_TUNNEL_UNIQUE` somewhere
safe. Re-deploys with the same pair keep the same public hostname.
**Mismatched pair → fatal `key_mismatch`** (the supervisor stops, no
retry); see Troubleshooting.

Container exposes one port to the PaaS: `$PORT`. That port serves the
landing page (a small static doc with the live tunnel hostname linked
through). The PaaS health probe should hit `GET /` on `$PORT`; a 200
means the container is up and the landing render succeeded.

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

## Verify the tunnel came up

Check the container logs after the first deploy. You're looking for,
in order:

```
[tunnel-supervisor] event kind=starting
[tunnel-supervisor] event kind=connecting attempt=1
[tunnel-client] identity loaded source=env fingerprint=ab12cd34ef56
[tunnel-client] registered hostname=myproject123-tunnel.example.com
[tunnel-supervisor] OPEN AT https://1977.myproject123-tunnel.example.com/
```

The `OPEN AT` line is the operator-friendly URL. The same hostname is
also broadcast on the WS status frame as `tunnelStatus.publicHostname`
and rendered on the landing page (`$PORT`), which updates per request
so it picks up label rotations live.

The `identity loaded` fingerprint should be stable across re-deploys
of the same `SWE_TUNNEL_IDENTITY_KEY`. Compare across deploys to
confirm no identity drift. The fingerprint is `sha256(pubkey)[:6]`
hex — the same value the admin sees in their `register ok` /
`register denied` log lines, so quoting it is a fast way to ask
"did my registration land?"

## Operating

**Re-deploy.** Keep `SWE_TUNNEL_IDENTITY_KEY` and `SWE_TUNNEL_UNIQUE`
identical and the public hostname is unchanged. Old container
exits → tunnel server closes its session → new container connects
and re-binds. There is a short outage window during the swap; for
zero-downtime, run two containers behind separate uniques and swap
DNS / front-end routing.

**Key rotation.** Generate a new identity (steps 1-2 above), send the
new pubkey to the admin, wait for them to authorize it, set both
`SWE_TUNNEL_IDENTITY_KEY` and a new `SWE_TUNNEL_UNIQUE`, redeploy.
The old `unique`/key binding on the tunnel server is leaked until
the admin GCs it.

**Migrating between PaaS providers.** Copy `SWE_TUNNEL_IDENTITY_KEY`
and `SWE_TUNNEL_UNIQUE` to the new provider's secrets, deploy,
decommission the old. Same hostname, no DNS work needed, no need to
re-authorize with the admin.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `kind=fatal reason=not_authorized` | Your pubkey isn't authorized on the tunnel server. Re-run the pubkey-extract command (Deploy step 2) and resend to your admin. |
| `kind=fatal reason=key_mismatch` | `SWE_TUNNEL_IDENTITY_KEY` doesn't match the key originally bound to `SWE_TUNNEL_UNIQUE` on the tunnel server. Either change the unique, or restore the original key. The supervisor stops on fatal; container will idle without a tunnel. |
| `kind=fatal reason=signature invalid` | Your `SWE_TUNNEL_IDENTITY_KEY` is corrupt or doesn't match the pubkey on file. Regenerate per Deploy step 1, resend pubkey to admin. |
| `kind=reconnecting retryAfterMs=300000` | Rate-limited by the tunnel server (typically pubkey churn). The frontend banner shows a countdown; wait it out. |
| Landing 200 but no `OPEN AT` line | Tunnel server unreachable or DNS not yet resolving. Check `SWE_TUNNEL_SERVER_URL` and outbound egress. |
| Identity fingerprint changes between deploys | `SWE_TUNNEL_IDENTITY_KEY` not actually set, or set with embedded newlines (use `base64 -w0`). The client falls back to file-based auto-gen each time. |

If your session was working and suddenly drops with `not_authorized`
on reconnect, your admin has revoked your pubkey. Talk to them.

## Fly.io walkthrough

Concrete copy-paste version. Adapt names as needed.

`fly.toml` (minimal):

```toml
app = "my-swe-swe"
primary_region = "iad"

[build]
  image = "registry.fly.io/my-swe-swe:tunnel"

[env]
  SWE_TUNNEL_SERVER_URL = "https://tunnel.example.com"
  SWE_TUNNEL_UNIQUE = "myproject123"
  SWE_BIND = "127.0.0.1:1977"

[http_service]
  internal_port = 8080
  force_https = true
  auto_stop_machines = false
  auto_start_machines = true

  [[http_service.checks]]
    grace_period = "10s"
    interval = "30s"
    method = "GET"
    timeout = "5s"
    path = "/"
```

Provision and deploy:

```sh
fly apps create my-swe-swe
fly secrets set \
    SWE_TUNNEL_IDENTITY_KEY="$(cat identity.key.b64)" \
    SWE_SWE_PASSWORD='choose-a-strong-one'
fly deploy --image my-org/swe-swe:tunnel
fly logs | grep -E 'OPEN AT|identity loaded'
```

Notes:

- `internal_port = 8080` matches `$PORT` Fly injects (8080 by
  default). The container's landing server binds `:$PORT`.
- `[http_service.checks].path = "/"` hits the landing render. It
  returns 200 even when the tunnel is reconnecting (status banner
  reflects state separately) so health stays green during transient
  reconnects.
- Do **not** add `[[services.ports]]` for 1977. It's an internal
  port; the tunnel handles ingress.

## Known limitations

(none currently tracked here — `swe-swe tunnel-identity create` is
the next planned ergonomic upstream change; until it lands, the
`openssl` recipe in the Deploy section is the manual path.)
