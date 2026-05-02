# Deploying swe-swe with tunnel mode

This guide covers deploying a swe-swe container to a PaaS (Fly.io,
Railway, Render, Cloud Run, etc.) using **tunnel mode** so the
container is reachable from the public internet without owning a
public IP, opening ports, configuring DNS, or provisioning a TLS
cert. The tunnel client dials a tunneld server outbound; the tunneld
server fronts traffic to the container over the same connection.

For background, see `tasks/2026-04-29-tunnel-subprocess-pivot.md`
(swe-swe side) and the swe-swe-tunnel repo for the wire protocol.

## What you need

- A reachable tunneld server (e.g. `https://tunnel.example.com`).
  This guide assumes it exists and accepts new clients.
- Docker on your workstation to build the image.
- A PaaS account that can run a Linux container, accept env vars /
  secrets, and route a single HTTP port to the container.

## One-time identity bootstrap

The tunnel client authenticates to tunneld with an Ed25519 keypair.
On a PaaS you do **not** want this on a persistent volume: deliver it
as a secret env var instead.

Generate a key locally once with `openssl`:

```sh
openssl genpkey -algorithm Ed25519 -out identity.key
SWE_TUNNEL_IDENTITY_KEY=$(base64 -w0 < identity.key)
echo "$SWE_TUNNEL_IDENTITY_KEY"
rm identity.key
```

(A consumer-side `swe-swe tunnel-identity create` subcommand that
does this end-to-end is planned but not yet shipped — see "Known
limitations".)

Save `SWE_TUNNEL_IDENTITY_KEY` and the matching `SWE_TUNNEL_UNIQUE`
(any short label, e.g. `acme-prod`) somewhere safe; you'll paste them
into the PaaS as secrets. Re-deploys with the same pair keep the same
public hostname. **Mismatched pair → fatal `key_mismatch`** (the
supervisor stops, no retry); see "Troubleshooting" below.

## Build the image

From the host repo where you want swe-swe scaffolded:

```sh
swe-swe init --tunnel-server-url=https://tunnel.example.com
docker build -t my-org/swe-swe:tunnel ./.swe-swe/projects/<project>/
```

The `--tunnel-server-url` flag flips the Dockerfile into tunnel mode:
it builds the `swe-swe-tunnel` client into the image and the
`swe-swe-server` supervisor exec's it on startup. The tunnel ref is
pinned via the `SWE_SWE_TUNNEL_REF` build arg — bump that to pick up
upstream tunnel changes.

Push the tagged image to the registry your PaaS reads from.

## Deploy: env vars and ports

| Var                       | Value                                  | Notes |
|---------------------------|----------------------------------------|-------|
| `SWE_TUNNEL_SERVER_URL`   | `https://tunnel.example.com`           | required to enter tunnel mode |
| `SWE_TUNNEL_IDENTITY_KEY` | base64-of-PEM from bootstrap step      | secret; do not log |
| `SWE_TUNNEL_UNIQUE`       | bare label, e.g. `acme-prod`           | the public hostname will be `<unique>-tunnel.<server-suffix>` |
| `PORT`                    | PaaS-assigned port                     | landing/health server binds this |
| `SWE_SWE_PASSWORD`        | strong password                        | swe-swe auth (still required behind the tunnel) |

Container exposes one port to the PaaS: `$PORT`. That port serves the
landing page (a small static doc with the live tunnel hostname linked
through). The PaaS health probe should hit `GET /` on `$PORT`; a 200
means the container is up and the landing render succeeded.

Internally swe-swe-server binds `0.0.0.0:9898` and the tunnel client
dials `localhost:9898` from the same container. **Do not publish
9898** — its only consumer is the in-container tunnel client.

## Verify the tunnel came up

Check the container logs after the first deploy. You're looking for,
in order:

```
[tunnel-supervisor] event kind=starting
[tunnel-supervisor] event kind=connecting attempt=1
[tunnel-client] identity loaded source=env fingerprint=ab12cd34ef56
[tunnel-client] registered hostname=acme-prod-tunnel.example.com
[tunnel-supervisor] OPEN AT https://9898.acme-prod-tunnel.example.com/
```

The `OPEN AT` line is the operator-friendly URL. The same hostname is
also broadcast on the WS status frame as `tunnelStatus.publicHostname`
and rendered on the landing page (`$PORT`), which updates per request
so it picks up label rotations live.

The `identity loaded` fingerprint should be stable across re-deploys
of the same `SWE_TUNNEL_IDENTITY_KEY`. Compare across deploys to
confirm no identity drift.

## Operating

**Re-deploy.** Keep `SWE_TUNNEL_IDENTITY_KEY` and `SWE_TUNNEL_UNIQUE`
identical and the public hostname is unchanged. Old container
exits → tunneld closes its session → new container connects and
re-binds. There is a short outage window during the swap; for
zero-downtime, run two containers behind separate uniques and swap
DNS / front-end routing.

**Key rotation.** Generate a new identity, set both
`SWE_TUNNEL_IDENTITY_KEY` and a new `SWE_TUNNEL_UNIQUE`, redeploy.
The old `unique`/key binding on tunneld is leaked until tunneld GCs
it; live with that or coordinate with the tunneld operator.

**Migrating between PaaS providers.** Copy
`SWE_TUNNEL_IDENTITY_KEY` and `SWE_TUNNEL_UNIQUE` to the new
provider's secrets, deploy, decommission the old. Same hostname, no
DNS work needed.

## Troubleshooting

| Symptom | Likely cause |
|---------|--------------|
| `kind=fatal reason=key_mismatch` | `SWE_TUNNEL_IDENTITY_KEY` doesn't match the key originally bound to `SWE_TUNNEL_UNIQUE` on tunneld. Either change the unique, or restore the original key. The supervisor stops on fatal; container will idle without a tunnel. |
| `kind=fatal reason=bad_sig` / `version` | client/server protocol mismatch. Bump `SWE_SWE_TUNNEL_REF` and rebuild. |
| `kind=reconnecting retryAfterMs=300000` | rate-limited by tunneld (typically pubkey churn). The frontend banner shows a countdown; wait it out. |
| Landing 200 but no `OPEN AT` line | tunneld unreachable or DNS not yet resolving. Check `SWE_TUNNEL_SERVER_URL` and outbound egress. |
| Identity fingerprint changes between deploys | `SWE_TUNNEL_IDENTITY_KEY` not actually set, or set with embedded newlines (use `base64 -w0`). The client falls back to file-based auto-gen each time. |

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
  SWE_TUNNEL_UNIQUE = "my-swe-swe-prod"

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
- Do **not** add `[[services.ports]]` for 9898. It's an internal
  port; the tunnel handles ingress.

## Known limitations

- The default `swe-swe init` `docker-compose.yml` template does not
  yet propagate `SWE_TUNNEL_IDENTITY_KEY` from the host environment
  to the container. On a PaaS this is moot (you set the env on the
  container directly), but for local docker-compose tunnel testing
  you must add it manually to the compose file or pass
  `-e SWE_TUNNEL_IDENTITY_KEY=...` to `docker compose run`.
- A consumer-side `swe-swe tunnel-identity create` subcommand that
  generates a key, prints `SWE_TUNNEL_IDENTITY_KEY` and a suggested
  `SWE_TUNNEL_UNIQUE`, and stops short of writing to disk is planned
  but not yet shipped. Until then, use the `openssl` recipe above.
