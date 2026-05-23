# Run swe-swe with tunnel mode on your laptop

Copy-paste runbook for running a tunnel-mode container locally (no
PaaS deploy). For what the env vars mean, the identity model, and
troubleshooting, see [tunnel-explained.md](tunnel-explained.md).

## Prerequisites

- Docker + Compose plugin
- `swe-swe` CLI installed (`npx swe-swe ...` works too)
- A `swe-swe-tunnel` admin who can authorize your pubkey

## 1. Generate identity, send pubkey to admin (one-time)

```sh
openssl genpkey -algorithm Ed25519 -out identity.key

# extract the matching pubkey and send out-of-band to your admin
openssl pkey -in identity.key -pubout -outform DER | tail -c 32 | base64 -w0 | tr -d '='
```

Wait for the admin to confirm authorization, then keep `identity.key`
somewhere safe (1Password, etc.).

## 2. Init the project with tunnel mode

```sh
cd /path/to/your/project
swe-swe init --tunnel-server-url=https://tunnel.example.com
```

This regenerates the project's docker-compose stack with tunnel mode
baked in (no Traefik, swe-swe-tunnel client built into the image,
supervisor wired up). Without this step, the `SWE_TUNNEL_*` env vars
in step 3 are read by nothing.

## 3. Run

```sh
env \
  SWE_TUNNEL_SERVER_URL=https://tunnel.example.com \
  SWE_TUNNEL_IDENTITY_KEY=$(base64 -w0 < /path/to/identity.key) \
  SWE_TUNNEL_UNIQUE=<your-unique-label> \
  SWE_BIND=127.0.0.1:1977 \
  SWE_SWE_PASSWORD=<strong-password> \
  swe-swe up --build
```

## 4. Verify

In another terminal:

```sh
swe-swe logs swe-swe | grep -E 'identity loaded|OPEN AT|kind='
```

You should see:

```
[tunnel-client] identity loaded source=env fingerprint=ab12cd34ef56
[tunnel-supervisor] OPEN AT https://1977.<your-unique>-tunnel.example.com/
```

Open that URL -- it'll prompt for `SWE_SWE_PASSWORD`. After login,
the swe-swe UI loads through the tunnel.

If verification fails (no `OPEN AT`, `source=file` instead of
`source=env`, fatal `kind=...`), see
[tunnel-explained.md](tunnel-explained.md#troubleshooting).

## Optional: reach the containers directly from your laptop

By default tunnel mode publishes **no host ports** -- swe-swe-server
binds `127.0.0.1` *inside the container* and is reachable only through
the tunnel. If you also want to hit it from the laptop running the
container (e.g. `curl localhost:1977`, or point a local tool at a
preview port), add `--tunnel-local-ports` at init:

```sh
swe-swe init \
  --tunnel-server-url=https://tunnel.example.com \
  --tunnel-local-ports
```

This widens the server bind to all interfaces and publishes, on the
host's `127.0.0.1` only:

| Port(s) | What |
|---|---|
| `SWE_PORT` (default `1977`) | main UI |
| `23000-23019` | app preview proxy (`preview-port + proxy-offset`) |
| `24000-24019` | agent-chat proxy |
| `27000-27019` | VNC proxy |
| `5000-5019` | public (no-auth) range |

(Proxy ranges shown with the default `--proxy-port-offset=20000`.)

Host-loopback only: nothing is exposed beyond the machine's own
localhost, so the tunnel stays the only network-facing path. The tunnel
itself is unaffected -- the tunnel client still dials `127.0.0.1:{port}`
inside the container, which the all-interfaces bind covers.
`SWE_SWE_PASSWORD` still gates these ports exactly as it gates the
tunnel.
