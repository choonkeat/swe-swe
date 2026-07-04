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

## Out of scope (follow-ups)

- **Loopback hostname mapping** — when chromium runs here, the agent's
  `http://localhost:3000` dev server resolves to *this* box, not the swe-swe
  host. The intended fix is chromium `--host-resolver-rules`; see
  `tasks/2026-06-27-browser-backend-service.md`.
- **Remote `vnc-ready`** — the readiness probe currently checks the local VNC
  port on the swe-swe host; in remote mode it should consult the backend's
  `/sessions/{id}/ready`.
