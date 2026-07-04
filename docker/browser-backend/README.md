# swe-swe/browser-backend

The relocatable **Agent View** backend. Agent View is the only swe-swe tab that
needs a heavy display stack (chromium + Xvfb + x11vnc + noVNC/websockify). This
image runs that stack as a standalone, network-facing allocation service so a
lean (dockerless) swe-swe host can offload Agent View to it.

It is the **same `swe-swe-server` binary** as the main image, started with
`-mode browser-backend`.

## Build

From the repo root, after building the dockerless payload (which compiles the
static `swe-swe-server`):

```sh
make dockerless-payload
make browser-backend-image            # or the docker build below
docker build -f docker/browser-backend/Dockerfile --build-arg ARCH=amd64 \
    -t swe-swe/browser-backend .
```

## Run

```sh
docker run --rm \
    -p 9333:9333 -p 6000-6019:6000-6019 -p 7000-7039:7000-7039 \
    -e SWE_BROWSER_BACKEND_TOKEN=some-shared-secret \
    swe-swe/browser-backend
```

Then point a swe-swe host at it (no display stack needed there):

```sh
SWE_BROWSER_BACKEND_TOKEN=some-shared-secret \
    swe-swe up --agent-view=https://browser-box.internal:9333
```

(The server reads the token from `SWE_BROWSER_BACKEND_TOKEN` for both the
service and the client.)

## API

| Method | Path                     | Purpose                                   |
|--------|--------------------------|-------------------------------------------|
| POST   | `/sessions`              | Allocate a browser → `{sessionId,host,cdpPort,vncPort}` |
| DELETE | `/sessions/{id}`         | Free a session + reap its processes       |
| GET    | `/sessions/{id}/ready`   | Readiness (websockify listening)          |
| GET    | `/health`                | `{sessions,max}` (open, no auth)          |

`/sessions*` require `Authorization: Bearer $SWE_BROWSER_BACKEND_TOKEN` when a
token is configured. Each session gets an isolated Chromium profile and X
display; the service caps concurrency at the VNC port-range size (override with
`-browser-backend-max`).

## Networking

The agent host must be able to reach the backend's **API port** *and* the
**CDP/VNC port ranges** it returns. Terminate TLS at the box or behind a proxy.

## Localhost resolution

Chromium here resolves loopback-style dev hostnames back to the **swe-swe
host** (`--host-resolver-rules`), so pages the agent opens at
`http://localhost:3000` or `http://tenant1.lvh.me:3000` reach the dev server
there, not this box. Default domain set (each bare + `*.` wildcard):
`localhost`, `lvh.me`, `localtest.me`. Deliberately NOT `*.nip.io`/`*.sslip.io`
-- those encode arbitrary IPs that must keep resolving normally.

- Target address: defaults to the allocation request's source address;
  override per-host with `SWE_AGENT_VIEW_LOCALHOST` on the swe-swe side (NAT)
  or per-request with `resolveLocalhostTo` on `POST /sessions`.
- Domain set: override with `SWE_AGENT_VIEW_LOOPBACK_DOMAINS` (comma-separated)
  on the swe-swe side, or `loopbackDomains` (array) on `POST /sessions`.
- IP-literal URLs (`http://127.0.0.1:3000`) bypass the resolver and stay
  local to this box.

## CDP forwarder

Headful chromium ignores `--remote-debugging-address` and binds CDP to
loopback only. Each session therefore runs chromium on an internal loopback
port (one range-size above `cdpPort`) behind a reverse-proxy forwarder that
serves the advertised `cdpPort` on all interfaces and keeps the `/json`
discovery URLs pointing at it.

## e2e

`make test-e2e-agent-view-remote` (binary tier, no Docker) /
`make test-e2e-agent-view-remote-image` (this image) prove the full loop:
allocation, vnc-ready, noVNC canvas in the UI, and the cross-namespace
localhost navigation.
