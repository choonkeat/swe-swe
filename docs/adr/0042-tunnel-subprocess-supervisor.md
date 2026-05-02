# ADR-0042: Tunnel-mode Subprocess Supervisor

**Status**: Accepted
**Date**: 2026-04-30
**Research**: [tasks/2026-04-29-tunnel-subprocess-pivot.md](../../tasks/2026-04-29-tunnel-subprocess-pivot.md)

## Context

Tunnel mode (`swe-swe init --tunnel-server-url=...`) lets a swe-swe container be reached from the public internet without owning an IP, opening ports, or provisioning TLS. The container dials a `swe-swe-tunnel` server outbound, and the tunnel server fronts subdomain-routed traffic to the container over the same connection.

This requires the swe-swe-server inside the container to know its **live public hostname** — the subdomain assigned by the tunnel server at registration. URLs in the WebSocket broadcast, the subdomain iframes for preview / Agent View / VNC, and `Cookie.Domain` for cross-subdomain auth all depend on it.

The first shipped design (v1, commits `55dbfde43..638a58d9c`) coupled the two processes via a **state file**: the tunnel client wrote `/workspace/.swe-swe/tunnel-state.json` on `register_ok`, and swe-swe-server called `resolvePublicHostname` once during `main()` boot to read it.

This had an inter-process timing flaw. swe-swe-server reads the file exactly once. There is no watcher, no polling, no re-resolve. Boot orders therefore split into:

1. **Tunnel client first, then swe-swe-server.** State file already on disk, swe-swe-server reads it, all good.
2. **swe-swe-server first, then tunnel client.** State file missing at read time, swe-swe-server stays in legacy mode permanently. Tunnel client writing the file later does nothing. A swe-swe-server restart is required after the tunnel finishes registering.

Order 2 is the realistic case under most supervisors (compose, systemd unit ordering, fly.io machine boot). Even adding a watcher only papers over the bigger issue: **two opaque processes whose only communication channel is a file**. They cannot signal each other on disconnect, on label rotation, or on graceful shutdown. Re-registration with a new label cannot propagate without a server restart.

## Decision

Make swe-swe-server the **parent process** of the tunnel client. swe-swe-server exec's `swe-swe-tunnel` as a child and reads structured lifecycle events off its stdout as a JSONL event stream:

- `register_ok { publicHostname, ... }` — tunnel registered, hostname is live
- `disconnect { reason }` — connection dropped, supervisor restarts the child
- `fatal { reason }` — terminal failure (e.g. unauthorized pubkey), supervisor stops instead of restart-looping
- `retry-after { ms }` — backoff hint surfaced to the frontend

The supervisor:

1. Owns the live `publicHostname` value end-to-end and broadcasts it on the WebSocket on every change.
2. Restarts the child on `disconnect` with exponential backoff.
3. On `fatal`, stops the child and surfaces the reason — no restart loop.
4. On graceful swe-swe-server shutdown, signals the child to close cleanly.

Supersedes the state-file fallback approach shipped as Step 4 of `tasks/2026-04-27-swe-swe-tunnel-integration.md`. The state file, `--public-hostname`, `--tunnel-state-file`, and `resolvePublicHostname` were removed (commit `cd9893427`).

The `swe-swe-tunnel` binary is built into the container image at the pinned `SWE_SWE_TUNNEL_REF` build-arg ref (commit `5e1916a5c`); tunnel-side changes ship by bumping the pin.

## Consequences

Good:

- Boot order independence — the supervisor blocks on the child's `register_ok` event, no race.
- Real-time hostname updates — re-registration with a new label propagates without a swe-swe-server restart.
- Lifecycle observability — `disconnect`, `fatal`, and `retryAfterMs` flow from the child to the frontend without polling.
- Graceful shutdown coordination — parent/child relationship gives us SIGTERM propagation for free.
- Independent release cadence — tunnel-side changes ship by bumping `SWE_SWE_TUNNEL_REF`, no swe-swe rebuild needed for protocol-compatible changes.
- No file-based IPC — eliminates state-file corruption, stale-read, and permission concerns.

Bad:

- Two binaries to ship in the image (offset by the build-arg pin keeping it manageable).
- Supervisor adds a small amount of process-management complexity to swe-swe-server.
- The wire protocol between supervisor and child becomes a versioned interface — additions need to be backward-compatible until the pinned ref is rolled forward.

## Alternatives Considered

- **fsnotify file watcher in swe-swe-server.** Solves boot order 2 but retains the loose coupling. Adds a Go dependency and a goroutine. Does not solve disconnect/re-register propagation without further plumbing.
- **Polling the state file.** Same shape as fsnotify but uglier and slower. Same semantic limits.
- **Tunnel client as an in-process Go library.** Cleanest in code but requires moving `internal/tunnelclient` to a public package in the swe-swe-tunnel module, rebuilds swe-swe on every tunnel-side change, and pulls every tunnel transitive dep (yamux, ed25519, slog) into the swe-swe binary. Considered and rejected in favor of the subprocess approach for ops simplicity and independent release cadence.
- **Subprocess supervision (chosen).** Two binaries, swe-swe-server exec's the tunnel client and reads structured events off stdout. Tunnel-side changes ship independently. Lifecycle is observable. No build coupling.
