<!-- agent-chat export
title: Share Live Session Scoped Cookie
date: 2026-07-09
index: 01
slug: share-live-session-scoped-cookie
agent: claude
version: 0.8.7 (2089b8b)
-->

# Share Live Session Scoped Cookie

_2026-07-09 · 01 · claude · agent-chat 0.8.7 (2089b8b)_

**USER**

> I want a feature that shares a single live session with someone else via unique link and a password. 
>
> Maybe generate a password that auth accepts but somehow after accepting and having person logged in, they can only access this live session on homepage? Sounds like expanding jwt to have a scope limit and the sessions related code respect that limit
>
> Discuss options.

<small>took 17.6s</small><br>
**AGENT**

> Reading the auth/JWT + session code so the options are grounded in how it actually works today. One moment...

<small>took 23.4s</small><br>
**AGENT**

> Two explorers running in parallel — one mapping the JWT/login flow, one mapping how live sessions are routed/accessed. I'll come back with concrete options grounded in the real code.

<small>took 1m 27s</small><br>
**AGENT**

> Auth flow mapped: it's an HMAC-signed cookie (not JWT), payload is just `timestamp|hmac`, the HMAC key IS the shared SWE_SWE_PASSWORD, and auth is all-or-nothing — no user, no scope. Waiting on the session-routing map, then I'll lay out options.

<small>took 21m 9s</small><br>
**AGENT**

> Dug into the code. One correction up front that changes the framing: **there's no JWT.** The auth cookie is `timestamp|HMAC-SHA256(timestamp, secret)` where the HMAC key literally *is* `SWE_SWE_PASSWORD`. It's stateless, carries no identity, no roles, no scope — auth is binary (valid cookie = full access to every session). So "expand JWT scope" becomes "add a scope segment to the signed cookie + enforce it." Same spirit, simpler object.
>
> Also important: there is **no owner/scope anywhere** today, and the closest existing primitive to copy is the **per-session MCP auth key** (`mcp_authkey.go` → `sessionKeyMatchesPath`), which already does exactly "this token may only touch session X." That's the model.
>
> ## The 3 real options
>
> **A — Scoped cookie (your idea, literally).** Extend the payload to `timestamp|scopeUUID|hmac`. Empty scope = unscoped (backward-compatible with existing 2-part cookies). Guest hits a share link, enters the generated password, gets a cookie stamped `scope=<uuid>`. Every UUID-resolving handler rejects a mismatched scope; homepage renders only that one session.
> - ✅ Matches your mental model exactly ("logged in but boxed in", homepage shows only this session), single cookie mechanism.
> - ⚠️ Stateless → **can't revoke a share early** (dies only on 7-day expiry or when the session ends). Touches cookie format.
>
> **B — Session-scoped token, no cookie change.** Copy the MCP-key pattern: mint a random token bound to one UUID, link is `/session/{uuid}?share=<token>`, accepted only for that UUID. The random token *is* the password-equivalent.
> - ✅ Zero cookie changes, models a tested primitive, naturally revocable (server-side registry).
> - ⚠️ No real "logged-in" feel; token rides every request. Weak homepage story (guest lands direct, doesn't browse).
>
> **C — Hybrid (my recommendation).** Share link → password prompt → on success issue a **scoped cookie** (A's payload) backed by a **server-side share record** (B's registry). Best of both: guest gets a normal boxed-in session, homepage filters to the one session, AND you can revoke / one-time / set short expiry because the cookie's scope UUID is validated against a live registry entry.
>
> ## Decisions I need from you (these shape the build)
>
> 1. **Read-only or collaborate?** Right now *every* WS client can type into the agent's PTY. If the guest should only *watch*, we gate input in the WS read loop (~`main.go:5695`) — a genuine new "spectator" path. If it's pair-programming, no change. Which?
> 2. **Revocable?** Do you want a "revoke share" button / one-time link / custom expiry? If yes → stateful (Option C). If "eh, it dies with the session" is fine → stateless is simpler (Option A).
> 3. **Scope to embedded-auth mode only?** Auth is embedded only when `SWE_SWE_PASSWORD` is set; compose/Traefik mode auths externally. I'd build this for embedded mode first and enforce scope at the handler level (which we do regardless). OK to defer compose mode?
>
> One thing I'll bake in regardless: a single `enforceSessionScope(r, uuid) bool` helper called at *every* UUID branch (WS, `/session` page, `/proxy`, per-session APIs, files/VNC) + homepage filter + block new/fork/list — because there's no router, scope-escape via a missed branch is the main risk, so it defaults to deny.
>
> My lean: **Option C, read-only guest, embedded-mode first.** Want me to write it up as a plan?

[Quick replies]
- Go with C, read-only, embedded-first — write the plan
- Collaborate (read-write), not read-only
- Stateless is fine — do Option A
- Let me answer the 3 decisions first

**USER**

> 1. Collaborate as full actual user in session. 2. No need; session can end. 3. Yes
>
> Write a task

<small>took 2m 58s</small><br>
**AGENT**

> Task written: `tasks/2026-07-08-share-live-session-scoped-cookie.md` (executable via `/swe-swe:execute-in-worktree`).
>
> **Design, locked to your 3 decisions:**
> - **Scoped cookie**, not JWT. Today's cookie is `timestamp|hmac(timestamp)` keyed by `SWE_SWE_PASSWORD`. A guest gets `timestamp|scope|hmac(timestamp|scope)` where scope = the session UUID. HMAC is always keyed by the master secret, so a guest **can't forge or un-scope** their cookie (they don't know the secret). Old 2-part cookies keep working untouched.
> - **Full participant** (your #1) → zero changes to the WS/PTY read loop. Guest types into the agent like anyone else.
> - **No revocation** (your #2) → the share password lives on the in-memory `Session`; ending the session IS the revocation. No DB, no registry.
> - **Embedded-auth first** (your #3), but scope is enforced at the handler level too so compose mode isn't silently bypassed.
>
> **Flow:** owner clicks Share → `POST /api/session/{uuid}/share` → gets `{url, password}` → sends both to the guest → guest logs in on a `?scope={uuid}` link → boxed into that one session.
>
> **4 phases:** (1) cookie scope primitive + unit tests, (2) share password on Session + issue endpoint + login scope-branch, (3) enforcement at *every* UUID handler (the security-critical phase — there's no router, so I baked in a `scopeAllows()` helper + a full enforcement matrix so no branch leaks), (4) owner Share UI + end-to-end browser test that a guest is denied `/`, other sessions, `/ws/{X}`, spawn, etc.
>
> The one real risk I've flagged in-plan: **scope-escape via a missed handler branch** (per-port preview/vnc proxies especially — `requireAuthCookie` needs the owning UUID to compare against). The matrix covers all of them.
>
> Want me to start executing Phase 1, or review/adjust the plan first?

[Quick replies]
- Start Phase 1
- Execute in a worktree
- Let me read the plan first
- Adjust something in the design

**USER**

> 🎤 You said the password is with the session why is it not in the server service

<small>took 29.9s</small><br>
**AGENT**

> Still here. The plan is written and ready, saved as the share-live-session task file. Quick recap: it uses a scoped cookie, not a JWT. The guest's cookie carries the session ID as a scope, signed with the master secret, so a guest can't forge it or escape their session. The guest joins as a full participant, typing into the agent normally. There's no revocation needed since the share password lives on the in-memory session and dies when the session ends. It's four phases: the cookie primitive with tests, then the share password and login handling, then enforcement at every handler, then the owner share button plus an end-to-end lockout test. How do you want to proceed? Start phase one, run it in a worktree, or read the plan first?

[Quick replies]
- Start Phase 1
- Execute in a worktree
- Let me read the plan first
- Adjust something in the design

**USER**

> 🎤 The password should be with the AUTH service then the login homepage and everything will work normally

<small>took 37.4s</small><br>
**AGENT**

> Got it, and that actually simplifies things. So the auth service owns the password and the scope: it validates the share password, stamps the session scope into the cookie, and it's the single gate that enforces it. The login page, the homepage, and every normal handler stay untouched and just work, because in embedded mode the auth middleware already wraps every route. So instead of sprinkling scope checks into each handler, there's one check in the auth service. I'll rewrite the plan that way. One wrinkle to confirm: the preview and VNC proxies run as separate listeners that don't pass through that main auth gate, so a guest could otherwise reach another session's preview port. Do you want me to close that one too, or is leaving it out fine for now?

[Quick replies]
- Close that one too
- Leave it out for now
- What's the risk if we leave it?

**USER**

> 🎤 How do you propose we close that same will be for the chat and the files and the agent terminal no correct

<small>took 32.6s</small><br>
**AGENT**

> Right, you're thinking about it correctly, with one nuance. Those per-port services each belong to exactly one session. Each session gets its own preview port, its own chat port, its own files port, its own VNC port. And they're all already wrapped by the same guard, the one called requireAuthCookie, which today only checks that you have a valid cookie. So the fix is uniform: because each listener is created for a specific session, we hand that session's ID to the guard when we set it up, and it just adds one comparison, does the cookie's scope match this listener's session. If not, reject. Same exact change for preview, chat, files, and VNC. So it's not new logic per service, it's one guard taught to compare scope, applied in all four spots. The one thing that's already handled for free is the agent terminal itself, because that WebSocket runs on the main mux behind the central auth gate, so scoping it happens automatically. So net: the auth service is the gate for everything on the main path, plus that one guard learns scope for the four side ports. Shall I fold that into the plan and close all of them?

[Quick replies]
- Yes, fold it in and close all
- Just the terminal for now
- Walk me through the guard change

**USER**

> 🎤 When the session is scoped the person at the homepage the person cannot do a new session the person cannot view other session the person cannot view other recordings

**USER**

> 🎤 It is better

<small>took 27.9s</small><br>
**AGENT**

> Yes, exactly, and I'll add recordings to the enforced list. So when the cookie is scoped: no new session, no forking, can't open any other live session, and can't open any recording. The auth gate rejects all of those, and the homepage only shows their one live session. Recordings are their own route, so I'll have the gate block those for a scoped cookie too. One thing to pin down there. Recordings are replays of ended sessions, and they use a different ID than the live session. Since we agreed ending the session is basically the revocation, my instinct is the simplest and safest rule is: a scoped guest can't open any recording at all, including a replay of their own session after it ends. Once the session ends, they're done. Does that match what you want, or should a guest still be able to replay their own session after it ends?

[Quick replies]
- Block all recordings, that's right
- Let them replay their own after it ends
- Doesn't matter, pick the simplest

**USER**

> 🎤 You are correct no recordings

<small>took 1m 43s</small><br>
**AGENT**

> Done, the plan now reflects all of that. Enforcement is centralized in the auth service: one scope check in the middleware covers the homepage, the session page, the agent terminal, the proxy, and the API, plus a single scope-aware change on the four side ports for preview, chat, files, and VNC. And a scoped guest is denied new sessions, forking, any other session, and all recordings, with the homepage showing only their one live session. I also saved the design to memory so it survives a restart. Want me to start building Phase one, the cookie scope primitive with its tests?

[Quick replies]
- Yes, start Phase 1
- Do it in a worktree
- Not yet, I'll review the plan first

**USER**

> 🎤 Yes build it EDD

<small>took 10.6s</small><br>
**AGENT**

> On it. Starting Phase one test-driven: I'll write the cookie scope tests first, watch them fail, then add the scope segment to the sign and verify functions and make them pass. I'll ping you when Phase one is green.

<small>took 6m 25s</small><br>
**AGENT**

> Phase one is done and committed. The scoped cookie primitive is in with seven new tests all green, and the golden files updated cleanly, only the auth file changed. No behavior change yet, that's by design. I'm moving straight into Phase two now, the share password on the session plus the login handling. I'll keep you posted.

<small>took 9m 12s</small><br>
**AGENT**

> Phase two committed, the share endpoint and scoped guest login are in with nine more tests green. Now the important one, Phase three, the enforcement in the auth gate. I'm mapping every route first so the guest lock-down has no gaps, then I'll wire it in.

<small>took 9m 3s</small><br>
**AGENT**

> Good news, the whole backend is done and committed across three phases. The scoped cookie, the share endpoint that hands back a link and password, the scoped guest login, and the enforcement that boxes a guest into one session, denying other sessions, new sessions, forks, recordings, and even the side ports for preview, chat, files, and VNC. Everything is covered by unit tests, all green, and I confirmed a guest can't resurrect a session after it ends. What's left is Phase four, the owner-facing Share button in the session UI plus a live browser test in a throwaway container to prove a guest really is locked in. That part touches the frontend and spins up a test container, so I wanted to check in first. Want me to go ahead and build the Share button and run the live test? And do you have a preference for where the button lives, or should I pick a sensible spot?

[Quick replies]
- Go ahead, you pick the placement
- Go ahead, put it in the session header/menu
- Pause here, backend is enough for now
- Show me the share endpoint response shape first

**USER**

> Put it inside Session Settings dialog

<small>took 5m 3s</small><br>
**AGENT**

> Share tab added to Session Settings (Collaborate > Share session), with Create-link button, copy buttons, and CSS. JS parses, all Go tests green, golden updated. Now booting a throwaway test container to prove end-to-end that a guest logs in and is locked to just this session.

<small>took 18m 46s</small><br>
**AGENT**

> Done — all 4 phases built TDD, committed on `main`, and verified live. 🎉
>
> **Commits**
> - scoped cookie primitive (`timestamp|scope|hmac`, HMAC keyed by master secret; old cookies untouched)
> - share endpoint + scoped guest login (share password on the in-memory Session)
> - centralized scope enforcement in the auth gate + scope-aware per-port guard
> - **Share session** tab in Session Settings (screenshot below)
>
> **Live end-to-end** (real e2e-simple server, embedded auth on) — every case correct:
> | request as guest | result |
> |---|---|
> | own `/session`, `/ws`, static assets | **200** ✅ |
> | homepage `/` | **302 → own session** ✅ |
> | other session, `/ws/other`, new, fork, `/recording`, `/api/repos`, `/api/worktrees`, mint-a-share | **403** ✅ |
> | wrong share password | **401** ✅ |
>
> The guest cookie came back as the 3-part scoped form and the Create-link button populated the link + 48-char password in the UI. Also confirmed a guest can't resurrect an ended session (WS creation needs a staged intent they can't get), and the homepage returns 410 instead of looping once the session ends.
>
> **Not pushed/released** — it's local commits only, awaiting your dogfood after the next rebuild+reboot (this feature only activates when `SWE_SWE_PASSWORD` is set / embedded-auth mode; compose-mode was deliberately deferred).
>
> Want me to push, or leave it local for you to review the diff first?
>
> [tui-check.js](./assets/2026-07-09-01-1-e4c9c37656f2.js)

[Quick replies]
- Leave it local, I'll review
- Push to origin/main
- Walk me through the enforcement diff
- Add compose-mode support too

**USER**

> What is compose-mode support? 
> Also, can we check if we lost the support/prompt where we nudged agent to set session name (a feature we added recently?)

<small>took 1m 49s</small><br>
**AGENT**

> **1. Session-name nudge — not lost, it's intact. ✅**
>
> The nudge lives in the `set_session_name` MCP tool description itself (main.go:8786):
> > "Set a session's display name… **Call it once the task at hand is clear, so the user can tell sessions apart**; use '{short task title} {owner}/{repo}@{branch}'."
>
> Every MCP-connected agent sees that in its tool list, so the nudge fires for all of them. The `/swe-swe:session-title-set` slash command source is also still present. The recent `refactor(mcp): remove MCP-less feature surface` commit only removed (a) an unrelated MCP-less CLAUDE.md steering block and (b) the `without-mcp` golden variant (which held *copies* of these files) — I grepped that commit's diff and it touched nothing about session naming. Nothing lost.
>
> **2. What "compose-mode" means**
>
> swe-swe runs auth in one of two topologies:
> - **Embedded** (single-binary / dockerless / tunnel): the server gates itself — `authMiddleware` wraps every route. This is where my scope enforcement lives, so the guest boxing works fully. ✅
> - **Compose** (multi-container behind **Traefik**): Traefik's ForwardAuth middleware gates requests by calling the server's `/swe-swe-auth/verify` before they reach the app.
>
> The gap: `/swe-swe-auth/verify` (the Traefik path) only checks that the cookie is *valid* — it doesn't check that the cookie's **scope matches the requested path**. And the four per-port proxies (preview/chat/files/vnc) in compose are fronted by Traefik ForwardAuth rather than my scope-aware `requireAuthCookie`. So in a pure-Traefik deployment a scoped guest could slip past the boxing on those routes. That's exactly why we scoped this to embedded-first (decision #3).
>
> Closing it = teach `authVerifyHandler` to apply the same guest policy using Traefik's `X-Forwarded-Uri` header. Maybe half a day, and testable in `make e2e-up-compose`.
>
> Want me to close the compose gap now, or leave the share feature embedded-only for now? (Still unpushed either way.)

[Quick replies]
- Leave embedded-only for now
- Close the compose gap too
- How do I dogfood the embedded version?
- Push what we have

**USER**

> Close the compose gap too

<small>took 1m 1s</small><br>
**AGENT**

> Digging into the compose topology before I touch code — I found the per-port proxies may already be scope-enforced by my Phase 3 change if SWE_SWE_PASSWORD is set in compose, so I want to pin down exactly what (if anything) bypasses the Go auth layer via Traefik. Verifying now.

<small>took 7m 23s</small><br>
**AGENT**

> Compose gap closed. ✅
>
> **What I found digging in** (more nuanced than my first take): compose actually sets `SWE_SWE_PASSWORD=changeme`, so `authMiddleware` *is* active there, and the single Traefik `/` router funnels all app traffic through swe-swe-server — so my Phase 3 enforcement already covered the app in compose. The **one genuine hole** was the **Traefik dashboard** router (`/dashboard`, `/api/http|tcp|entrypoints|overview`): it's gated by ForwardAuth → `/swe-swe-auth/verify` but routes to Traefik's own `api@internal`, bypassing swe-swe-server entirely. `/verify` only checked cookie *validity*, so a scoped guest's valid cookie would have reached the dashboard.
>
> **Fix:** `authVerifyHandler` (the ForwardAuth gate) now reads the cookie scope and, for a guest, runs `scopedVerifyAllowed(scope, X-Forwarded-Uri)` — denies the dashboard prefixes with a 403, and delegates everything else to the *same* `scopedPathAllowed` policy the embedded gate uses (own session + assets allowed; other sessions, recordings, spawn, repo APIs denied). Full users unaffected; missing/invalid cookie still redirects to login. Refactored so both gates share one policy function.
>
> Added 2 unit tests (`scopedVerifyAllowed` table + `authVerifyHandler` scoped end-to-end incl. dashboard-403 and full-user-unaffected). Full suite green, golden updated.
>
> So the feature now boxes a guest in **both** topologies: embedded (authMiddleware + the 4 per-port guards) and compose (+ the ForwardAuth gate). That's **5 commits** total on `main`, all unpushed.
>
> Want me to push, or anything else first?

[Quick replies]
- Leave it local, I'll review
- Push to origin/main
- Show me the full 5-commit diffstat
- How do I dogfood it after reboot?

**USER**

> Push to origin/main

<small>took 1m 14s</small><br>
**AGENT**

> Pushed to `origin/main`. ✅ The 5 share-feature commits (oldest → newest):
> - scoped session cookie primitive
> - share endpoint + scoped guest login
> - enforce scope in the auth gate (embedded)
> - Share session tab in Session Settings
> - enforce scope at the Traefik ForwardAuth gate (compose)
>
> **Heads up:** a 6th commit went up with them — `feat(new-session): order Where dropdown by per-device recency`. That one is **not mine** — it was already sitting unpushed on local `main` when I started, so it stacked underneath my commits and pushed along. It's legitimate-looking work, just wanted you to know it wasn't part of the share feature in case you were tracking it separately.
>
> The share-session feature is now fully on `origin/main`, guest-boxing enforced in both embedded and compose topologies. It goes live after the next rebuild+reboot (activates whenever `SWE_SWE_PASSWORD` is set). Anything else?

[Quick replies]
- That's all, thanks
- Was that 6th commit supposed to be there?
- Walk me through dogfooding after reboot
- Add a CHANGELOG entry

**USER**

> /ck:export-chat-logs

