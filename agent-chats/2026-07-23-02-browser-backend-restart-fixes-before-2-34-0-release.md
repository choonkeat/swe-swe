<!-- agent-chat export
title: Browser Backend Restart Fixes Before 2 34 0 Release
date: 2026-07-23
index: 02
slug: browser-backend-restart-fixes-before-2-34-0-release
session: b6c1965db0fad9bf
agent: claude
version: 0.8.17 (a9ccfee)
-->

# Browser Backend Restart Fixes Before 2 34 0 Release

_2026-07-23 · 02 · claude · agent-chat 0.8.17 (a9ccfee)_

**USER**

> Rebooted. What are we testing so we can make a release?

<small>took 10.9s</small><br>
**AGENT**

> Checking what is unpushed on main and which post-reboot verifications are still owed.

<small>took 33.6s</small><br>
**AGENT**

> Release candidate = 9 functional commits on top of 2.33.0 (main == origin/main, nothing unpushed). Next version: 2.34.0.
>
> **Test list, ranked (about 45 min total)**
>
> 1. **mcp-lazy-init retry** (a0746c4d0) — the only fix that needed this rebuild to go live. `docker rm -f swe-swe-browser-backend`, wait for restart=always, then in a session run `/mcp` — Agent View must recover instead of staying dead. ~5 min.
> 2. **Codex chat-log export** (25bf9cefa) — start a Codex session, send one message, confirm a file appears under `agent-chats/`. Proves AGENT_CHAT_EVENT_LOG/EXPORT_DIR forwarding. ~10 min.
> 3. **End-session flow** (2cc9b9dab + b644e2e16) — in a throwaway session hit End: "Commit the log, then end" must be preselected, and confirming must land on the homepage. ~10 min.
> 4. **New Session dialog** (3f11d5af4 + 3 iOS-zoom commits) — branch dropdown shows plain names (no `origin/`); inputs all >=16px font. I can check the dropdown live and the font-size in source; actual iOS pinch-zoom only you can confirm on your phone. ~10 min.
> 5. **reboot-light browser-backend guard** (ce7704a9c) — only verifiable by running reboot-light, which kills this session. Suggest deferring to the next light reboot rather than spending a cycle now.
>
> Plus `make test` as the baseline gate before the bump.
>
> Start with 1 (kill the browser-backend and retry), or want me to run `make test` first?

[Quick replies]
- Start with 1
- Run make test first
- Run 1-4 in order, report at the end
- Just bump and release

<small>took 2m 30s</small><br>
**AGENT**

> `make test` passed (exit 0). Baseline gate clear — still waiting on your pick for tests 1-4.

<small>took 5.3s</small><br>
**AGENT**

> `make test` passed (exit 0), so the release gate is clear. Still need your pick from the 5-item test list above — which do I run?

[Quick replies]
- Start with 1 (kill browser-backend, /mcp retry)
- Run 1-4 in order, report at the end
- Just bump and release
- Skip testing for now

**USER**

> Start with 1 (kill browser-backend, /mcp retry)

<small>took 6.1s</small><br>
**AGENT**

> Test 1 running. Step 1 of 4: baseline Agent View check before killing the backend.

<small>took 28.0s</small><br>
**AGENT**

> Heads-up: `docker rm -f` removed the browser-backend container outright (restart=always cannot resurrect a removed container). Locating how it is launched so I can bring it back.

<small>took 49.7s</small><br>
**AGENT**

> Test 1 PASSED — Agent View self-heals after the backend dies.
>
> What I did: `docker rm -f swe-swe-browser-backend` → recreated it with the documented `docker run` → called the MCP browser again. First call returned "Target page/context closed" (stale handle), the very next call re-allocated a fresh browser and `https://example.com` loaded with a real snapshot. No server restart, no `/mcp` reconnect needed. That is exactly what ee7221989 was supposed to fix.
>
> One gap found while doing it: `docker rm -f` deletes the container, and `ensure_browser_backend()` in `.swe-swe/restart-loop2.sh:103` only `docker start`s an *existing* container — if it is ever removed it just logs "recreate it manually" and Agent View stays dead. I recreated it by hand this time. Making that function recreate from the `docker run` in the header comment is a ~10-line change.
>
> Next: test 2, Codex chat-log export (start a Codex session, one message, confirm a file lands in `agent-chats/`). ~10 min.

[Quick replies]
- Run test 2 (Codex export)
- Fix ensure_browser_backend first
- Run tests 2-4 back to back
- Skip ahead and bump to 2.34.0

**USER**

> Fix ensure_browser_backend first

<small>took 3m 21s</small><br>
**AGENT**

> `ensure_browser_backend` fixed and live-tested — `.swe-swe/restart-loop2.sh:114` now has `create_browser_backend()` holding the canonical `docker run`; backup at `restart-loop2.sh.bak-20260723`.
>
> Three paths tested against the real container:
> - running -> no-op
> - **removed** (`docker rm -f`) -> recreated, "container does not exist -- creating it", verified `restart=always`, `--add-host host.docker.internal`, all 61 port mappings, all 3 env vars
> - stopped -> `docker start`
>
> Installed by `mv` (new inode), so the loop currently running in screen still executes the old code in memory; it picks up the fix on its next restart. Nothing to do now.
>
> **New bug found while testing** — `docker stop` + `docker start` of the backend leaves it silently broken:
> ```
> Xvfb exited with error (exit status 1)
> Chromium exited with error (exit status 1)
> browser-backend: allocated ... (slot 0, cdp 6000, vnc 7000)   <-- reports SUCCESS
> http: proxy error: dial tcp 127.0.0.1:6020: connection refused
> ```
> Cause: `/tmp/.X10-lock` and `/tmp/.X11-unix/X10` survive a stop/start (filesystem is preserved), so Xvfb refuses display :10 and Chromium dies with it — but allocation still returns a healthy-looking session. `docker/browser-backend/entrypoint.sh` (17 lines) does no cleanup. Two-part fix: clear stale X locks + chromium session dirs at entrypoint, and make allocation fail loudly when Xvfb/Chromium exit immediately. Needs an image rebuild (~10 min) to verify.
>
> Backend is healthy right now (recreated clean, example.com loads).
>
> Fix the X-lock bug next, or go back to test 2 (Codex chat-log export)?

[Quick replies]
- Fix the X-lock bug
- Back to test 2 (Codex export)
- Just the entrypoint cleanup, skip the loud-failure part
- File it as a task and move on

**USER**

> Fix the X-lock bug

<small>took 1m 9s</small><br>
**AGENT**

> Both fixes written (entrypoint cleanup + fail-loud allocation). Running `make build golden-update` now, then rebuilding the browser-backend image to test live. ~10 min.

<small>took 5m 45s</small><br>
**AGENT**

> X-lock bug fixed and committed — `ee1d46032`, 51 files (2 source + 49 golden). `make test` green.
>
> Both halves proven live:
> 1. **Cleanup** — `docker/browser-backend/entrypoint.sh` now clears `/tmp/.X*-lock`, `/tmp/.X11-unix/*` and chromium session dirs at boot. Rebuilt the image, then `docker stop` + `docker start`: Xvfb and chromium start clean, `example.com` loads through the MCP browser. That exact sequence gave 502 / connection refused an hour ago.
> 2. **Fail loud** — `startBrowserProcs` now checks whether Xvfb/chromium died during the startup sleep. Poisoned `/tmp/.X11-lock` inside the container and called `POST /sessions`:
> ```
> failed to start browser: Xvfb on display :11 exited immediately
>   (exit status 1) -- stale X lock or display in use
> ```
> instead of returning a fake-healthy session. Removed the lock, allocation succeeded on slot 1, then deleted the test session.
>
> Release count is now 10 functional commits on top of 2.33.0. Backend is healthy and running the new image.
>
> Next: test 2, Codex chat-log export — start a Codex session, send one message, confirm a file lands in `agent-chats/`. ~10 min.

[Quick replies]
- Run test 2 (Codex export)
- Run tests 2-4 back to back
- Bump to 2.34.0 and release now
- Stop here

**USER**

> Run /swe-swe:commit-session-chat-log to freeze, scrub and commit this session's chat log. When the commit has landed, end this session by calling the swe-swe MCP tool end_session with uuid e1cbe56c-36cf-4ca0-b0fe-32d4cde797d0. If you cannot commit it (for example a screenshot leaks a secret), say so and do NOT end the session.

