# Session creation requires a staged intent (kill ghost sessions + make fork a safe GET)

Date: 2026-06-28
Status: PLAN — not yet implemented

## Problem

Two bugs, one root cause: **a bare GET / WebSocket reconnect can create a session.**

1. **Side-effecting GET fork.** `GET /api/fork/{uuid}` writes a new `.jsonl`
   (`forkconvo.Fork`), mints a UUID, stages `pendingForks`, and 302s — all from a
   plain GET. Any passive URL-follower (prefetch, link unfurler, AV scanner,
   refresh, back-button) forks for you.

2. **Stale-tab ghost session.** `/session/{uuid}` render is harmless, but on WS
   connect `getOrCreateSession` treats **any** UUID not in the live `sessions`
   map as "create a brand-new empty session reusing this UUID." An ended session
   and a never-seen UUID are indistinguishable, so a stale tab silently spawns a
   ghost session with no history.

## Invariant we are establishing

> A session materializes **only** when the WS handler finds, for that UUID, either
> an already-live session OR a staged creation intent. A bare GET / navigation /
> WS-reconnect to an unknown UUID **never** creates a session — it reports "gone".

Both "+ New session" and "Fork/Resume" stage an intent via **POST**, then 302 to a
clean `/session/{uuid}`. `getOrCreateSession` stops being "create on any bare UUID".

This unifies fix #1 (GET-safe fork) and fix #2 (no ghost sessions) into one rule.

## CRITICAL — blast radius

This is the **session-creation critical path**. If it regresses, **no session can
be created after reboot** — the whole app is unusable. Therefore:

- Land behind verifiable e2e at every phase; do **not** reboot the live stack onto
  this until e2e is green end-to-end.
- Keep changes additive where possible; preserve a working fallback until the new
  path is proven (see Phase 0 safety net).
- Each phase is independently testable and independently revertable.

## Canonical source

All edits under `/workspace/cmd/swe-swe/templates/host/swe-swe-server/`
(NOT the generated copies in `testdata/golden/`, `tmp/`, `.test-home-e2e/`).
Template changes require `make build golden-update` + commit of the golden diff.

Key locations (from current code):
- Fork route + handler: `main.go:2313` (route), `handleSessionForkAPI` `main.go:7370`,
  Fork/mint/stage/redirect `main.go:7484-7520`.
- `pendingForks` consumption in WS path: `main.go:5125-5152`.
- `getOrCreateSession`: `main.go:4510-4532`; called from WS at `main.go:5154`.
- `/session/{uuid}` HTML route: `main.go:2366-2477`.
- New-session UUID minted server-side: `main.go:2219` / `:2233` (`NewUUID`).
- Homepage button: `selection.html:1439` (`data-uuid="{{.NewUUID}}"`).
- New-session dialog confirm + URL build: `new-session-dialog.js:562-607`.
- WS open in client: `terminal-ui.js:1198-1237`.
- Ended-source hydration (reuse for fork): `fork_legacy.go:33-115`.

---

## Phase 0 — Safety net + test scaffolding (do FIRST)

Goal: be able to prove every later phase without risking the live stack.

1. Stand up the e2e harness per memory rules: `make e2e-up-simple`
   (port 9780), drive via `http://host.docker.internal:9780/`. Tear down with
   `make e2e-down`. (Do NOT use `make run`.)
2. Write the **baseline e2e smoke** that must stay green through every phase:
   - Homepage loads.
   - "+ New session" (chat) → lands on `/session/{uuid}` → WS connects →
     session is live → can send a message and get agent output.
   - This is the "after reboot we can still get a session" guarantee. Capture it
     as a scripted browser flow (Playwright MCP) + screenshot.
3. Record current behavior as the regression baseline (screenshots:
   new-session works, fork works, stale-tab currently ghosts).

Verification: baseline smoke passes on unmodified code. Commit nothing here
(test assets only if scripted).

---

## Phase 1 — Server: generalize staged intents (`pendingNew` alongside `pendingForks`)

Pure server-side; no client change yet, so nothing regresses.

1. Introduce a unified staged-intent map (or add `pendingNew` mirroring
   `pendingForks`). Each entry carries the full `SessionParams` needed to
   materialize (assistant, mode chat/terminal, WorkDir/pwd, branch, name,
   extra_args, debug, and for forks the resume ExtraArgs + PrepopulateChatLog).
2. Add `POST /api/session` (new) that:
   - parses params from POST body,
   - mints (or accepts) a UUID,
   - stages a `pendingNew` entry,
   - `302 /session/{uuid}` (clean URL, no query params).
3. Convert fork to GET-confirm + POST-do:
   - `GET /api/fork/{uuid}` → render a **skeleton + confirm modal** page. Run the
     *cheap* guards only (source exists, forkable, not mid-tool-call) to populate
     the modal / disable the button. **No disk writes, no staging.**
   - `POST /api/fork/{uuid}` → existing Fork+mint+stage(`pendingForks`)+302.
     Carry `?bubble=&mode=` as hidden form fields → POST body.
4. **Do NOT yet** change `getOrCreateSession`'s auto-create — that flips in Phase 3.
   At this point both old (lazy auto-create) and new (staged intent) paths work,
   so the app is never broken between phases.

Verification:
- Unit/golden: `make test`; `make build golden-update` for any template touched;
  commit golden diff.
- e2e: `POST /api/session` 302s and the WS materializes from `pendingNew`.
  `GET /api/fork/{uuid}` renders the modal and creates **nothing** on disk
  (assert no new `.jsonl`); `POST /api/fork/{uuid}` forks. Baseline smoke (Phase 0)
  still green via the OLD navigation path.

---

## Phase 2 — Client: homepage New + fork links use POST

1. New-session dialog: replace `window.location = buildSessionUrl(mode)` with a
   POST to `/api/session` (form submit or `fetch` then follow the 302 /
   `window.location` to the returned `/session/{uuid}`). Move all params into the
   POST body; the resulting session URL is clean.
2. Fork/Resume links: the "Resume" anchor (`selection.html:1577`) and any
   bubble-fork affordance become POSTs (a form/button), OR the GET lands on the
   confirm page whose "Fork" button POSTs. Decide one (confirm-page is the nicer
   UX and matches the user's original ask).
3. Keep the WS open code (`terminal-ui.js`) reading params from the page, but
   since params now arrive via staged intent, the page no longer needs them in the
   URL — confirm the seeded `<terminal-ui>` attributes still come through (server
   can inject from the staged intent at render time if needed).

Verification:
- e2e: New (chat + terminal) via POST → session live + message round-trips.
  Resume/fork via confirm page → POST → forked session replays history +
  reattaches agent. Refresh of the resulting clean URL reconnects to the LIVE
  session (in `sessions` map) without re-creating. Golden updated + committed.

---

## Phase 3 — Server: WS handler stops auto-creating; adds "gone" state

The actual ghost-killer. Do this only after Phases 1-2 are green.

1. In the WS path, replace get-or-CREATE with:
   - live in `sessions` → attach.
   - `pendingNew` / `pendingForks` intent present → materialize + consume + delete.
   - **else → DO NOT create.** Send the client a structured "session gone /
     not available" status over the WS (and/or a distinct close code).
2. `getOrCreateSession` (or its WS caller) no longer creates from a bare UUID.
   Audit every other caller of `getOrCreateSession` to ensure none relied on
   bare-UUID auto-create (e.g. terminal mode, child shells via `deriveShellUUID`).
   List them and confirm each now goes through a staged intent or live attach.
3. Client: on "gone" status, render an **ended/not-available** screen with
   [Resume] (→ fork confirm) and [New session] actions — instead of a blank/ghost
   terminal.

Verification:
- e2e ghost test: open a session, end it, then load the OLD `/session/{uuid}` in a
  fresh tab → must show "ended — Resume?", and assert **no new session** was
  created (check `list_sessions` / no new recording). This is the core fix.
- e2e regression: New + Fork still materialize (they have intents). Live refresh
  still attaches. Child-shell / terminal-mode paths still work.

---

## Phase 4 — Full e2e gauntlet + reboot rehearsal

Before touching the live stack:

1. Run the full matrix on the e2e harness:
   - New chat, New terminal, New project (new repo), New on existing branch.
   - Fork live session (tail), fork at bubble (after/replay/before), Resume
     orphaned/ended recording.
   - Stale-tab ended UUID → "gone" screen, no ghost.
   - Refresh live session → reattach.
   - Passive GET on `/api/fork/{uuid}` (simulate prefetch/unfurl) → creates nothing.
   - Two tabs / concurrent connects to same new UUID → exactly one session.
2. `make test` green. Golden committed and `git diff --cached` reviewed to be
   functional-only.
3. **Reboot rehearsal** in the e2e/test container (NOT prod): restart the stack,
   confirm "+ New session" works from a cold start — this is the
   "can we get a session after reboot" guarantee.
4. Only then follow `docs/dev/how-to-restart.md` for the real stack, and
   immediately re-run the new-session smoke against it. Keep the prior binary
   ready to roll back.

## Resolved decisions (2026-06-28)

1. **One unified intent map** — `pendingSessions[uuid] = SessionParams` (which is
   already a superset: WorkDir, ExtraArgs, and fork-only PrepopulateChatLog +
   resume args). New leaves fork fields empty; fork fills them. Add an explicit
   `Kind` (`new`|`fork`) field for logging only. Keeps the WS consume-site a single
   lookup + single branch; collapses the existing `pendingForks` consume at
   `main.go:5125-5152`.
2. **TTL sweep** — timestamp each entry; background sweeper every 60s evicts
   entries older than **10 min**; **log every eviction (uuid+kind)**. For `fork`
   evictions the sweeper must also **delete the orphaned `.jsonl`** that
   `forkconvo.Fork` wrote on the POST (the map key alone isn't the only leak).
   Lazy drop-on-access is insufficient because an unconsumed entry is never
   accessed again.
3. **"gone" signalling = app-level WS JSON frame** `{type:"session_gone", uuid,
   canResume:<bool>}`, then a clean close. `canResume` = does an ended recording
   exist on disk for this UUID (so the client offers Resume vs. only New). Close
   codes carry no payload, so they can't drive the rich ended screen.
4. **Fork confirm = standalone skeleton page** that reuses the session-page CSS but
   ships **no connect/WS JS** — only the confirm modal + a POST form. Structurally
   incapable of side effects (no WS code loaded), vs. injecting a modal into the
   real `index.html` shell whose JS could auto-connect. Matches the user's ask
   ("skeleton loading session page + modal").

**Shared shell (3 + 4):** the "gone" screen (client-rendered after `session_gone`)
and the fork-confirm screen (server-rendered, no WS) are the same visual family —
one session-skeleton + two action panels ([Resume]/[New] for gone;
[Fork]/[Cancel] for confirm). Build one skeleton, two panels, not two pages.

## Rollback

Each phase is a separate commit. Phase 3 is the only behavior-flipping one for
existing flows; if it regresses, revert Phase 3 alone — Phases 1-2 are additive
and leave the old lazy-create path intact, so the app still creates sessions.
