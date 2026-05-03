# Deploy swe-swe with tunnel mode to a PaaS

Generic copy-paste runbook for deploying a tunnel-mode container to
any container PaaS that can pull a Linux image, run it, accept env
vars / secrets, and expose one HTTP port. For a concrete
PaaS-specific walkthrough, see [tunnel-fly.md](tunnel-fly.md). For
what the env vars mean, the identity model, and troubleshooting, see
[tunnel-explained.md](tunnel-explained.md).

## Prerequisites

- A container PaaS account
- A container registry your PaaS can pull from
- A `swe-swe-tunnel` admin who can authorize your pubkey

## 1. Generate identity, send pubkey to admin (one-time)

```sh
openssl genpkey -algorithm Ed25519 -out identity.key

# base64-encode the PEM for the PaaS secret
base64 -w0 < identity.key > identity.key.b64

# extract the matching pubkey and send out-of-band to your admin
openssl pkey -in identity.key -pubout -outform DER | tail -c 32 | base64 -w0 | tr -d '='

# wipe the local PEM once identity.key.b64 is stored safely --
# the PaaS secret holds the canonical copy
rm identity.key
```

## 2. Build and push the image

```sh
cd /path/to/your/project
swe-swe init --tunnel-server-url=https://tunnel.example.com --metadata-dir=./dockerbuild
docker build -t <registry>/<your-app>:tunnel ./dockerbuild/
docker push <registry>/<your-app>:tunnel
```

## 3. Configure PaaS env vars

Set on your PaaS (mark the SECRET ones as secrets):

```
SWE_TUNNEL_SERVER_URL=https://tunnel.example.com
SWE_TUNNEL_UNIQUE=<your-unique-label>
SWE_BIND=127.0.0.1:1977
SWE_TUNNEL_IDENTITY_KEY=<contents of identity.key.b64>   # SECRET
SWE_SWE_PASSWORD=<strong-password>                       # SECRET
```

`PORT` is usually injected by the PaaS (commonly 8080). The container
binds it for the landing/health server, which is what the PaaS health
probe hits. Don't override `PORT` unless your PaaS requires it.

## 4. Deploy

Push the image (step 2 already did this) and trigger a deploy via
your PaaS's UI or CLI. Most PaaSes pull-and-restart automatically
once env vars and image are set; some need an explicit deploy
command.

## 5. Verify

Check the container logs in your PaaS UI for:

```
[tunnel-client] identity loaded source=env fingerprint=ab12cd34ef56
[tunnel-supervisor] OPEN AT https://1977.<your-unique>-tunnel.example.com/
```

Open that URL -- it'll prompt for `SWE_SWE_PASSWORD`. After login,
the swe-swe UI loads through the tunnel.

## PaaS gotchas

- **Outbound HTTPS to the tunnel server must be allowed.** Most
  PaaSes allow this by default; some lock down egress. Test with a
  one-off exec-into-container `curl https://tunnel.example.com/` if
  the supervisor never reaches `connecting`.
- **Single port exposed to the PaaS.** Only `PORT` (the landing /
  healthcheck server) is exposed. Internal port 1977 (swe-swe-server)
  is loopback-only, reached by the in-container tunnel client.
- **Persistent volume not required.** `SWE_TUNNEL_IDENTITY_KEY`
  delivers the identity; nothing else needs persistence.
- **Healthcheck path.** Use `/` (the landing render). It returns 200
  even when the tunnel is reconnecting, so transient reconnects
  don't flap your healthcheck.

If verification fails (no `OPEN AT`, fingerprint changes between
deploys, fatal `kind=...`), see
[tunnel-explained.md](tunnel-explained.md#troubleshooting).
