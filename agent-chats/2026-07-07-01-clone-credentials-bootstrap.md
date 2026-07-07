<!-- agent-chat export
title: Clone Credentials Bootstrap
date: 2026-07-07
index: 01
slug: clone-credentials-bootstrap
agent: claude
version: 0.8.6 (d0dadbc)
-->

# Clone Credentials Bootstrap

_2026-07-07 · 01 · claude · agent-chat 0.8.6 (d0dadbc)_

**AGENT**

> Starting `tasks/2026-07-07-clone-credentials-bootstrap.md`. Read the full plan. Beginning Phase 1 (server: shared `runGitWithTransientCred` helper + credentialed clone). Exploring the existing broker/credential/clone code first.

<small>took 6m 2s</small><br>
**AGENT**

> Phase 1 (a): `runGitWithTransientCred` helper landed with 3 GREEN unit tests. It mints a `prep-*` transient sid via crypto/rand, registers the clone pid so the broker ancestry walk resolves the caller-supplied PAT, and tears down cred+pid on both success and non-zero exit. Next: wire it into the clone handlers + MCP tool with structured needsAuth response.

<small>took 4m 13s</small><br>
**AGENT**

> Phase 1 DONE (server, committed). Salient diff: clone handlers + prepare_repo MCP now route git through `runGitWithTransientCred(credHost,credUsername,credToken,...)`; git auth failure -> `{needsAuth:true,host}`; success -> `{justCloned,hasRemote,initSha}`. `make test` fully GREEN (4 new unit tests incl. cloneNeedsAuth 7/7); golden updated (clone_cred.go + main.go mirrored across variants). I folded Phase 1's manual private-clone container check into the Phase 2/3 browser session so the container spins up once. Starting Phase 2 (frontend three-state credential UX).

<small>took 6m 53s</small><br>
**AGENT**

> Phase 2 DONE (frontend code committed). New Session clone form now has three-state credential UX: saved PAT applied silently (a "Using saved credentials for <host> - Change" line), fields revealed only on Change or when the server returns needsAuth. Host derived by a unit-tested pure module (parseCloneHost, 8/8). `node --check` + ascii-check + golden all green. Implementing Phase 3 (skip redundant fetch on justCloned + pre-seed autosync-trust), then one test-container browser session verifies Phases 1-3 end-to-end.

<small>took 2m 20s</small><br>
**AGENT**

> Phase 3 code committed. Bringing up the e2e test container (`make e2e-up-simple`) to browser-verify Phases 1-3. Heads up: the positive path ("valid PAT clones a private repo", "in-session git fetch/push authenticates", "silent auto-restore with no trust prompt") needs a real private repo + PAT, which I don't hold. I'll verify everything that needs no secret first (public clone + no redundant fetch; FRESH reveal on private-no-token; REJECTED on bad token; TRANSPARENT line), then ask you how you want the PAT-dependent paths verified.

<small>took 5m 18s</small><br>
**AGENT**

> Verification surfaced a real bug (good catch): a bare clone of a PRIVATE repo HANGS the handler waiting on git's interactive credential prompt, so `needsAuth` never returns. Fix: force `GIT_TERMINAL_PROMPT=0` in the child git env (both bare + credentialed paths) so git fails fast and we detect auth failure. This is a correctness fix within Phase 1 scope, not a workaround. Applying it test-first, then rebuilding the container to re-verify.

<small>took 7m 10s</small><br>
**AGENT**

> Phases 1-3 browser-verified in the container (all no-secret paths GREEN):
> - Public clone: no cred UI, justCloned, and ZERO redundant &fetch=1 (Phase 3 skip) confirmed.
> - FRESH: private-no-token -> needsAuth returns INSTANTLY (the hang fix works), fields reveal with host prefilled.
> - REJECTED: bad token -> stored to localStorage, retried, "Saved github.com token was rejected" notice.
> - TRANSPARENT: stored token attached to the POST silently (no prompt); on a successful clone the "Using saved credentials for <host> - Change" line persists.
> - Change: reveals prefilled masked fields.
> Screenshots attached. Remaining: the POSITIVE path (valid PAT clones a private repo + in-session fetch/push + silent trust auto-restore) needs a real private repo + PAT I don't hold. Sending a question next on how you'd like that verified.

<small>took 5m 52s</small><br>
**AGENT**

> Phases 1-3 are code-complete and verified for every path that doesn't need a real secret (public clone + no-fetch, FRESH, REJECTED, TRANSPARENT, Change). One integration link remains unproven live: **a VALID PAT actually cloning a private repo, then in-session git fetch/push + silent trust auto-restore.**
>
> That link is already covered indirectly: the unit test proves the transient sid is registered and the broker ancestry walk resolves the clone pid, and the broker->git-credential-swe-swe->git path is the exact same shipped code that in-session fetch/push already uses. But I haven't exercised it end-to-end for a pre-session clone.
>
> How would you like to close that gap?
>
> 1. Give me a throwaway PRIVATE repo URL + a narrowly-scoped read PAT (paste it in chat) and I'll run the true positive path live in the test container, then you can revoke it. (Note: it'll be visible in this chat history.)
> 2. I stand up a local authenticated git HTTP server inside the container (fixed user/token, no external secret) and clone from that to prove the broker-served-token path succeeds. ~Extra setup, fully self-contained.
> 3. Accept the current coverage (unit + shared-path + browser negative/transparent) as sufficient; I document the residual gap in the task log and move on to wrap-up.
>
> My recommendation: option 2 - it proves the real positive path with zero secret exposure. Which do you want?

[Quick replies]
- Option 2: local authed git server
- Option 1: I'll paste a PAT
- Option 3: accept coverage, wrap up

**USER**

> Option 3: accept coverage, wrap up

<small>took 26.7s</small><br>
**AGENT**

> Drained the queue - it's empty (nothing pending). Continuing the wrap-up: updating the task file to mark Phases 1-3 done, running the full `make test` one more time, then tearing down the test container. After that I'll walk you through each phase log for sign-off and export this chat. Anything you want to add before I finish?

[Quick replies]
- Go ahead, finish up
- Wait, I have a note

**USER**

> Go ahead, finish up

<small>took 2m 57s</small><br>
**AGENT**

> Phase log walkthrough:
>
> PHASE 1 (server) - RED->GREEN on runGitWithTransientCred + cloneNeedsAuth; wired clone handlers + prepare_repo MCP. Problem hit during browser verify: a bare private clone HUNG on git's username prompt -> fixed with GIT_TERMINAL_PROMPT=0 (test-first), re-verified. Conclusion: server credentialed-clone path done, unit tests green.
>
> PHASE 2 (frontend) - parseCloneHost pure module 8/8; three-state UX wired. Browser: Test1 public (no UI, no fetch=1), Test2 FRESH (instant needsAuth, no hang), Test3 REJECTED (token stored, rejection notice; noted rejected-token retry ~20s = git's real 401 round-trip, not a hang), Test4 TRANSPARENT (creds in POST body, persistent line on success), Change (masked prefill). Conclusion: all UI states GREEN live.
>
> PHASE 3 - no-redundant-fetch confirmed live; trust pre-seed written with the same key format as terminal-ui. Conclusion: fetch-skip verified; PAT-dependent auto-restore accepted (Option 3).
>
> Now exporting this chat and doing the final commit.

