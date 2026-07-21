<!-- agent-chat export
title: Browser Backend Killed By Reboot Light Root Cause And Fix
date: 2026-07-22
index: 04
slug: browser-backend-killed-by-reboot-light-root-cause-and-fix
session: 85dbf7273b913442
agent: claude
version: 0.8.17 (a9ccfee)
-->

# Browser Backend Killed By Reboot Light Root Cause And Fix

_2026-07-22 · 04 · claude · agent-chat 0.8.17 (a9ccfee)_

**USER**

> I’m trying out our recent changes

**USER**

> Rebooted

<small>took 3m 22s</small><br>
**AGENT**

> Found a live regression from the reboot: `swe-swe-browser-backend` is Exited(137) and stayed down, so Agent View / MCP browser is dead right now. Root-causing + restarting it.

<small>took 2m 0s</small><br>
**AGENT**

> Browser is back up -- I restarted `swe-swe-browser-backend` and verified it (MCP browser navigated to example.com successfully). Try Agent View now.
>
> The reboot killed it. Root cause found, and it's the same bug that was open since 2026-07-21:
>
> 1. `/reboot-light`'s kill list was "every container except self and `swe-swe-tunneld`" -- so it `docker kill`ed the browser backend as a "stray". `--restart=always` does not re-apply after an explicit kill, so it stayed dead. Proof: your own crash forensics logged `kill swe-swe-browser-backend` 15s before `kill <swe-swe container>`.
> 2. Fixed `.claude/commands/reboot-light.md` -- backend is now a named protected container alongside tunneld.
> 3. Fixed `.swe-swe/restart-loop2.sh` -- new `ensure_browser_backend()` runs every loop iteration and starts it if down (backup at `.bak-20260722`, syntax-checked, patched via rename so the running loop was not corrupted).
> 4. Updated `tasks/2026-07-21-browser-backend-silent-death.md` to root-caused + fixed.
>
> Everything else from the reboot looks healthy: stack up 20min, this build has `reboot_server`, the end-session chat-log dialog, and Agent-Chat-default New Session.
>
> Next: `git add .claude/commands/reboot-light.md tasks/2026-07-21-browser-backend-silent-death.md && git commit` -- want me to commit those two?

[Quick replies]
- Yes, commit those
- Not yet
- What else did the reboot break?

**USER**

> Run /swe-swe:commit-session-chat-log to freeze, scrub and commit this session's chat log. When the commit has landed, end this session by calling the swe-swe MCP tool end_session with uuid d9caa9ac-76e4-4469-8b7d-8e6a0f550d6f. If you cannot commit it (for example a screenshot leaks a secret), say so and do NOT end the session.

