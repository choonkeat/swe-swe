# Agent View reverse tunnel: browser box reaches swe-swe with zero inbound ports

Executable task plan for `/swe-swe:execute-step-by-step` (via
`/swe-swe:execute-in-worktree tasks/2026-07-18-agent-view-reverse-tunnel.md`).
Log convention: `tasks/2026-07-18-agent-view-reverse-tunnel.md-phase{N}.log`.

Origin: dockerless design chat 2026-07-18. Today the remote Agent View
backend (Phase 5 of `tasks/2026-06-27-dockerless-single-binary.md`) makes
chromium reach apps on the swe-swe box via `--host-resolver-rules` (MAP
localhost/lvh.me/localtest.me -> swe-swe IP). That requires the swe-swe
box's ports to be NETWORK-REACHABLE from the browser box -- the resolver
rule redirects DNS, it does not tunnel. This task removes that requirement:
the swe-swe box dials OUT to the browser backend (same trust direction as
swe-swe-tunnel) and page traffic for loopback-style hostnames is shuffled
back over that connection. End state: the swe-swe box needs zero inbound
reachability from anyone; the user reaches it via swe-swe-tunnel, the
browser backend is reached by dialing out.

## Status

**In progress.**

- [x] Phase 1 -- stream mux over one WebSocket (TDD, net.Pipe) -- DONE 2026-07-18
- [x] Phase 2 -- backend side: tunnel endpoint + declarative bind manager + peercred guard -- DONE 2026-07-18 (live-verified with real chromium; note: task's "curl -> open frame" positive check replaced by chromium-driven load since the fail-closed peer guard correctly rejects curl; curl is the negative check)
- [x] Phase 3 -- client side: dial-out, local dial-back, port sources incl. /proc/net/tcp mirror -- DONE 2026-07-18 (one-machine e2e: app on 127.0.0.2 so backend can bind 127.0.0.1:same-port; app-kill removal proven via static-clear+exclude since the mirror would re-see the backend's own listener on one machine)
- [ ] Phase 4 -- chromium wiring + e2e proving no-inbound-route operation
- [ ] Phase 5 -- docs + changelog + netns follow-up note

## Ground rules for the executing agent

- ASCII only in all code/markdown (no em-dashes, no smart quotes).
- Run tests with `make test`, never bare `go test` / `go vet`.
- After ANY change under `cmd/swe-swe/templates/`: `make build golden-update`,
  `git add cmd/swe-swe/testdata/golden`, review the staged golden diff.
- Stage explicit paths by name. NEVER `git add -A`.
- Never create per-request `http.Transport` (CLAUDE.md memory-leak rule).
- Never silently discard child process exit status.
- If any verification fails and a workaround is tempting: STOP and ask via
  send_message. No silent compromises.

## Design (settled -- do not relitigate)

### Approach: loopback listeners on the browser box, declaratively bound

REJECTED alternative, recorded for context: a per-session HTTP CONNECT
forward proxy (`chromium --proxy-server`). It handles arbitrary ports and
multi-tenancy elegantly, but needs PAC/bypass configuration, changes
chromium flags, and buys generality we do not need for the v1 deploy shape
(ONE dedicated backend per swe-swe box, matching the documented
`docker run -p 9333:9333 swe-swe/browser-backend`).

CHOSEN: the backend binds real listeners on ITS OWN loopback for the ports
the swe-swe box cares about. `lvh.me`/`localtest.me`/`localhost` resolve to
127.0.0.1 on the browser box naturally, so in tunnel mode chromium needs NO
`--host-resolver-rules` at all -- the Host header arrives intact and the
preview vhost demux on the swe-swe side routes normally. Each accepted
connection becomes a stream over the swe-swe-initiated WebSocket; the
swe-swe side dials its own 127.0.0.1:<same port> and pipes.

### Transport

One WebSocket per session, dialed BY swe-swe:
`GET <backend>/sessions/<id>/tunnel` with the existing bearer token
(gorilla/websocket v1.5.3 is already in the server go.mod; both endpoints
are swe-swe-server code -- the client role and `-mode browser-backend` --
so the mux package is shared, not duplicated).

Framing (custom, small, stdlib + gorilla only -- no yamux dependency):

- Control frames: WS text messages, JSON:
  - client -> backend: `{"op":"sync","ports":[1977,3000,...]}` -- the FULL
    desired port set, declarative. Backend reconciles: binds missing,
    closes removed, replies `{"op":"sync-result","bound":[...],"refused":
    [{"port":6080,"reason":"reserved"}, ...]}`. Idempotent; re-sent on
    every change and on reconnect. No incremental bind/unbind ops -- sync
    semantics survive reconnects and drop no state.
  - backend -> client: `{"op":"open","stream":7,"port":3000}` when a
    loopback connection is accepted.
  - either: `{"op":"close","stream":7}`.
- Data frames: WS binary messages, 4-byte big-endian stream id + payload.
- Liveness: WS ping/pong, 30s interval, 2 misses = dead -> client
  reconnects with capped exponential backoff (1s..30s) and re-sends sync.

### Port sources on the swe-swe side (union, deduped)

1. **Static**: SWE_SERVER_PORT and the preview vhost port. Always bound.
2. **Procfile**: ports declared by swe-run services for the session --
   pre-bound at session/tunnel start.
3. **Mirror**: poll `/proc/net/tcp` + `tcp6` (~2s) for LISTEN sockets on
   loopback or wildcard addresses; new port -> appears in next sync,
   vanished -> removed. This catches ad-hoc `npm run dev`-style processes
   with zero configuration. Exclusions: an env-configurable deny-list
   (`SWE_AGENT_VIEW_TUNNEL_EXCLUDE_PORTS`, CSV of ports/ranges) defaulting
   to swe-swe's own internal per-session pools (proxy fleet, MCP, CDP/VNC
   client-side ports) so we do not mirror plumbing. Off Linux the mirror
   source is simply absent (static + Procfile still work).

### Backend bind rules

- Bind 127.0.0.1:<port> only. Never wildcard.
- REFUSE (and report in sync-result) ports in the backend's own reserved
  ranges: the service port, `SWE_CDP_PORTS`, `SWE_VNC_PORTS` (both internal
  and external halves), and anything already bound by another session.
  First bind wins across sessions -- collision is reported, not silent.
- Guard accepted connections with SO_PEERCRED + /proc ancestry (prior art:
  `broker.go` / `peercred_*.go`): only processes in THAT session's
  chromium process tree may connect. The backend image is Linux, so the
  guard is always on there; the pure-Go accept path keeps a build-tag
  fail-open like the broker for non-Linux dev builds.
- Every listener/goroutine ties to the session lifecycle: session teardown
  closes listeners, streams, and the WS. No orphans (log exits with PID +
  status per the no-silent-Wait rule).

### Mode selection and coexistence

- New value semantics for the existing knob: `--agent-view=<url>` keeps
  today's DIRECT behavior (resolver rules, needs inbound reachability).
  Tunnel mode is opt-in: `--agent-view-tunnel` boolean flag (+
  `SWE_AGENT_VIEW_TUNNEL=1`), only meaningful with a remote URL.
- In tunnel mode: allocation request carries `"tunnel":true`; the backend
  omits `--host-resolver-rules` from chromium and expects the client to
  dial `/sessions/<id>/tunnel`; `SWE_AGENT_VIEW_LOCALHOST` /
  `SWE_AGENT_VIEW_LOOPBACK_DOMAINS` are ignored (log a note if set).
- Direct mode remains the default -- zero behavior change for existing
  deployments; CDP and VNC continue to flow as today in BOTH modes (they
  are already outbound-only from swe-swe).

### Future (documented, NOT built now)

Multi-tenant shared backends want per-session network namespaces so each
chromium gets a private loopback (kills cross-tenant port collisions AND
strengthens the peercred guard). Needs CAP_NET_ADMIN in the backend
container. Record as a follow-up task stub in Phase 5; the sync protocol is
unchanged by it -- only where the binds happen moves.

## Phase 1 -- stream mux (TDD)

New file(s) in `cmd/swe-swe/templates/host/swe-swe-server/` (e.g.
`agentview_tunnel_mux.go` + test). Implement the framing above over an
abstract `io.ReadWriteCloser`-ish message conn so tests use `net.Pipe`-
backed fakes, not real WebSockets.

Tests (RED first): frame encode/decode round-trip; interleaved streams;
close propagation both directions; data-after-close dropped without panic;
sync/sync-result marshal; backpressure sanity (a slow stream does not
deadlock others -- bounded per-stream buffers, document the chosen bound).

**Verify:** `make test` green.

## Phase 2 -- backend side

In `browser_backend_service.go` (+ new `agentview_tunnel_backend.go`):

- WS endpoint `GET /sessions/{id}/tunnel`, bearer-authed like `/sessions`,
  404 for unknown session, 409 for a second concurrent tunnel on one
  session.
- Declarative bind manager implementing the sync/reconcile + refusal rules
  above. Unit-test reconciliation (bind new, close removed, refuse
  reserved, refuse cross-session duplicate, full teardown on session end).
- Peercred guard on accept (reuse broker helpers; build-tag split like
  `peercred_*.go`). Unit-test the Linux path with a real Unixless TCP
  loopback pair asserting self-connections pass (test process is its own
  ancestor stand-in via an injectable checker).
- Allocation handler: accept `"tunnel":true`, skip resolver rules, include
  `"tunnel":true` in allocResponse.

**Verify:** `make test` green; run a real
`swe-swe-server -mode browser-backend` locally, drive it with a small Go
test client: sync [<free port>], curl that loopback port, see the open
frame arrive.

## Phase 3 -- client side (swe-swe server)

In `browser_backend_remote.go` (+ new `agentview_tunnel_client.go`):

- When tunnel mode is on and allocation succeeded: dial the tunnel WS,
  start the port-source aggregator (static + Procfile + /proc mirror),
  send sync on every set change, log refused ports at warn once per port.
- Handle `open` frames: dial `127.0.0.1:<port>`, pipe both directions,
  close stream on either EOF; dial failure -> immediate close frame.
- Reconnect loop with backoff; on reconnect re-send current sync. Tunnel
  death does NOT kill the session -- Agent View pages just fail until it
  reconnects (log clearly).
- /proc/net/tcp watcher as its own small, unit-tested parser function
  (feed it fixture file contents; hex parsing, loopback + wildcard
  filtering, deny-list).

**Verify:** `make test` green; wire a loopback e2e ON ONE MACHINE: server
in tunnel mode + backend service on localhost, an `http.Server` on a
random loopback port, assert a curl against the BACKEND's loopback port
returns that server's response (proving the full accept->stream->dial-back
path), then kill the app and assert the port leaves the next sync.

## Phase 4 -- chromium wiring + e2e proving the point

- Tunnel-mode chromium launch omits `--host-resolver-rules` (assert in a
  unit test on the arg builder).
- Extend `make test-e2e-agent-view-remote` with a tunnel-mode tier. The
  headline assertion: the browser-box chromium loads a page served ONLY on
  the swe-swe side's 127.0.0.1 while the allocation's
  `resolveLocalhostTo` path is proven unused -- run the backend container
  with NO route back to the swe-swe host's ports (e.g. do not publish
  them / point resolveLocalhostTo at a blackhole IP) and the page still
  renders. Also assert: `*.lvh.me` vhost routing works through the tunnel
  (Host header intact), and a Procfile-declared port is bound before its
  first request (no mirror race).
- Existing direct-mode e2e tiers must still pass unchanged.

**Verify:** both e2e tiers PASS live; direct-mode tier unchanged;
`make test` green; goldens updated if any template/flag surface changed.

## Phase 5 -- docs + changelog

- `docs/dockerless.md`: Option B gains the tunnel variant -- "your box can
  stay fully firewalled; swe-swe connects out" -- with the
  `--agent-view-tunnel` flag and the port-source story (Procfile pre-bind,
  auto-mirror for ad-hoc ports).
- `tasks/2026-06-27-dockerless-single-binary.md`: follow-up pointer here.
- New stub `tasks/TODO-agent-view-netns-multitenancy.md` capturing the
  netns design sketch from this file's Future section.
- CHANGELOG entry.

**Verify:** `make test` green; commit.

## End state

A swe-swe box with ZERO inbound reachability -- user arrives via
swe-swe-tunnel, browser backend arrives via this reverse tunnel -- runs
full remote Agent View: chromium on the browser box hits its own loopback
for localhost/*.lvh.me, the backend shuffles it down the swe-swe-initiated
WebSocket, swe-swe replays it against its own loopback. Ports follow the
session automatically (static + Procfile + /proc mirror), collisions and
reserved ranges are refused loudly, and per-session peercred guarding
keeps other processes on the browser box out. Direct mode is untouched.
