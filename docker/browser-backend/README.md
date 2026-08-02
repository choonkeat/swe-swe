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
| POST   | `/sessions/{id}/touch`   | Keepalive: "still in use, do not reap"    |
| GET    | `/health`                | `{sessions,max}` (open, no auth)          |

`/sessions*` require `Authorization: Bearer $SWE_BROWSER_BACKEND_TOKEN` when a
token is configured. Each session gets an isolated Chromium profile and X
display; the service caps concurrency at the VNC port-range size (override with
`-browser-backend-max`).

## Idle reaping

A client that crashes (or whose `DELETE` fails) would otherwise hold its slot
and its 4 processes forever, so ~20 losses exhaust the pool. The backend
therefore frees sessions that nothing has used for `-browser-backend-idle`
(default `30m`, env `SWE_BROWSER_BACKEND_IDLE`, `0` disables).

"Used" is deliberately broad, so an active browser is never pulled out from
under anyone:

- any request through the session's CDP forwarder -- **including** the
  long-lived debugger websocket, which counts for as long as it stays open, not
  just when it was opened;
- a live reverse tunnel;
- `POST /sessions/{id}/touch`, which the swe-swe host sends every 2 minutes
  while a human has the Agent View (VNC) pane open -- that traffic terminates
  inside websockify and is invisible to the allocator otherwise;
- `POST /sessions` (including the idempotent re-POST) and `GET .../ready`.

If a browser IS reaped and the agent uses it afterwards, the swe-swe host's CDP
proxy re-allocates and replays the request, so the agent sees a pause rather
than a failure. The replacement is a **fresh** browser: open pages, history,
cookies and logins are gone. When no replacement can be had, the proxy answers
with a plain-English JSON body (`agent-view-browser-unavailable`) instead of a
bare dial error.

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
