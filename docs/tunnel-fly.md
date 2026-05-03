# Deploy swe-swe with tunnel mode to Fly.io

Copy-paste runbook for deploying a tunnel-mode container to Fly. For
what the env vars mean, the identity model, and troubleshooting, see
[tunnel-explained.md](tunnel-explained.md).

## Prerequisites

- `flyctl` installed and logged in
- A container registry your Fly app reads from (Fly's built-in
  `registry.fly.io/<app>` works)
- A `swe-swe-tunnel` admin who can authorize your pubkey

## 1. Generate identity, send pubkey to admin (one-time)

```sh
openssl genpkey -algorithm Ed25519 -out identity.key

# base64-encode the PEM for the Fly secret
base64 -w0 < identity.key > identity.key.b64

# extract the matching pubkey and send out-of-band to your admin
openssl pkey -in identity.key -pubout -outform DER | tail -c 32 | base64 -w0 | tr -d '='

# wipe the local PEM once identity.key.b64 is stored safely --
# the Fly secret holds the canonical copy
rm identity.key
```

## 2. Build and push the image

```sh
cd /path/to/your/project
swe-swe init --tunnel-server-url=https://tunnel.example.com --metadata-dir=./dockerbuild
docker build -t registry.fly.io/<your-app>:tunnel ./dockerbuild/
docker push registry.fly.io/<your-app>:tunnel
```

## 3. Configure Fly

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

## 4. Deploy

```sh
fly apps create my-swe-swe
fly secrets set \
    SWE_TUNNEL_IDENTITY_KEY="$(cat identity.key.b64)" \
    SWE_SWE_PASSWORD='choose-a-strong-one'
fly deploy --image registry.fly.io/my-swe-swe:tunnel
```

## 5. Verify

```sh
fly logs | grep -E 'identity loaded|OPEN AT|kind='
```

You should see:

```
[tunnel-client] identity loaded source=env fingerprint=ab12cd34ef56
[tunnel-supervisor] OPEN AT https://1977.myproject123-tunnel.example.com/
```

Open that URL -- it'll prompt for `SWE_SWE_PASSWORD`. After login,
the swe-swe UI loads through the tunnel.

## Notes

- `internal_port = 8080` matches `$PORT` Fly injects (8080 by
  default). The container's landing server binds `:$PORT`.
- `[http_service.checks].path = "/"` hits the landing render. It
  returns 200 even when the tunnel is reconnecting (status banner
  reflects state separately) so health stays green during transient
  reconnects.
- Do **not** add `[[services.ports]]` for 1977 -- it's an internal
  port; the tunnel handles ingress.

If verification fails (no `OPEN AT`, fingerprint changes between
deploys, fatal `kind=...`), see
[tunnel-explained.md](tunnel-explained.md#troubleshooting).
