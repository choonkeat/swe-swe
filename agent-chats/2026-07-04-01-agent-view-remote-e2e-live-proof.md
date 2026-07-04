<!-- agent-chat export
title: Agent View Remote E2e Live Proof
date: 2026-07-04
index: 01
slug: agent-view-remote-e2e-live-proof
agent: claude
version: dev (unknown)
-->

# Agent View Remote E2e Live Proof

_2026-07-04 · 01 · claude · agent-chat dev (unknown)_

**USER**

> /ck:resume-session

<small>took 41.1s</small><br>
**AGENT**

> Found TWO handoff files — this is a stale-resume warning.
>
> **Fresh `.swe-swe/TODO.md`** (Jul 3, 23:25):
> - Goal: verify the `swe-swe-preview` MCP proxy completes the `initialize` handshake instead of hanging — the LAST open thread of the mcp-less fleet rollout.
> - The preview fix is already COMMITTED (`8ddc8bcf7`, `ecc18b26d`) and we rebooted so the running server is rebuilt from it.
> - Next step: re-dogfood `mcp swe-swe-preview` (previously hung 60-120s; expect a tool list in seconds now).
>
> **Old `.swe-swe/TODO.resumed.md`** (May 23): describes the preview-auth BUG *before* it was fixed, plus a separate skills feature (already shipped in v2.24.0). Both are done — this file is stale.
>
> My read: the fresh TODO.md supersedes the old one. I'd **discard TODO.resumed.md** and proceed with the fresh TODO.md — verify the preview proxy live via the `mcp` CLI.
>
> Proceed that way?

[Quick replies]
- Yes, discard old + verify preview
- Show me more detail first
- Abort, I will clean up manually

**USER**

> Yes, discard old + verify preview

<small>took 28m 25s</small><br>
**AGENT**

> ✅ **Preview proxy fix confirmed — last mcp-less thread closed.**
>
> Ran the full verification from the handoff:
>
> 1. **Static** — `auth.go:567` has the `const suffix = "/preview/mcp"` key-exemption. Fix is in the running binary.
> 2. **`mcp swe-swe-preview`** — returned its 7 tools in **8ms**. The MCP `initialize` handshake completes; the old 60-120s hang is gone. (This was THE test.)
> 3. **Real tool call** — `preview_browser_evaluate` routed through the proxy and returned a proper tool-level result ("No response from preview... Is the app running?"). That message comes from the preview handler *after* it was authorized and dispatched — the old bug would have 302'd at the transport layer and never reached it. The "no app running" is orthogonal (no preview app loaded this session).
> 4. **Raw curl** on `/proxy/<uuid>/preview/mcp` (no key) → **401**, not the old **302 → /swe-swe-auth/login**. Confirms the path now hits the key-exempt handler.
>
> **Result:** all 5/5 mcp-less proxies verified live (agent-chat, swe-swe, playwright, whiteboard, preview). The fleet rollout is complete.
>
> Housekeeping: discarded the stale May-23 `TODO.resumed.md`; the fresh handoff is now the breadcrumb at `.swe-swe/TODO.resumed.md`. Nothing to commit — this was verification of already-committed work (`8ddc8bcf7`, `ecc18b26d`).
>
> Anything else?

[Quick replies]
- That's all, thanks
- Load a preview app and re-test end-to-end
- Show me the auth.go exemption code

**USER**

> Is dockerless worktree ready to merge into here (main)?

<small>took 20m 0s</small><br>
**AGENT**

> **Short answer: not cleanly, and not fully verified — I would not merge it as-is.** Two separate issues:
>
> **1. Merge mechanics (fixable):**
> - `dockerless` is **43 ahead / 54 behind** main — main absorbed the entire mcp-less rollout after dockerless branched, so the two big features have never been tested together.
> - A merge dry-run hits **3 real source conflicts**: `Makefile`, `cmd/swe-swe/init.go`, `cmd/swe-swe/main_test.go` (both branches added init flags/config), plus the usual golden churn (auto-regenerate those).
>
> **2. Readiness of the branch itself:**
> - Phases 0-5 are ✅ done + Linux-tested (static payload, dockerless init/up, tunnel, pluggable browser backend, e2e harness).
> - **Phase 6 (Mac-native) is `[~]` — code-complete but PENDING LIVE MAC VERIFICATION.** That's the defining feature of the branch (no-Docker matters most on macOS) and it has never been run on a Mac. Also still open on macOS: full broker (currently disabled → git falls back to normal creds) and Agent View (degrades / needs remote backend).
>
> **So it depends on your goal:**
> - Ship **Linux-dockerless now**, Mac as a follow-up → feasible: reconcile with main, resolve the 3 conflicts, regen goldens, get `make test` green (first mcp-less+dockerless run), leave Phase 6 marked pending. I can prep that merge.
> - Ship **Mac-dockerless too** → NOT ready; needs the live Mac run first (build CLI → init --dockerless → up → exercise all 6 tabs), which I can't do from this Linux box.
>
> Which way do you want to go?

[Quick replies]
- Prep the merge (Linux ready, Mac as follow-up)
- Hold until Mac is verified live
- Just show me the 3 conflicts
- Merge it all now anyway

**USER**

> Before i answer, do we have Dockerfile for running chrome+novnc et al for dockerless to connect to — have we tested with linux dockerless swe-swe?

<small>took 1m 2s</small><br>
**AGENT**

> **Dockerfile: yes. Full loop tested with Linux dockerless: no.**
>
> **What exists** (Phase 5e, on the `dockerless` branch):
> - `docker/browser-backend/Dockerfile` + README + `make browser-backend-image` — debian-slim with ONLY the display stack (chromium, Xvfb, x11vnc, noVNC, websockify) plus the same static `swe-swe-server` run as `-mode browser-backend`.
> - Usage: `docker run -p 9333:9333 -p 6000-6019 -p 7000-7039 -e SWE_BROWSER_BACKEND_TOKEN=...` then `swe-swe up --agent-view=https://browser-box:9333`.
>
> **What was actually tested:**
> - ✅ Backend service **live** — but via the raw payload binary on this box, NOT the image: /health 200, bearer auth (no-token=401), POST /sessions really spawned Xvfb + Chromium CDP. (x11vnc/noVNC died only from dogfood-box port collisions.)
> - ✅ Remote client wiring (allocation, CDP reverse-proxy host rewrite, VNC redirect) — unit/httptest level.
> - ✅ Linux dockerless e2e: 18 PASS — but **Agent View explicitly out of scope**; only graceful degradation was covered (lean host → tab hidden).
> - ❌ **Docker image never built** (avoided the production daemon on this box; COPY sources only checked to exist).
> - ❌ **The loop you're asking about — dockerless swe-swe → `--agent-view=<url>` → backend container → Agent View tab live — has NEVER run.** It's the documented follow-up ('live cross-host remote e2e').
>
> **Two known functional gaps in remote mode** (documented, unfixed):
> 1. `vnc-ready` probes the LOCAL VNC port — wrong when the browser is remote.
> 2. Chromium on the backend resolves `localhost:3000` to the backend box, not your swe-swe host — the `--host-resolver-rules` fix is still a TODO. So watching the agent browse your dev server would break.
>
> I can close the gap now: build the image, run it on this box (non-default ports to dodge the dogfood collisions), boot a dockerless instance with `--agent-view` at it, and drive the tab via browser MCP. Want me to?

[Quick replies]
- Yes, run the missing e2e now
- No — back to the merge question
- Fix the 2 remote-mode gaps first

**USER**

> Defer Mac. We have this full e2e (maybe `make e2e-dockerless` ?) for linux before we think about Mac. Plan and discuss

<small>took 1m 39s</small><br>
**AGENT**

> Agreed — defer Mac, close the Linux loop first. Here's the plan, with the decisions I want your input on.
>
> **Current harness:** `make test-e2e-dockerless` = `scripts/e2e-dockerless.sh` (init --dockerless → boot server, curl contract asserts) + `e2e/tests/dockerless-tabs.spec.js` (Playwright: WS, Files, Preview). Agent View is explicitly out of scope today.
>
> **Plan — 3 phases on the `dockerless` branch:**
>
> **Phase A — fix the remote `vnc-ready` gap (blocker).** Today `/api/session/{uuid}/vnc-ready` probes the LOCAL VNC port; with a remote backend it never turns ready, so the e2e would fail before it starts. Fix: when the session has a remote backend, consult the backend's `GET /sessions/{id}/ready` instead. Unit-test both paths. ~small diff in `main.go`/`browser_backend_remote.go`.
>
> **Phase B — the missing e2e, two tiers:**
> 1. `e2e-agent-view-remote` (binary tier, no Docker): launch `swe-swe-server -mode browser-backend` directly on high ports, boot the dockerless instance with `--agent-view=http://127.0.0.1:<port>` + shared token, then Playwright: Agent View tab **visible** (symmetric to today's hidden-when-lean), click it, noVNC canvas renders, backend `/health` shows sessions:1, close session → DELETE frees it.
> 2. `e2e-agent-view-remote-image` (image tier): same, but the backend is the real `make browser-backend-image` container — finally exercising the Dockerfile (apt display stack + COPY'd static binary). Ports: run container with `-e SWE_CDP_PORTS/SWE_VNC_PORTS` on high ranges published 1:1 (service reads the same env as the main server — verified), so no dogfood collisions.
> Tier 1 can run on Docker-free CI; tier 2 wants Docker (fine on this box — but it's an apt-heavy build, few hundred MB on the production daemon; OK?).
>
> **Phase C — `--host-resolver-rules` (the localhost gap):** chromium on the backend resolves `localhost:3000` to the backend box, so watching the agent browse your dev server breaks. Options: (a) fix now + assert in the e2e that the remote browser loads a page served FROM the swe-swe host — that's the real proof; (b) defer, scope e2e to browser-up/VNC/CDP. **I'd do (a)** — without it 'Agent View works remotely' is hollow.
>
> **Sequence:** A → B1 → C → B2, wire into `test-e2e-dockerless` umbrella, THEN return to the main-merge question with a fully-proven branch.
>
> Two questions: (1) OK to build/run the backend image on this box's Docker? (2) Phase C now (my vote) or defer?

[Quick replies]
- Approve: A, B1, C, B2 — image build OK
- Defer C, minimal loop first
- Binary tier only, skip the image
- Let me adjust the plan

**USER**

> > but it's an apt-heavy build, few hundred MB on the production daemon; OK?
>
> You can docker system df and prune it should be ok
>
> Question before we proceed: if we have 1x url for docker chrome, how do we have independent browsers for each swe-swe session? Is it still lazy init like current?

<small>took 44.2s</small><br>
**AGENT**

> **Yes — still lazy, and each session gets its own isolated browser.** The one URL is an *allocator*, not a single browser. Verified in code:
>
> **Lazy init (unchanged):** nothing starts when a session opens. When the user first opens the Agent View tab, the UI hits `POST /api/session/{uuid}/browser/start` exactly like today → `startSessionAgentView` → in remote mode it POSTs `/sessions` to the backend (`browser_backend.go:106`). No tab click, no browser.
>
> **Independent browsers per session:** each POST allocates a **slot** on the backend, and every slot is a full isolated stack:
> - own X display (`:10+slot`)
> - own chromium with its own `--user-data-dir` profile (no shared cookies/state)
> - own CDP port (`cdpStart+slot`) and own VNC port + x11vnc/noVNC chain
>
> So swe-swe session A → cdp 6000/vnc 7000, session B → 6001/7001, etc. Your swe-swe host then wires each session's local CDP reverse-proxy + VNC proxy to its own allocated pair — the tabs look identical to local mode.
>
> **Lifecycle:**
> - **Idempotent** per swe-swe session UUID — a reconnect re-POSTs the same id and gets the SAME live instance back (no leak, no second browser).
> - **Close** → swe-swe DELETEs `/sessions/{id}` → backend reaps the whole process tree, slot freed.
> - **Capacity** = VNC range size (default 20; `-browser-backend-max` to override). Full → 503 → surfaces as a tab error, not a hang.
>
> One small edge bug I spotted while reading: the capacity check runs BEFORE the idempotency check, so a re-POST for an *already-running* id while the pool is full gets a wrong 503 instead of its existing instance. Two-line fix — I'll fold it into Phase B with a unit test.
>
> That answers it — proceed with the plan (A → B1 → C → B2, image build OK per your df/prune note)?

[Quick replies]
- Yes, proceed
- One more question first
- Adjust the plan

**USER**

> Yes proceed. Screenshots of swe-swe ui in various stages as progress please

<small>took 31.5s</small><br>
**AGENT**

> Starting Phase A: routing the vnc-ready probe to the remote backend for remote sessions (+ the capacity/idempotency edge fix). Screenshots will come once the e2e phases boot real UI.

<small>took 6m 24s</small><br>
**AGENT**

> Phase A done ✅ (commit 92dade39e on dockerless): vnc-ready now probes the remote websockify target for remote sessions (was a permanent 503 — sessions never turned ready), plus the idempotency-before-capacity fix. 5 new unit tests, make test green, goldens regenerated.
>
> Now Phase B1: building the binary-tier e2e — backend service on high ports + dockerless instance with --agent-view pointed at it. Screenshots coming once the UI is up.

<small>took 4m 33s</small><br>
**AGENT**

> Phase B1 harness written and running: scripts/e2e-agent-view-remote.sh + e2e/tests/agent-view-remote.spec.js. Flow: dumped swe-swe-server as browser-backend on :19844 → dockerless instance on :19833 with SWE_AGENT_VIEW pointed at it → Playwright types the real agent trigger (curl browser/start with $MCP_AUTH_KEY) into the shell PTY → asserts backend allocation, vnc-ready 200 (the Phase A fix), and the noVNC canvas rendering. Screenshots being captured at 3 stages — will share when the run lands.

<small>took 2m 12s</small><br>
**AGENT**

> Phase B1 PASSED ✅ — the full remote Agent View loop works end-to-end (7.1s Playwright run):
>
> 1. Backend allocated exactly 1 browser for the session (/health 0→1)
> 2. vnc-ready flipped 200 probing the REMOTE websockify — Phase A fix proven live
> 3. noVNC canvas rendered the remote chromium in the Agent View tab — see screenshot 3: 'Connected to <backend>:10', DuckDuckGo start page, View only/Interactive toggle all working
>
> The lazy-start was triggered the real way: typed the mcp-lazy-init curl (browser/start + $MCP_AUTH_KEY) into the shell PTY.
>
> Screenshots: (1) session before start, (2) browserStarted pushed over WS + pane auto-added, (3) remote chromium live in the tab.
>
> Committing B1, then Phase C (--host-resolver-rules so the remote browser can reach YOUR dev server's localhost).
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-04-01-1-4f2ef096bafc.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-04-01-1-4f2ef096bafc.png" alt="01-session-before-start.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> <a href="./assets/2026-07-04-01-2-c6dbb98c92f9.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-04-01-2-c6dbb98c92f9.png" alt="02-browser-started.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> <a href="./assets/2026-07-04-01-3-958b1b75b99a.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-04-01-3-958b1b75b99a.png" alt="03-agent-view-canvas.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

<small>took 5m 39s</small><br>
**AGENT**

> Phase C implemented: the backend now passes --host-resolver-rules='MAP localhost <swe-swe-host>' to chromium — target auto-derived from the allocation request's source IP (override via SWE_AGENT_VIEW_LOCALHOST for NAT). Unit tests green (derive + override). The e2e spec gained stage 4: connect over CDP through the session proxy, navigate the REMOTE chromium to http://localhost:<host-port>, assert the swe-swe homepage loads — trivial on same-host binary tier, the real proof lands on the image tier next. Binary-tier re-run in flight.

<small>took 3m 11s</small><br>
**AGENT**

> Phase C committed (7ef7de183) — binary-tier e2e re-passed with the new CDP-driven localhost-nav stage (9.4s). Now Phase B2: building the browser-backend Docker image for the FIRST time (apt display stack, ~few min) and running the e2e against the container. This tier is where localhost-resolution gets its genuine cross-namespace proof: the remote chromium (in-container) must load a marker page served on the docker bridge via http://localhost:42999 — impossible without the --host-resolver-rules mapping. (Disk is fine: 35GB reclaimable per docker system df.)

<small>took 9m 15s</small><br>
**AGENT**

> B2 debug found a REAL product bug the image tier exposed: '-mode browser-backend' dispatched before the SWE_CDP_PORTS/SWE_VNC_PORTS env parsing in main(), so the service silently ignored configured port ranges and allocated defaults (6000/7000) — B1 only passed by luck (those happened to be free here). Fixed by hoisting the range parsing above the dispatch; harness now also asserts the allocation lands on the configured range. Also switched the backend container to --network=host (published-loopback ports were unreachable from this containerized dogfood harness). make test green; full image-tier run (with the first-ever Dockerfile build) in flight — full log kept this time.

<small>took 5m 19s</small><br>
**AGENT**

> SECOND product bug found by the image tier: headful chromium silently IGNORES --remote-debugging-address=0.0.0.0 and always binds CDP to loopback — so a remote backend's CDP was never reachable cross-host in ANY deployment (B1 masked it: same network namespace). Fix mirrors the existing VNC internal/external split: chromium now listens on an internal loopback port and a reverse-proxy forwarder serves the public CDP port, rewriting /json discovery bodies so debugger URLs stay consistent. Applies to both local and service mode; teardown closes the forwarder. make test green; image-tier e2e re-running (image rebuild + full loop).

<small>took 7m 13s</small><br>
**AGENT**

> 🎉 **All 4 phases done — the full Agent View remote loop is proven live on Linux, both tiers.**
>
> **4 commits on `dockerless`:**
> - `92dade39e` Phase A: remote-aware vnc-ready + idempotency-before-capacity (+5 unit tests)
> - `afe830e93` Phase B1: the e2e harness + Playwright spec (binary tier PASS)
> - `7ef7de183` Phase C: --host-resolver-rules maps chromium's localhost → swe-swe host (auto from allocation source IP; SWE_AGENT_VIEW_LOCALHOST override)
> - `4df224aed` Phase B2: image tier PASS + **two real product bugs it caught & fixed**:
>   1. `-mode browser-backend` dispatched BEFORE main() parsed SWE_CDP_PORTS/SWE_VNC_PORTS → configured ranges silently ignored
>   2. headful chromium IGNORES --remote-debugging-address → CDP was loopback-only, never reachable cross-host in ANY deployment. Fixed with a per-session CDP reverse-proxy forwarder (mirrors the x11vnc/websockify split)
>
> **Final state:** `make test-e2e-agent-view-remote` (binary, no Docker) and `-image` (real Dockerfile — its first build ever) both PASS in ~10s. Screenshot 2 attached is the money shot: chromium **inside the backend container** at `localhost:42999` rendering the marker page served from the swe-swe side — cross-namespace localhost proof. `make test` green throughout, goldens updated, README + plan doc updated (Phase 5 follow-ups all closed).
>
> **Where this leaves the merge question:** the Linux dockerless story is now fully verified end-to-end incl. remote Agent View. Remaining before merging to main: reconcile the 54-behind gap (3 source conflicts: Makefile, init.go, main_test.go) + golden regen + first combined mcp-less+dockerless test run. Mac stays deferred as agreed.
>
> Want me to prep that merge now?
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-04-01-4-d1e0bdea0dc9.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-04-01-4-d1e0bdea0dc9.png" alt="03-agent-view-canvas.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> <a href="./assets/2026-07-04-01-5-bdc219299e71.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-04-01-5-bdc219299e71.png" alt="04-remote-localhost-nav.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

[Quick replies]
- Yes, prep the merge
- Push dockerless first
- Not now

**USER**

> I need *lvh.me and similar wildcard domains to work too — not just localhost

<small>took 44.3s</small><br>
**AGENT**

> On it. Extending the mapping to a wildcard rule set: default domains localhost, *.localhost, lvh.me, *.lvh.me, localtest.me, *.localtest.me → all MAP'd to the swe-swe host; per-session override via SWE_AGENT_VIEW_LOOPBACK_DOMAINS (comma-separated) for other wildcard DNS schemes. (Deliberately NOT mapping *.nip.io/*.sslip.io wholesale — those encode arbitrary IPs like 10.0.0.5.nip.io that must keep resolving normally.) The e2e gains an app.lvh.me navigation assert — DNS-free, pure mapping proof. Building now.

<small>took 6m 22s</small><br>
**AGENT**

> ✅ **Wildcard loopback domains shipped** (`158b798ba` on `dockerless`) — both e2e tiers PASS live.
>
> **What changed:**
> - The single `MAP localhost` rule is now a **rule set over a domain list**. Defaults: `localhost`, `lvh.me`, `localtest.me` — each mapped bare **and** `*.` wildcard, so `tenant1.lvh.me:3000`, `app.localtest.me`, `foo.localhost` all reach your dev server on the swe-swe host.
> - **Override per-project:** `SWE_AGENT_VIEW_LOOPBACK_DOMAINS=myapp.test,lvh.me` (comma-separated, swe-swe side) or `loopbackDomains` array on `POST /sessions`. Caller wildcards normalize (`*.myapp.test` → bare + wildcard).
> - Deliberately **NOT** `*.nip.io`/`*.sslip.io` — those encode arbitrary IPs (`10.0.0.5.nip.io`) that must keep resolving normally. Add specific ones via the override if you need them.
> - MAP rules bypass real DNS entirely, so these domains work in the remote browser even offline.
>
> **Proof:** screenshot attached — chromium **inside the backend container** at `app.lvh.me:42999` rendering the marker page served from the swe-swe side. e2e stage 5 asserts this on every run; unit tests cover the rule builder + body/env overrides. `make test` green, goldens updated.
>
> **`dockerless` branch now:** 48 commits ahead, all Linux e2e proven (6 tabs + remote Agent View both tiers + wildcard domains). Back to the pending question: prep the merge into main?
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-04-01-6-816cb81fd76f.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-04-01-6-816cb81fd76f.png" alt="05-remote-lvh-me-nav.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

[Quick replies]
- Yes, prep the merge
- Push dockerless first
- Something else first

**USER**

> Yes, prep the merge

**USER**

> Stop

**USER**

> /ck:export-chat-logs

<small>took 13.2s</small><br>
**AGENT**

> Ready to prep the merge. Here's my plan — confirm the two open choices before I start:
>
> **Plan (safest ordering, keeps your live main checkout untouched until the end):**
> 1. **In the `/worktrees/dockerless` worktree: `git merge main`** (merge-commit, not rebase — replaying 48 commits over 54 with golden conflicts at each step is pain for zero benefit).
> 2. Resolve the **3 source conflicts** (Makefile, cmd/swe-swe/init.go, cmd/swe-swe/main_test.go — both branches added flags/targets, likely take-both). Golden conflicts: accept either side, then `make build golden-update` regenerates truth (your saved strategy).
> 3. **Validate the combined tree** — the first time mcp-less + dockerless ever run together: `make test`, `make e2e-dockerless`, both `test-e2e-agent-view-remote` tiers.
> 4. Only then, in `/workspace`: `git merge dockerless` into main — clean, since reconciliation already happened on the branch. (Working tree changes only; the running dogfood server is untouched until your next reboot.)
>
> **Two choices for you:**
> 1. **Push after merging?** main → origin right after step 4? Or leave local and you push when ready?
> 2. **Compose-mode e2e too?** `make test-e2e` (simple + compose modes, boots real containers) on the merged tree before it lands on main — slower but validates the Docker path also survived the merge. Include it?

<small>took 15.6s</small><br>
**AGENT**

> Merge prep stopped — no merge actions taken. Exporting the chat log now.

