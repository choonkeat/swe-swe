# Tunnel-mode pivot: subprocess supervision instead of state-file IPC

## Status

**Proposed (2026-04-29)** — supersedes the state-file fallback approach
shipped as Step 4 of `tasks/2026-04-27-swe-swe-tunnel-integration.md`.
No code changes yet; this file captures the decision and the planned
follow-up work on the swe-swe side.

Companion plan in the swe-swe-tunnel repo:
`/repos/swe-swe-tunnel/workspace/tasks/2026-04-29-supervisor-event-protocol.md`.

## Why we are pivoting

The shipped state-file design (`/workspace/.swe-swe/tunnel-state.json`,
read once at startup by `resolvePublicHostname`) has an inter-process
timing flaw. swe-swe-server reads the file exactly once during `main()`
boot. There is no watcher, no polling, and no re-resolve. Possible
boot orders therefore split into:

1. **Tunnel client first, then swe-swe-server.** State file already on
   disk, swe-swe-server reads it, all good.
2. **swe-swe-server first, then tunnel client.** State file missing at
   read time, swe-swe-server stays in legacy mode permanently. Tunnel
   client writing the file later does nothing. A swe-swe-server restart
   is required after the tunnel finishes registering.

Order 2 is the realistic case under most supervisors (compose, systemd
unit ordering, fly.io machine boot). Even adding a watcher only papers
over the bigger issue: **there are two opaque processes whose only
communication channel is a file**. They cannot signal each other on
disconnect, on label rotation, or on graceful shutdown.

A subprocess relationship makes the supervisor (swe-swe-server) the
parent of the tunnel client. Lifecycle events arrive on a stdout event
stream as they happen. Re-registration with a new label propagates in
real time. Crashes are observed and restarted. The state file becomes
obsolete.

Alternatives considered:

- **fsnotify file watcher in swe-swe-server.** Solves order 2 but
  retains the loose coupling. Adds a Go dependency and a goroutine.
- **Polling the state file.** Same shape as fsnotify but uglier and
  slower.
- **Tunnel client as an in-process Go library.** Cleanest in code but
  requires moving `internal/tunnelclient` to a public package in the
  swe-swe-tunnel module, rebuilds swe-swe on every tunnel-side change,
  and pulls every tunnel transitive dep (yamux, ed25519, slog) into
  the swe-swe binary. Considered and rejected in favor of the
  subprocess approach for ops simplicity.
- **Subprocess supervision (chosen).** Two binaries, swe-swe-server
  exec's the tunnel client and reads structured events off stdout.
  Tunnel-side changes ship independently. Lifecycle is observable.
  No build coupling.

## What changes on the swe-swe side

### Removed

- `cmd/swe-swe/templates/host/swe-swe-server/tunnel_state.go` —
  the file becomes dead code.
- `cmd/swe-swe/templates/host/swe-swe-server/main.go` —
  - `--public-hostname` / `SWE_PUBLIC_HOSTNAME` flag (no longer the
    path of truth; the live event stream is).
  - `--tunnel-state-file` / `SWE_TUNNEL_STATE_FILE` flag.
  - `resolvePublicHostname` (replaced by the live-event reader).
- The `flag → env → state-file → ""` resolution chain in `main()`.
- Associated tests (`TestResolvePublicHostname`, the readTunnelState
  table). The remaining `TestBuildStatusPayloadIncludesPublicHostname`
  stays — it tests the WS broadcast shape, which is unchanged.

### Added

- New flags on `swe-swe-server`:
  - `--tunnel-server-url` / `SWE_TUNNEL_SERVER_URL` (e.g.
    `https://tunnel.example.com`). When non-empty, swe-swe-server
    spawns the tunnel client subprocess. Empty disables tunnel mode
    entirely (legacy port-mode).
  - `--tunnel-unique` / `SWE_TUNNEL_UNIQUE` (e.g. `alpha`). The bare
    label the client requests. Optional; if empty the tunnel client's
    own default kicks in (probably "auto-generate / read identity").
  - `--tunnel-bin` / `SWE_TUNNEL_BIN` — path to the swe-swe-tunnel
    client binary. Default `swe-swe-tunnel` on `$PATH`. Empty falls
    through to legacy mode (so missing binary in dev does not crash
    swe-swe-server boot).
- Subprocess supervisor goroutine in swe-swe-server:
  - Spawns the tunnel client with the right flags + a flag asking
    for the structured-event output mode (see companion plan).
  - Reads the child's stdout line-by-line, decodes each line as one
    JSON event.
  - Updates an in-memory `serverPublicHostname` on `register_ok` and
    `relabel`. Calls `BroadcastStatus` on every change so connected
    browsers update without reload.
  - Logs the child's stderr to swe-swe-server's logger with a clear
    `[tunnel-client]` prefix.
  - On child exit: log PID + exit status (per the no-silent-Wait
    coding rule), backoff (exponential, capped at 60s), respawn.
    Hostname is cleared during the gap so the WS broadcasts an empty
    string and the frontend falls back to legacy mode until the
    child re-registers. UI behavior during outage is part of the
    test plan.
  - On swe-swe-server graceful shutdown: signal the child, wait up
    to N seconds for graceful Deregister, then SIGKILL.

### Unchanged (stays as-is)

Everything that lives downstream of `serverPublicHostname` is
identical:

- `Session.PublicHostname` field, copied at session creation.
- Frontend `getPreviewBaseUrl` and `getAgentChatUrl` branching on
  `this.publicHostname`.
- `auth.go:resolveCookieDomain` returning `publicHostname` so cookies
  are scoped across `{port}.{publicHostname}` subdomains.
- `tailscale.go:resolveListenAddr` `--bind` / `SWE_BIND` flag.
- The frontend `buildSubdomain*` URL helpers and their unit tests.

The only seam touched is the source of `serverPublicHostname`: it
becomes a live, mutable value driven by child events instead of a
static value resolved at startup.

### Bonus: the flag-deletion closes a cookie-scope footgun

`Cookie.Domain` is set from `serverPublicHostname` verbatim. Per
RFC 6265, `Domain=alpha-tunnel.example.com` scopes the cookie to
that FQDN plus its subdomains — so `1977.alpha-tunnel.example.com`
and `3000.alpha-tunnel.example.com` share auth, but
`beta-tunnel.example.com` does not. That's the intended isolation
between tunnels.

But today `--public-hostname` is a free-form operator-supplied
string. An operator who set `--public-hostname=sweswe.com` (apex
only, no `{unique}-tunnel` prefix) would scope cookies to the
entire apex — Alice's session cookie would be sent to Bob's
tunnel. There is no CI / runtime check enforcing the FQDN shape.

The state-file path is shape-correct because it always carries
whatever `RegisterOK.Hostname` returned (server-assembled, always
full FQDN). But any human-set flag or env var is on the honor
system.

Removing `--public-hostname` and `SWE_PUBLIC_HOSTNAME` in this
pivot eliminates the footgun. The hostname comes only from the
live `register_ok` event whose payload is server-assigned. There
is no longer any path where an operator typo can set
`Cookie.Domain` to the apex.

### Tightened: broadcast on change, not on every status frame

Today `buildStatusPayload` includes `publicHostname` in every
status broadcast. That was a shortcut taken when the value was
static at boot — piggybacking on the existing broadcast was one
line of code, no new message kind. Cost is ~30 bytes per status
frame, immaterial individually but redundant.

Under the subprocess model the hostname is genuinely mutable, so
the right shape is:

- Send `publicHostname` once on WS connect (so newly-arriving
  browsers get the current value without waiting for a state
  change).
- Re-broadcast only when the supervisor receives a `register_ok`
  or `relabel` event from the child — i.e. when the value actually
  changes.
- Stop including `publicHostname` in unrelated status broadcasts
  (port allocation, agent state, etc.). Those are about session
  state, not server config.

Implementation: split out a small `broadcastPublicHostname()` that
the supervisor calls on event, and emit it once during the WS
handshake's initial state dump. Remove the field from the generic
`buildStatusPayload`.

Frontend already stores `this.publicHostname` and only refreshes
when it sees a value, so the change is server-side only — the
existing client-side handler keeps working unchanged.

### Concurrency / mutability

`serverPublicHostname` was a `var` set once during `main()`. After
this change it becomes a value protected by a small mutex (or, more
idiomatically, an `atomic.Value` holding a string), with a getter
used everywhere it is currently read. Reads on the hot path (every
`Session` creation, every `buildStatusPayload` call) must be
lock-free or near-lock-free.

Setter is called only by the supervisor goroutine; readers are the
HTTP / WS handlers. `atomic.Value` is the right tool.

## Compose / single-binary implications

The compose template change (Step 5 of the original plan) gets
simpler. Tunnel mode no longer requires a sidecar tunnel-client
service in `docker-compose.yml`. swe-swe-server itself is the parent
of the tunnel client; one container, one process tree.

What still needs to change in tunnel mode:

- Drop `traefik:` service.
- Bind swe-swe-server to `127.0.0.1:1977` via `--bind` (added in
  Step 3, already shipped on `main`).
- Skip LE volumes / certs / entrypoints.
- Preserve `{{PREVIEW_PORTS}}` etc. — the tunnel server still demuxes
  by leftmost label and forwards to those target ports.

The `--tunnel-server-url` flag becomes the trigger for "tunnel mode"
in the compose template (passed in via env from compose). Set ⇒
tunnel mode ⇒ no Traefik. Empty ⇒ legacy ⇒ existing template.

## Tests

Per the standing test mandate (extensive unit + e2e for every
feature):

### Unit (Go)

- New `tunnel_supervisor_test.go`:
  - **Event-stream parsing.** Feed a `bytes.Buffer` of newline-
    delimited JSON events, assert the supervisor produces the right
    sequence of `serverPublicHostname` updates and broadcasts.
  - **Restart on child exit.** Use a fake `exec.Cmd`-shaped struct or
    a tiny test helper subprocess that exits with a code on signal,
    assert the supervisor logs PID + status and respawns after the
    backoff.
  - **Graceful shutdown.** Send a context cancel, assert child got
    SIGTERM and the supervisor exits cleanly.
  - **Backoff cap.** Force fast crashes, assert the backoff caps at
    60s rather than growing unbounded.
- Update or remove `TestResolvePublicHostname` and the
  state-file-fallback table. `TestBuildStatusPayloadIncludesPublicHostname`
  stays.

### e2e (Playwright)

Extend `e2e/tests/tunnel.spec.js`:

- **Subprocess startup.** Boot a stack with `--tunnel-server-url=`
  pointed at a local fake tunnel server fixture (a tiny Go binary in
  `e2e/fakes/` that speaks just enough of the control protocol to
  reply with `register_ok`). Assert the iframe `src` ends up using
  the subdomain shape after the fake server announces a hostname.
- **Tunnel-client crash recovery.** Kill the tunnel-client child PID
  mid-test, assert swe-swe-server respawns it within the backoff
  window and the iframe `src` snaps back to subdomain shape after
  the new register.
- **No-tunnel-mode regression gate.** Boot with no `--tunnel-server-url`,
  assert legacy port-mode still works (this is just the existing
  pre-tunnel behavior — still an explicit gate so future changes
  cannot silently break it).

## Compatibility / migration

- **Existing deployments using `SWE_PUBLIC_HOSTNAME`** (env path):
  break on upgrade. The flag is removed. Document this in the
  release notes; provide the migration: replace
  `SWE_PUBLIC_HOSTNAME=foo.example.com` with
  `SWE_TUNNEL_SERVER_URL=https://tunnel.example.com` plus
  `SWE_TUNNEL_UNIQUE=foo`.
- **Existing deployments using the state file** (Step 4 path): same
  migration. The state file is no longer read.
- **Legacy port-mode (no tunnel)**: unaffected. Just don't set
  `--tunnel-server-url`.

## Open questions

1. **What does the tunnel client do during a network partition?**
   Today it presumably retries internally. The supervisor needs to
   know when the *registered* label is alive vs in-reconnect, so
   the WS frontend can show a "tunnel down" indicator. The event
   protocol design (companion file) needs a `disconnected` /
   `reconnecting` event type. Not blocking for v1 of this pivot,
   but worth a UI follow-up.
2. **Hot config reload.** If the operator changes the unique label
   on the live host, do we want to support that without a
   swe-swe-server restart? Probably not for v1 — restart is fine.
   Capture the answer in the supervisor design.
3. **Multiple tunnels.** Can a swe-swe instance be reachable through
   two tunnels at once (Cloudflare + sweswe-tunnel)? Out of scope
   for this task; `serverPublicHostname` is scalar.

## Sequencing

This pivot supersedes Steps 4 and parts of Step 5 in
`tasks/2026-04-27-swe-swe-tunnel-integration.md`. Order of work:

1. Land the swe-swe-tunnel companion plan (event protocol, opt-in
   flag) and ship it on the tunnel side. swe-swe is the consumer;
   the producer must exist first.
2. Once the tunnel client emits stable events, rip out the state-file
   path on the swe-swe side and add the supervisor.
3. Add the tunnel-mode compose template branch (Step 5 simplified —
   no traefik, bind localhost).
4. Update the `www/swe-swe-tunnel.md` docs page (Step 6) to describe
   the subprocess model.

## Coding rules to honor

- **No silent goroutine `cmd.Wait()`.** The supervisor logs PID +
  exit status on every child exit. (See `feedback_no_silent_wait.md`.)
- **ASCII only in code/markdown.** No em-dashes, smart quotes.
- **Direct commits on `main`**, no feature branch.
- **Memory rule (2026-03-07):** no per-request `http.Transport`. The
  supervisor uses the existing shared client only if it makes any
  HTTP calls itself; the tunnel client is a child process and owns
  its own HTTP state.
