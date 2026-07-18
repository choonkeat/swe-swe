<!-- agent-chat export
title: Agent View Reverse Tunnel
date: 2026-07-18
index: 02
slug: agent-view-reverse-tunnel
agent: claude
version: 0.8.13 (65cde4c)
-->

# Agent View Reverse Tunnel

_2026-07-18 · 02 · claude · agent-chat 0.8.13 (65cde4c)_

**AGENT**

> Starting task: Agent View reverse tunnel (zero inbound ports on the swe-swe box). 5 phases, TDD throughout. Phase 1 first: stream mux over one WebSocket -- framing, interleaved streams, close propagation, backpressure -- tested over net.Pipe fakes. Reading existing browser-backend code now to match conventions.

<small>took 7m 43s</small><br>
**AGENT**

> Phase 1/5 DONE: stream mux (TDD, RED->GREEN).
>
> What happened: wrote 6 failing tests first (confirmed compile-fail RED), then implemented `agentview_tunnel_mux.go` -- the framing layer both tunnel ends share. Next: Phase 2, backend side (WS endpoint + declarative bind manager + peercred guard).
>
> Salient design bits:
> ```go
> // Data frames: 4-byte big-endian stream id + payload (WS binary)
> func encodeTunnelFrame(stream uint32, payload []byte) []byte
> // Control: {"op":"sync"|"sync-result"|"open"|"close",...} (WS text JSON)
> // Backpressure: 4 MiB per-stream receive buffer; overflow kills THAT
> // stream only (close frame + local error) -- no head-of-line blocking.
> var tunnelStreamBufMax = 4 << 20
> ```
> Tests cover: frame round-trip, spec-literal control JSON, interleaved streams, close propagation both directions, data-after-close dropped without panic, slow-stream overflow not blocking a fast stream.
>
> git stat since last report: 2 commits -- mux impl 388 lines + test 407 lines; goldens +47 files (the new template file replicated per variant, verified nothing else changed). Full `make test` EXIT=0.

<small>took 8m 54s</small><br>
**AGENT**

> Phase 2/5 backend side: all unit tests GREEN (RED->GREEN cycle logged). Declarative bind manager (bind/close/refuse reserved/first-bind-wins across sessions), WS endpoint /sessions/{id}/tunnel (401/404/409 + reconnect), full sync->bind->TCP accept->open frame->bidirectional data->DELETE teardown, and a real /proc-based TCP peer-PID guard (SO_PEERCRED does not exist for TCP, so: /proc/net/tcp inode -> /proc/*/fd scan -> PPid ancestry walk; fail-closed on Linux, verified v4+v6 self-connection).
>
> One deliberate deviation being logged, not hidden: the task's phase-2 verify says "curl that loopback port, see the open frame arrive" -- but the settled design's peercred guard REJECTS curl (not in the chromium tree). So the live check now running: real `swe-swe-server -mode browser-backend` + real chromium told via CDP to load the page (legitimate tree member -> open frame must arrive), and curl as the NEGATIVE check (guard must reject). Full `make test` EXIT=0. Now doing the live run.

<small>took 4m 3s</small><br>
**AGENT**

> Phase 2/5 DONE and LIVE-VERIFIED. Ran a real `swe-swe-server -mode browser-backend` with real Xvfb+chromium, drove it with a small Go tunnel client:
>
> - sync [46711] -> sync-result bound=[46711]
> - Told the allocated chromium via CDP to load http://localhost:46711/livecheck -> OPEN frame arrived, page served THROUGH the tunnel (chromium -> backend loopback -> WS -> dial-back to app server). PASS
> - Guard negative check: an outsider connection got closed with 0 bytes; backend log: "rejected connection on 46711: pid 60491 is not in the session's browser process tree". PASS
> - DELETE session -> tunnel port stopped accepting. PASS
>
> One deviation from the task text, logged not hidden: the plan said "curl the port, see the open frame" but the (settled-design) fail-closed peer guard rightly rejects curl -- so chromium-driven load is the positive check and curl is the negative. Noted in the task file + phase2 log.
>
> Salient diff: backend gained `handleTunnel` (WS upgrade, 409 on dup), `tunnelBindManager.reconcile` (bind/close/refuse: reserved | in-use | bind-failed), `findTCPPeerPID` (/proc/net/tcp inode -> /proc/*/fd -> PPid ancestry; SO_PEERCRED is unix-only so TCP needs the /proc walk).
>
> git stat: 2 commits this phase; server templates +4 files changed (~600 lines) + goldens x47 variants (verified: only the 4 expected files). Full `make test` EXIT=0.
>
> Next: Phase 3 -- client side (dial-out, dial-back, port sources incl /proc/net/tcp mirror).

<small>took 11m 47s</small><br>
**AGENT**

> Phase 3/5 DONE (client side, RED->GREEN). The swe-swe box now dials OUT and keeps the backend's loopback binds in sync.
>
> Salient bits:
> ```go
> // -agent-view-tunnel / SWE_AGENT_VIEW_TUNNEL=1 (only with -agent-view=<url>)
> // port set = static (server port + preview port + Procfile via swe-run math)
> //          + /proc/net/tcp{,6} mirror (LISTEN, loopback/wildcard, ~2s poll,
> //            excludes default to swe-swe's internal pools)
> // open frame -> dial 127.0.0.1:<port>, pipe; reconnect 1s..30s backoff + re-sync
> ```
>
> One-machine e2e PASSED (1.3s): app on 127.0.0.2:P, backend bound 127.0.0.1:P -- http.Get against the BACKEND loopback returned the app's response through the tunnel with the port discovered by the REAL /proc mirror (no static hint). Killed the tunnel server-side -> client reconnected, re-synced, served again. Declarative removal -> backend listener closed.
>
> Honest caveat (logged in task + phase3 log): the "kill the app, port leaves sync" check runs as static-clear-with-exclude because on ONE machine the mirror would re-discover the backend's own listener on the same port; production has two separate loopbacks. Same removal code path either way.
>
> git: 2 commits this phase (RED tests, then impl). New agentview_tunnel_client.go (430 lines) + wiring in browser_backend_remote.go/main.go; goldens x47 verified clean. Full `make test` EXIT=0.
>
> Next: Phase 4 -- chromium arg assertion + e2e tier proving no-inbound-route operation.

<small>took 5m 53s</small><br>
**AGENT**

> Phase 4/5 in progress. Unit part done RED->GREEN: extracted `buildChromiumArgs` -- empty resolver rules (tunnel mode) provably emit NO --host-resolver-rules flag. e2e wiring added: `E2E_AV_TUNNEL=1` tier in scripts/e2e-agent-view-remote.sh + spec branch (marker on instance loopback only, SWE_AGENT_VIEW_LOCALHOST pointed at a blackhole to prove the resolver path is unused, *.lvh.me Host-intact check, Procfile port pre-bind check) + Makefile targets (tunnel, tunnel-image) and test-full-e2e now includes the tunnel tier.
>
> Now running the live e2e tiers, slowest part of the task (each boots a real chromium stack + Playwright): 1) direct binary tier (must pass unchanged), 2) tunnel binary tier, 3) tunnel image tier (the genuine cross-netns no-inbound-route proof). Will report each result.

<small>took 2m 1s</small><br>
**AGENT**

> e2e status: DIRECT binary tier PASS (unchanged, 11.5s playwright), TUNNEL binary tier PASS ("backend saw the reverse tunnel connect"). The TUNNEL image tier (cross-netns no-inbound-route proof) is running in the background now -- it rebuilds the browser-backend docker image first, so this takes a few minutes. Will report the result when it lands.

<small>took 3m 12s</small><br>
**AGENT**

> Image tier hiccup + fix: the run died at a PRE-EXISTING infra gap -- `ip: command not found` in this dev container (the original script used `ip route` too, so the direct image tier was equally unrunnable here). Fixed with a portable /proc/net/route fallback (verified: resolves 172.18.0.1 here; unchanged where ip(8) exists). Re-running the tunnel image tier in the background now.

<small>took 3m 11s</small><br>
**AGENT**

> Phase 4/5 DONE -- the headline proof landed. All three e2e tiers PASS live:
>
> 1. direct binary tier: unchanged, still green (regression check)
> 2. tunnel binary tier: green + "backend saw the reverse tunnel connect"
> 3. tunnel IMAGE tier (the point of the whole task): chromium in the HOST netns rendered a marker page served ONLY on the instance container's loopback -- no route exists between them except the swe-swe-initiated WebSocket. SWE_AGENT_VIEW_LOCALHOST was deliberately set to a blackhole IP: had tunnel mode failed to drop --host-resolver-rules, every nav would have died. Also proven: app.lvh.me arrives Host-intact through the tunnel (vhost demux answers), and the Procfile-declared port was bound on the backend BEFORE anything listened on it (no mirror race), then served real content through the tunnel.
>
> Salient diff: `buildChromiumArgs` extracted + unit-tested (empty rules -> no resolver flag); e2e script/spec grew the E2E_AV_TUNNEL branch; Makefile: test-e2e-agent-view-remote-tunnel[-image], tunnel tier added to test-full-e2e. One infra fix: gateway lookup falls back to /proc/net/route when ip(8) is missing (pre-existing gap that also blocked the old image tier here).
>
> Attached: the noVNC view of the remote chromium showing the marker page loaded via http://localhost:42999 through the tunnel.
>
> git: 3 commits this phase. Full `make test` EXIT=0, goldens verified (browser_backend.go only). Next: Phase 5 -- docs + changelog + netns follow-up stub.
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-18-02-1-e04e9315c612.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-18-02-1-e04e9315c612.png" alt="04-remote-localhost-nav.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

<small>took 6m 47s</small><br>
**AGENT**

> All 5 phases are COMPLETE. As per the task protocol, let me walk you through each phase log, one at a time.
>
> **Phase 1 log (tasks/...-phase1.log) -- stream mux (TDD over net.Pipe)**
>
> What was done:
> - Logged the RED plan, wrote 6 tests FIRST (frame encode/decode round-trip, control JSON matching the spec's literal wire examples, interleaved streams, close propagation both directions, data-after-close dropped without panic, backpressure), confirmed the expected compile-failure RED, committed.
> - Implemented `agentview_tunnel_mux.go`: WS-text JSON control frames (sync / sync-result / open / close), WS-binary data frames with a 4-byte big-endian stream id, all over a tiny `tunnelMessageConn` interface that `*websocket.Conn` satisfies -- tests drive it with net.Pipe fakes, no real sockets.
> - Key design decision documented in-code: **4 MiB bounded per-stream receive buffer; overflow kills that one stream** (close frame + local error) instead of head-of-line-blocking every other stream behind the shared read loop or growing memory unboundedly.
>
> Problems encountered: only one, caught by me before running -- my data-after-close test originally mutated a callback field after the read loop started (data race); fixed by passing the callback at construction.
>
> Conclusion: all 6 tests GREEN, full `make test` EXIT=0, goldens showed exactly the new file across 47 variants and nothing else.
>
> OK to proceed to the phase 2 log?

[Quick replies]
- OK, next
- Show me the phase 1 log verbatim
- Questions first

**USER**

> OK, next

<small>took 14.8s</small><br>
**AGENT**

> **Phase 2 log -- backend side (tunnel endpoint + bind manager + peer guard)**
>
> What was done:
> - RED first: tests for the declarative bind manager (bind new + actually accepts, close removed, refuse reserved, refuse cross-session duplicate with first-bind-wins, release on teardown), the WS endpoint (401 no token / 404 unknown session / 409 second concurrent tunnel / reconnect allowed after close), a full integration (sync -> bound -> TCP dial -> open frame -> bytes both ways -> DELETE tears the listener down), and the Linux peer identification on a real TCP self-connection (v4 + v6).
> - Implemented: `handleTunnel` (WS upgrade per session), `tunnelBindManager.reconcile` (idempotent, sorted, refusal reasons "reserved"/"in-use"/"bind-failed"), and the peer guard. Notable: **SO_PEERCRED doesn't exist for TCP**, so the guard resolves the peer the /proc way -- match the conn's remote endpoint in /proc/net/tcp{,6} to a socket inode, scan /proc/*/fd for the owner pid, walk the PPid chain against the session's browser pids. Fail-closed on Linux, fail-open elsewhere (dev builds), mirroring the broker's stance.
> - LIVE verify with a REAL `-mode browser-backend` (real Xvfb+chromium): sync bound the port; chromium (told via CDP) loaded http://localhost:46711/livecheck -> open frame arrived and the page was served THROUGH the tunnel; an outsider connection got 0 bytes and the backend logged "pid 60491 is not in the session's browser process tree"; DELETE closed everything.
>
> Problems encountered / deviation (logged, not hidden):
> 1. The task text said "curl that loopback port, see the open frame arrive" -- but the settled design's fail-closed guard rightly REJECTS curl (not in the chromium tree). I replaced the positive check with a chromium-driven load (the production path) and kept curl as the negative guard check. This is recorded in the task file and the log.
> 2. First live run "failed" on a test-client artifact: chromium's own favicon.ico request arrived through the tunnel while the outsider check was sampling; fixed the checker to only flag the outsider's own path.
>
> Conclusion: all tests GREEN, live verify ALL PASS, full make test EXIT=0, goldens exactly the 4 expected files.
>
> OK to proceed to the phase 3 log?

[Quick replies]
- OK, next
- Explain the curl deviation more
- Show the live-check output

<small>took 2m 19s</small><br>
**AGENT**

> (Resending -- the previous blocking message got canceled by the barge-in; ignore the duplicate if you already read it.)
>
> **Phase 2 log -- backend side (tunnel endpoint + bind manager + peer guard)**
>
> - RED first: bind-manager reconcile tests (bind + really accepts, close removed, refuse reserved / cross-session dup with first-bind-wins, release on teardown), WS endpoint tests (401/404/409 + reconnect-after-close), full integration (sync -> bound -> TCP dial -> open frame -> bytes both ways -> DELETE teardown), and Linux peer-PID resolution on real TCP self-connections (v4+v6).
> - Implemented `handleTunnel`, `tunnelBindManager.reconcile`, and the guard. Notable: SO_PEERCRED doesn't exist for TCP, so the guard goes /proc/net/tcp inode -> /proc/*/fd owner pid -> PPid ancestry against the session's browser pids. Fail-closed on Linux.
> - LIVE verify with a real `-mode browser-backend` + real chromium: sync-result bound; chromium loaded the page THROUGH the tunnel (open frame observed); outsider connection got 0 bytes with the backend logging "pid ... is not in the session's browser process tree"; DELETE tore everything down.
>
> Problems / deviation (logged in task + log, not hidden):
> 1. Task text said "curl the port, see the open frame" -- the fail-closed guard rightly rejects curl, so the positive check became a chromium-driven load (production path) and curl became the negative guard check.
> 2. First live run tripped on chromium's own favicon.ico arriving during the outsider check -- test-client artifact, fixed.
>
> Conclusion: all GREEN, live ALL PASS, make test EXIT=0, goldens = exactly the 4 expected files.
>
> OK to proceed to the phase 3 log?

[Quick replies]
- OK, next
- Explain the curl deviation more
- Show the live-check output

**USER**

> OK, next

<small>took 14.9s</small><br>
**AGENT**

> **Phase 3 log -- client side (dial-out, port sources, reconnect)**
>
> What was done:
> - RED first: fixture tests for the /proc/net/tcp{,6} parser (hex little-endian addresses, LISTEN-state only, loopback + wildcard only, specific non-loopback IPs skipped, garbage-safe), exclude-list CSV parsing (ports + lo-hi ranges), default excludes covering swe-swe's internal pools but NOT preview/dev ports, Procfile port math matching swe-run exactly (web-primary gets the base, i-th other gets base+5000+i*20), alloc payload (tunnel:true + resolver envs ignored), refusal warn-once, and a one-machine e2e.
> - Implemented `agentview_tunnel_client.go` + wiring: the `-agent-view-tunnel` flag / SWE_AGENT_VIEW_TUNNEL=1, per-session client that dials out, unions static + Procfile + mirror ports, syncs on change (~2s cadence), dials back 127.0.0.1:<port> on open frames, 30s ping liveness, 1s..30s capped reconnect backoff with full re-sync. Tunnel death never kills the session. Also deduplicated the hex-address parser shared with the phase-2 peer guard.
> - One-machine e2e (now a permanent unit test, 1.3s): app on 127.0.0.2:P so the backend could bind 127.0.0.1:P on the SAME machine. The REAL /proc mirror discovered P with no static hint; http.Get against the BACKEND's loopback returned the app's response through the tunnel; a server-side tunnel kill was recovered by the client (reconnect + re-sync + serves again); declarative removal closed the backend listener.
>
> Problems encountered:
> - First e2e run failed: after killing the app, the mirror re-discovered the BACKEND'S OWN 127.0.0.1:P listener and kept re-syncing it -- a pure one-machine artifact (production has two separate loopbacks). Restructured honestly into two parts: mirror-driven discovery (real /proc), then removal driven via static-clear with the port excluded from the mirror. The removal code path exercised is identical; the caveat is recorded in the task file and the log.
>
> Conclusion: all GREEN, full make test EXIT=0, goldens = client file + 3 expected edits x47.
>
> OK to proceed to the phase 4 log?

[Quick replies]
- OK, next
- Why 127.0.0.2?
- Show the phase 3 log verbatim

**USER**

> OK, next

<small>took 14.2s</small><br>
**AGENT**

> **Phase 4 log -- chromium wiring + e2e proving the point**
>
> What was done:
> - RED->GREEN unit: extracted `buildChromiumArgs` and asserted empty resolver rules (tunnel allocations) emit NO --host-resolver-rules flag; non-empty passes verbatim; core flags intact.
> - e2e: new `E2E_AV_TUNNEL=1` tier in scripts/e2e-agent-view-remote.sh + a tunnel branch in the Playwright spec. Design choices that make it a real proof: SWE_AGENT_VIEW_LOCALHOST deliberately points at a TEST-NET blackhole (if tunnel mode failed to drop resolver rules, every nav would die); the marker page binds the INSTANCE netns loopback only; *.lvh.me nav asserts Host-intact vhost routing; a Procfile declares an api service whose port (47000) must be bound on the backend BEFORE anything listens on it (nav error must be anything but CONNECTION_REFUSED), then an in-spec listener serves real content through the tunnel. New make targets test-e2e-agent-view-remote-tunnel[-image]; tunnel tier added to test-full-e2e.
> - Live runs, all three tiers:
>   1. direct binary tier: PASS unchanged (regression gate)
>   2. tunnel binary tier: PASS + "backend saw the reverse tunnel connect" (procfile content half auto-skips there -- backend holds the port on the shared loopback -- documented in-spec)
>   3. tunnel IMAGE tier: PASS -- the headline: chromium in the HOST netns rendered the marker served ONLY on the container-netns loopback. Zero inbound route; the page rendered anyway. You saw the screenshot earlier.
>
> Problems encountered:
> - The image tier first died on `ip: command not found` -- a PRE-EXISTING gap (the original script used `ip route` too, so the old direct image tier was equally unrunnable in this dev container). Fixed with a /proc/net/route fallback (verified it resolves 172.18.0.1 here); behavior unchanged where ip(8) exists. Logged as a redo per protocol.
>
> Conclusion: all three tiers PASS live, make test EXIT=0, goldens = browser_backend.go only.
>
> OK to proceed to the phase 5 log (last one)?

[Quick replies]
- OK, next
- Show the tunnel-image e2e output
- Questions about the blackhole trick

**USER**

> OK, next

<small>took 16.0s</small><br>
**AGENT**

> **Phase 5 log -- docs + changelog (last one)**
>
> What was done:
> - `docs/dockerless.md`: Option B gained the "Tunnel variant -- your box can stay fully firewalled" section: the `--agent-view-tunnel` flag, the trust-direction story (swe-swe connects out, same as swe-swe-tunnel), and the full port-source story (static / Procfile pre-bind / ~2s auto-mirror with the exclude env), plus the loud-refusal, peer-guard, and reconnect semantics.
> - `tasks/2026-06-27-dockerless-single-binary.md`: Phase 5 entry gained a "Follow-up SHIPPED 2026-07-18" pointer to this task.
> - New `tasks/TODO-agent-view-netns-multitenancy.md`: the netns follow-up stub -- per-session network namespaces on multi-tenant backends (private loopback per chromium kills cross-tenant port collisions AND upgrades the peercred guard to defense-in-depth; needs CAP_NET_ADMIN; sync protocol unchanged), with an acceptance sketch.
> - CHANGELOG: one comprehensive Unreleased feature entry for the reverse tunnel.
>
> Problems encountered: none. Docs-only phase, so no TDD cycle -- logged that explicitly; verification was `make test` EXIT=0 (which includes the ascii-check lint over all the new markdown) and confirming zero golden drift.
>
> Conclusion: committed as phase 5/5; the task file is marked COMPLETE.
>
> That's all five logs. Remaining wrap-up: export this chat to markdown (agent-chats/) and commit it, then I'll give you the final git status. Proceed?

[Quick replies]
- Yes, wrap up
- Hold on, questions first

**USER**

> Yes, wrap up

