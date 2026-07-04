# Browser-backend service (relocatable Agent View)

## Status

**Planned.** Not started. Companion to
`tasks/2026-06-27-dockerless-single-binary.md` Phase 5 -- that phase
builds the *client*; this builds the *service* it talks to.

## Problem

Agent View (the agent-drivable Chromium shown over VNC) is the only tab
that needs a heavy, non-bundleable stack: `chromium`, `xvfb`, `x11vnc`,
`websockify`, `novnc`. Today that stack is **embedded in the main image
and spawned in-process per session** by `startSessionBrowser`
(`main.go:4231+`) -- four subprocesses per session, lifetimes tracked and
reaped by swe-swe-server itself. There is no standalone browser service.

The dockerless vision (`docs/dockerless.md`, "Option B") promises:

```sh
docker run -p 9333:9333 swe-swe/browser-backend
swe-swe up --agent-view=https://browser-box.internal:9333
```

i.e. a separate, network-facing, multi-session browser service that a
lean host binary (no display stack) can offload Agent View to. That
service does not exist yet.

## What exists to build on

The per-session wiring is already env/URL-driven, which is the seam:
- Playwright MCP attaches via `BROWSER_CDP_PORT` ->
  `@playwright/mcp --cdp-endpoint http://localhost:$BROWSER_CDP_PORT`.
- The VNC view is a reverse-proxy to `localhost:$VNCPort` (websockify).
- Browser start is already on-demand: `POST
  /api/session/{uuid}/browser/start` + `mcp-lazy-init`; readiness via
  `GET /api/session/{uuid}/vnc-ready`.

So per-session allocation logic already lives in swe-swe-server; this
task extracts/repackages it behind a network API.

## Goals / non-goals

- **Goal:** a publishable `swe-swe/browser-backend` image that allocates
  an **isolated** Chromium per session (own `--user-data-dir`, own
  display) and exposes, per session, a CDP endpoint (for the agent) and a
  VNC/noVNC stream (for the human).
- **Goal:** the existing in-process `local` backend stays the default and
  unchanged; the service is purely additive.
- **Non-goal:** a general-purpose browser farm / autoscaling. One box,
  many sessions, bounded by config.
- **Non-goal:** reimplementing the per-session browser logic from
  scratch -- reuse what `startSessionBrowser` already does.

## Out of scope: loopback hostname mapping (noted, not built here)

When chromium runs on a separate box, `localhost:$PORT` (and dev domains
like `*.lvh.me`) resolve to the *browser box's* loopback, not the
swe-swe-server host where the agent's dev server lives -- so
agent-driven navigation to `http://localhost:3000` reaches nothing. (The
Preview tab is unaffected; it is a server-side reverse-proxy.)

The intended fix is **chromium `--host-resolver-rules`** (NOT
`/etc/hosts` -- no wildcards, and globally remapping `localhost` would
break the box's own CDP/x11vnc/websockify loopback). It is
chromium-scoped and wildcard-capable, e.g.
`--host-resolver-rules="MAP localhost HOST_IP, MAP *.lvh.me HOST_IP, MAP
*.localhost HOST_IP, MAP *.localtest.me HOST_IP, MAP vcap.me HOST_IP"`
(skip `nip.io`/`sslip.io` -- they encode an IP in the name). Caveats to
revisit when this is picked up: resolver-rules map host only (port/path
preserved), so the per-session dev port must be reachable browser-box ->
server-host; and scoping `HOST_IP` to the dev-port range (not the whole
host) is needed to avoid an SSRF surface.

**This is explicitly out of scope for this task.** Build the allocation
service first; loopback mapping is a follow-up.

## Design sketch

- **Allocation API** (the contract Phase 5's `remote` client calls):
  - `POST /sessions` -> `{ sessionId, cdpURL, vncURL }` (allocates Xvfb +
    chromium + x11vnc + websockify for an isolated profile).
  - `DELETE /sessions/{id}` -> tears down + frees ports/profile.
  - `GET /sessions/{id}/ready` -> readiness (mirror of `vnc-ready`).
  - Health endpoint + a max-sessions cap (back-pressure when full).
- **Auth:** shared token (header/bearer) so a public box is not an open
  browser relay. Config `SWE_BROWSER_BACKEND_TOKEN`; client sends it.
- **Networking:** both CDP and the VNC websocket must be reachable by the
  swe-swe-server host. Document TLS expectations (terminate at the box or
  behind a proxy).
- **Lifecycle/cleanup:** reap on `DELETE`, on idle timeout, and on client
  disconnect; never leak chromium processes (per CLAUDE.md: log every
  child exit -- no silent `Wait`).
- **Image:** factor the browser stack + a small Go service (could be a
  `swe-swe-server browser-backend` subcommand of the multi-call binary)
  into its own thin image. Reuses the same chromium/novnc layers the main
  image has today.

## Work

1. Extract the per-session browser orchestration (`startSessionBrowser` /
   `stopSessionBrowser`, port allocation, reaping) into a reusable
   package shared by the in-process path and the service.
2. Add the allocation HTTP API + auth + caps + health.
3. Add `browser-backend` as a subcommand of the multi-call binary
   (Phase 1) and a thin `swe-swe/browser-backend` image; publish it.
4. Wire it to Phase 5's `remote` client end-to-end (allocate ->
   cdpURL/vncURL -> agent drives chromium + human sees VNC).
5. Graceful degradation: unreachable/over-capacity backend -> Agent View
   "unavailable", other tabs unaffected.

## Verify

- Lean host (no chromium) + `swe-swe up --agent-view=https://box:9333`:
  Agent View renders and the agent can navigate via Playwright MCP.
- Two concurrent sessions get isolated profiles (no cookie/state bleed).
- Kill a session -> all four backend subprocesses for it are gone (no
  leaks); backend log shows each child's exit.
- Backend down -> Agent View degrades, five other tabs unaffected.
