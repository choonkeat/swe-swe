<!-- agent-chat export
title: Fix Agent View Init Retry After Browser Backend Outage
date: 2026-07-22
index: 10
slug: fix-agent-view-init-retry-after-browser-backend-outage
session: 1930c0df58256cda
agent: claude
version: 0.8.17 (a9ccfee)
-->

# Fix Agent View Init Retry After Browser Backend Outage

_2026-07-22 · 10 · claude · agent-chat 0.8.17 (a9ccfee)_

**USER**

> What is wrong at session fb210b70-2a67-4809-b9a2-8a879d37a792 our browser backend cannot be used? Fix it and let the session know

<small>took 3m 54s</small><br>
**AGENT**

> Fixed - browser is live for session fb210b70; that session has been told to reconnect its playwright MCP.
>
> Root cause: at 14:42 UTC that session tried to allocate an Agent View browser while `swe-swe-browser-backend` was down (`dial tcp 172.17.0.1:9333: connection refused`). `mcp-lazy-init`'s POST failed, so the playwright MCP server never started. The backend came back up at 16:18 UTC, but nothing ever re-triggered the failed allocation.
>
> What I did: POSTed `/api/session/fb210b70.../browser/start`. Backend allocated slot 0 (cdp 6000, vnc 7000), tunnel connected, CDP verified live - Chrome/150.0.7871.100 on the session's local port 6041.
>
> The gap: commit 0e8b34e77 only recovers an allocation *lost* mid-session (404 on the tunnel upgrade). A *failed initial* allocation has no retry at all - the session is browser-less until it ends. Session 25263e68 hit the same thing at 16:11 UTC.
>
> Next: want me to fix the initial-allocation path so `browser/start` retries with backoff instead of failing permanently? About an hour, on a branch.

[Quick replies]
- Yes, fix the initial-allocation retry
- Also fix session 25263e68 now
- Just leave it, thanks

**USER**

> Yes, fix the initial-allocation retry

<small>took 2m 0s</small><br>
**AGENT**

> Fix written, unit tests green. Now running `make build golden-update` (the template copy of mcp-lazy-init is in golden testdata), then `make test`. About 5 minutes.

<small>took 2m 54s</small><br>
**AGENT**

> Done: `git log fix/mcp-lazy-init-retry` -> 101ad2b95. Branch committed, `make test` fully green, main left clean and unchanged.
>
> The bug was one level lower than I said. `mcp-lazy-init` fires init on the first `tools/call`, and it set `initDone = true` even when init FAILED - "forwarding tools/call anyway". So one failure latched permanently and every later browser tool call skipped init. That is why fb210b70 was dead for 9 hours after the backend came back.
>
> Two changes, both in `cmd/mcp-lazy-init/main.go`:
> 1. `run()` only latches `initDone` on success - the next tool call re-attempts. Costs one failed tool call, not the session.
> 2. `doInit()` retries transient failures (transport errors, 5xx) 4 times with 1s/2s/4s backoff, so a backend merely mid-restart is absorbed invisibly. 4xx is not retried - a bad key never fixes itself.
>
> `mcp-bridge.ts` spawns the same binary, so the mcp-less path gets it too. 4 new tests cover 5xx-exhausts, 4xx-fails-fast, recovers-on-3rd-attempt, and the retry-on-next-tools-call regression. Golden files regenerated: 49 files, all mcp-lazy-init copies, nothing else.
>
> Ships at the next image rebuild - it is a container binary, so a reboot is required.
>
> Next: merge `fix/mcp-lazy-init-retry` into main with `--no-ff`? Separately, session 25263e68 hit the same failure at 16:11 and is still browser-less - I can revive it the same way I did fb210b70, in about 30 seconds.

[Quick replies]
- Merge it into main
- Revive 25263e68 first
- Leave the branch for now

**USER**

> 🎤 Revive it and tell the session

<small>took 37.7s</small><br>
**AGENT**

> Session 25263e68 is revived. Its CDP is live on port 6040 running Chrome 150, and I've told that session to reconnect its playwright MCP. Both stranded sessions are back now, fb210b70 and 25263e68, neither needed a restart. I checked the queue, nothing new, just a redelivery of your revive request. The actual fix is committed on the branch fix slash mcp-lazy-init-retry, tests green, main untouched. Want me to merge that branch into main?

[Quick replies]
- Merge it into main
- Leave the branch for now
- Show me the diff first

**USER**

> 🎤 Yes

<small>took 14.4s</small><br>
**AGENT**

> Merged. Main is now at commit 5ef82852b, a no-fast-forward merge of the fix branch, and the tests still pass on the merged tree. It's local only, I haven't pushed. The fix is a container binary so it goes live at the next image rebuild and reboot. Want me to push main?

[Quick replies]
- Push main
- Leave it local
- Reboot to deploy it

**USER**

> 🎤 What is our current directory

<small>took 2.6s</small><br>
**AGENT**

> Slash workspace. That's the swe-swe repo, on branch main. Should I push it?

[Quick replies]
- Push main
- Leave it local
- Reboot to deploy it

**USER**

> 🎤 Yes

<small>took 1m 20s</small><br>
**AGENT**

> Pushed. Main is on GitHub at ee7221989. The timestamp shifter did its usual rewrite-then-abort dance, and one thing blocked it, the modified agent-chats index dot html, so I stashed that for the push window and popped it right back. It's untouched. I also noted down the manual revive procedure so I don't have to rediscover it before this box reboots. One loose end, the local branch fix slash mcp-lazy-init-retry still points at the pre-rewrite commit. Want me to delete it?

[Quick replies]
- Delete the branch
- Keep it
- Reboot to deploy the fix

**USER**

> 🎤 Sure delete

<small>took 7.6s</small><br>
**AGENT**

> Deleted. I checked the content matched what's on origin main first, so nothing was lost. Everything's wrapped up: both stranded sessions revived, the fix merged and pushed to main, branch cleaned up. The only thing left is that the fix is a container binary, so it won't actually be live until the next image rebuild and reboot. Want me to do that reboot now?

[Quick replies]
- Reboot to deploy it
- Not now
- Later, I'll do it myself

**USER**

> Run /swe-swe:commit-session-chat-log to freeze, scrub and commit this session's chat log. When the commit has landed, end this session by calling the swe-swe MCP tool end_session with uuid 82bba603-1b8a-497c-a3fa-1981b7e3c36e. If you cannot commit it (for example a screenshot leaks a secret), say so and do NOT end the session.

