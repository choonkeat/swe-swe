# Agent View remote: re-allocate after backend restart

## Problem (observed live 2026-07-18)

A `swe-swe-browser-backend` restart (chromium bump, config change, crash)
orphans every LIVE session's Agent View: the allocation table is in-memory,
so the new backend knows nothing about existing sessions.

Observed on the dev box after recreating the backend container:

- Session `e18603ce`'s tunnel client reconnect-loops forever:
  `disconnected (dial: websocket: bad handshake); reconnecting in 16s`
  (the tunnel endpoint `/sessions/<id>/tunnel` 404s for an unknown
  allocation -> gorilla reports "bad handshake").
- The playwright MCP gets `Target page, context or browser has been closed`.
- Nothing re-allocates until session end (`stopSessionAgentView` is only
  called from session teardown, main.go:1371). New sessions are fine.

`handleBrowserStartAPI` is idempotent ("already started") keyed on
`sess.BrowserStarted`, which stays true -- so even a manual re-open of the
Agent View tab does not heal the session.

## Root cause

`startRemoteAgentView` runs exactly once per session. All remote state is
captured at that moment:

- `wireRemoteSession` closure captures `remoteCDP` (host:port) inside the
  CDP reverse proxy Director/ModifyResponse (browser_backend_remote.go).
- `sess.RemoteVNCTarget` is a plain field (read per-request in main.go
  ~5546 and ~8718, so an update WOULD take effect on the next connection).
- The tunnel client (`agentview_tunnel_client.go` run loop, ~line 353)
  retries the dial with capped backoff but treats every error the same --
  it cannot distinguish "backend briefly down" (retry is right) from
  "backend up but allocation gone" (retry can never succeed).

## Fix sketch

1. **Classify the tunnel dial failure.** In the tunnel client's dial path,
   capture the HTTP response of the failed websocket upgrade
   (`websocket.DefaultDialer.Dial` returns `(conn, resp, err)`). A 404 (or
   403) from `/sessions/<id>/tunnel` means "allocation gone" -> surface it
   via a callback (`onAllocationLost func()`) instead of retrying blind.
   Keep plain retry for network errors / 5xx.
2. **Re-allocate.** The callback re-runs the allocation half of
   `startRemoteAgentView` for the session:
   - POST /sessions again (same sessionId; after a restart there is no
     duplicate, and a 409 means someone else re-allocated -- treat as
     success after a GET, or just log and stop).
   - New `alloc.CDPPort`/`VNCPort` may DIFFER (slot reuse), so:
     - update `sess.RemoteVNCTarget` (per-request reads pick it up),
     - replace the CDP proxy: keep the listener on `sess.CDPPort` but make
       the proxy target an atomically-swappable value instead of a closure
       capture (e.g. `atomic.Pointer[string]` read inside Director), so no
       listener churn,
     - restart the tunnel client (Stop + start) with the fresh allocation;
       its declarative sync re-establishes the port set.
   - Broadcast session status so the Browser tab / UI recovers.
3. **Locking care:** the callback runs from the tunnel goroutine; take the
   same locks `startSessionAgentView` takes and nothing more. Mind the
   Close/s.mu re-lock hazard from the end-session deadlock fix (3f3fb88f9)
   -- do not call back into session teardown paths while holding s.mu.
   Guard against racing session teardown (check a "closing" flag under
   lock before re-allocating; teardown Stop()s the tunnel client first,
   which must also cancel a pending re-allocate).
4. **Backoff:** re-allocation itself needs capped backoff (backend may
   still be restarting); reuse the tunnel client's backoff constants.

## Non-tunnel mode (follow-up, lower priority)

Direct (non-tunnel) remote mode has the same orphaning but no reconnect
loop to hook. Options: a lightweight allocation health probe (GET
/sessions/<id> on CDP proxy connection-refused), or re-allocate lazily
when the CDP proxy sees N consecutive dial failures. Out of scope for the
first cut; tunnel mode is what the dev box runs.

## Test plan

- Unit: fake backend that 404s the tunnel endpoint after first connect ->
  assert re-allocate POST happens, proxy target swaps, tunnel reconnects
  (extend agentview_tunnel_backend_test.go harness).
- Unit: teardown during pending re-allocate -> no leak, no deadlock.
- Live: `docker rm -f swe-swe-browser-backend && docker run ...` mid-session,
  then drive the session's playwright MCP -- browser comes back without
  ending the session.
