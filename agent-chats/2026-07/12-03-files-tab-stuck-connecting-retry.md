<!-- agent-chat export
title: Files Tab Stuck Connecting Retry
date: 2026-07-12
index: 03
slug: files-tab-stuck-connecting-retry
agent: claude
version: 0.8.10 (80c7515)
-->

# Files Tab Stuck Connecting Retry

_2026-07-12 · 03 · claude · agent-chat 0.8.10 (80c7515)_

**AGENT**

> Read the bug report (`tasks/2026-07-12-files-tab-stuck-connecting-retry.md`) and traced the code. Confirmed root cause: `setIframeUrl()` (terminal-ui.js:6588) hides the "Connecting to files…" overlay only on `iframe.onload`. If that first navigation is dropped (cold tunnel / backgrounded tab / weak mobile signal), `load` never fires → overlay sticks forever, nothing retries. There IS already a backend-readiness probe (`/files-ready`), so the gap is purely the **iframe navigation itself** failing + no reload-on-return.
>
> The task file is a bug report with no explicit phases, so here's the phased plan I'll execute TDD-style:
>
> **Phase 1 — `modules/iframe-load-supervisor.js` + unit tests (pure logic, TDD)**
> A small `IframeLoadSupervisor` (injectable timers + iframe stub, reuses `reconnect.js` backoff, monotonic cache-bust token — no `Date.now`/`Math.random`). Covers: 4s load-watchdog → retry w/ cache-busted src; `load` → done+reset+hide overlay+clear watchdog; `error` → retry; `kick()` (focus/visibility) → immediate retry w/ backoff reset when not loaded, no-op when loaded; backoff capped at ~15s. RED first, then GREEN. Run via `node --test`.
>
> **Phase 2 — Wire it into the Files pane (terminal-ui.js) + live browser verify**
> Attach the supervisor when the files iframe src is set; add visibilitychange/focus/pane-activate kicks; overlay shows "Reconnecting…" while retrying. Verify in a live test container with the browser MCP against the 4 acceptance criteria (auto-recover, reload-on-visible, rate-limited, happy-path-unchanged).
>
> Each phase ends with `make build golden-update` (terminal-ui.js + the new module are in golden testdata) + `make test` + a scoped commit. I'll scope to **Files only** for now (preview/browser panes noted as follow-up, per "if practical").
>
> Starting Phase 1 now unless you want changes.

[Quick replies]
- Looks good, proceed
- Also fix preview/browser panes now
- Change the plan

**USER**

> Is this problem code shared across other tabs? Or files tab is a different scenario

<small>took 31.1s</small><br>
**AGENT**

> Good question — I checked. **The flaw is shared, Files isn't special.**
>
> All four iframe panes (preview, files, shell, browser) dismiss their "Connecting…" overlay only on `iframe.onload`, with no watchdog / `error` / visibility retry:
>
> - **shell, files, browser** all funnel through the *same* `setIframeUrl()` (terminal-ui.js:6568) → same `iframe.onload = () => hide overlay` and nothing else.
> - **preview** has its own parallel path (`setPreviewURL`, :6600) with the identical pattern — but it also has a URL bar with a manual ↻ refresh button, so a stuck preview has a built-in escape hatch the others lack.
>
> What differs is only the *cold-start* handling, not the *dropped-navigation* handling:
> - files & browser already have a backend-readiness probe (`/files-ready`, `/vnc-ready`) that waits for the server before setting src — so they don't paint a dead iframe on cold start. But once src IS set, a dropped nav hangs them identically.
> - Files feels worst on mobile because md-serve renders full pages → it's a *plain* iframe with no inner shell-page of its own to recover, and mobile tab-reaping is where dropped navs happen.
>
> **Recommendation:** put the load-supervisor in the shared `setIframeUrl()` so files + shell + browser all get watchdog+retry+visibility-reload in one place (barely more work than files-only, and it fixes the actual shared root cause). Preview's separate `setPreviewURL` path I'd wire in the same PR as a small follow-up, or defer since it has the manual ↻.
>
> So revised plan: Phase 1 supervisor module (unchanged) → Phase 2 wire into shared `setIframeUrl` (covers files/shell/browser) → Phase 3 preview path. Want me to go shared, or keep it strictly Files-only?

[Quick replies]
- Go shared (files/shell/browser)
- Shared + preview too
- Keep it Files-only

**USER**

> Go shared (files/shell/browser)

<small>took 59.5s</small><br>
**AGENT**

> ✅ Confirmed: shared fix (files/shell/browser via `setIframeUrl`). Starting Phase 1 — writing the failing unit tests for the new `iframe-load-supervisor.js` module first (TDD RED).

<small>took 5m 38s</small><br>
**AGENT**

> ✅ **Phase 1 complete** — `iframe-load-supervisor.js` module (pure logic, TDD).
>
> Salient behavior (all injectable/testable):
> ```
> start()  -> load clean url, arm 4s watchdog, overlay 'connecting'
> watchdog -> retry with cache-busted src, backoff 1s→2s→4s→8s→cap 15s, overlay 'reconnecting'
> load     -> overlay 'loaded', reset backoff, clear watchdog (no more reloads)
> error    -> immediate retry
> kick()   -> instant retry + backoff reset when NOT loaded; no-op once loaded
> ```
> Reuses `reconnect.js` backoff; cache-bust token from a monotonic counter (no `Date.now`/`Math.random`).
>
> - `node --test`: **14/14 GREEN**, all modules **349/349**
> - `make build golden-update`: 92 new golden files (only the new module), `make test`: **PASS**
>
> `git --stat` (this phase): +162 module, +231 tests, ×46 golden variants.
>
> Next: **Phase 2** — wire the supervisor into the shared `setIframeUrl()` (files/shell/browser) + visibility/focus/pane-activate kicks, then verify live in a test container with the browser MCP.

<small>took 5m 44s</small><br>
**AGENT**

> **Phase 2 code wired** — supervisor now drives `setIframeUrl` (files/shell/browser), plus visibility/focus/pane-activate kicks + cleanup. Rendered `terminal-ui.js` passes syntax check, golden diff is exactly the wiring, `make test` PASS, supervisor units still 14/14.
>
> Now bringing up a real test container (`make e2e-up-simple`, port 9780) to verify against the 4 acceptance criteria with the browser MCP. This rebuilds the Docker image + binary — a few minutes.

<small>took 9m 55s</small><br>
**AGENT**

> ⚠️→✅ **Live browser testing caught a real bug.** The default timer wiring `{ setTimeout, clearTimeout }` (bare global refs) threw `TypeError: Illegal invocation` in the browser — `this.timers.setTimeout(...)` calls with the wrong receiver, which the browser's WebIDL rejects. Node has no such check, so the 14 injected-clock unit tests passed but the real iframe never even started its load attempt. **This is exactly why we verify in a real browser, not just unit tests.**
>
> Fix (module constructor):
> ```js
> this.timers = timers || {
>   setTimeout: (fn, ms) => setTimeout(fn, ms),   // call the global directly,
>   clearTimeout: (id) => clearTimeout(id),        // preserving window binding
> };
> ```
> Added a default-timers regression test → `node --test` **15/15**. Rebuilding the container to re-verify the full flow live.

<small>took 7m 55s</small><br>
**AGENT**

> ✅ **Phase 2 complete — all 4 acceptance criteria verified live** (real container, real browser). Files pane shown loaded below.
>
> Live results:
> - **Happy path:** Files pane loaded once (src `…:29200/`), overlay hidden, src stable over 12s — no periodic reloads.
> - **Auto-recover:** firing `load` after failures → `loaded`, retries stop, overlay clears.
> - **Reload-on-visible:** kick when loaded = no-op; forced not-loaded + kick → instant cache-busted reload (`?_r=1`) + "Reconnecting…" → recovered.
> - **Rate-limited:** shipped module w/ real browser timers, black-hole URL → 5 attempts in 2s, gaps `301→401→600→601` (backoff grows, caps) — no tight loop.
>
> Also caught + fixed a **browser-only `Illegal invocation`** in the default timer wiring that the unit tests couldn't see — exactly the kind of thing live verification exists for.
>
> `git --stat` (Phase 2 code): terminal-ui.js +94, module +12, tests +42. Container torn down. Committed across scoped commits.
>
> Next: I'll walk you through each phase log for sign-off, then export this chat.
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-07-12-03-1-b21a8218eadf.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-07-12-03-1-b21a8218eadf.png" alt="files-recovered-ok.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

<small>took 31m 29s</small><br>
**AGENT**

> (Still here — the previous message may have timed out on my side while you were away.)
>
> The fix is **done and committed** across scoped commits; the e2e container is torn down. I'm at the final sign-off step: I posted the **Phase 1 log walkthrough** just above and I'm waiting on your OK before walking through **Phase 2** and then exporting this chat.
>
> Quick recap so far: Phase 1 = new tested `iframe-load-supervisor.js` module (15 tests). Phase 2 = wired it into the shared `setIframeUrl` (files/shell/browser) + focus/visibility kicks; caught & fixed a browser-only `Illegal invocation`; **all 4 acceptance criteria verified live**.
>
> Reply when ready and I'll continue the walkthrough / export.

[Quick replies]
- Show Phase 2 walkthrough
- Just export the chat and finish
- Question first

<small>took 38ms</small><br>
**AGENT**

> **Walkthrough — Phase 1 log** (`…-phase1.log`)
>
> **What I did:** TDD'd a new pure module `modules/iframe-load-supervisor.js` — the `IframeLoadSupervisor` holding the load-watchdog + capped-backoff + `kick()` logic (reuses `reconnect.js`; monotonic cache-bust tokens, no `Date.now`/`Math.random`; injectable timers so it's unit-testable).
>
> **Sequence:**
> - Wrote the test file first → **RED** confirmed (`ERR_MODULE_NOT_FOUND`, module didn't exist yet).
> - Implemented the module → **GREEN 14/14**. One test I'd written was wrong (assumed a phantom watchdog would fire after `load` cleared it); I corrected it to exercise backoff-reset via the `error` path — a test bug, not a code bug.
> - Regression: all JS modules **349/349** pass.
> - `make build golden-update` → 92 new golden files (just the new module ×46 variants); `make test` **PASS**.
>
> **Problems:** none in the code — only the self-inflicted test-logic fix above.
> **Conclusion:** Solid, fully-tested pure module. Clean.
>
> OK to proceed to the Phase 2 walkthrough?

[Quick replies]
- Yes, show Phase 2
- Wait — question about Phase 1

