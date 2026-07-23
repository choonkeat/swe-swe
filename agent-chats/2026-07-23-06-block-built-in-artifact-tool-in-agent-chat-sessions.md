<!-- agent-chat export
title: Block Built In Artifact Tool In Agent Chat Sessions
date: 2026-07-23
index: 06
slug: block-built-in-artifact-tool-in-agent-chat-sessions
session: 5b876368ccea74c1
agent: claude
version: 0.8.18 (0d8ceea)
-->

# Block Built In Artifact Tool In Agent Chat Sessions

_2026-07-23 · 06 · claude · agent-chat 0.8.18 (0d8ceea)_

**USER**

> Swe-swe comes bundled with disabled AskUserQuestion because we use agent chat (or when agent chat is active). 
>
> I’m thinking we disable clause artifacts because agent should produce html and show in Preview

<small>took 5m 34s</small><br>
**AGENT**

> Done, uncommitted. Say "commit" and I'll commit the 46 files.
>
> Artifact is now blocked the same way AskUserQuestion is:
>
> 1. New `cmd/swe-swe/hook-scripts/swe-swe-artifact-guard.sh` — exit 2 with "write the HTML into the workspace, serve it, open App Preview".
> 2. Wired as a `PreToolUse` matcher `Artifact` in both installers: container `entrypoint.sh` (jq merge drops any prior `Artifact` entry, so re-init never duplicates) and dockerless `writeDockerlessHooks`.
> 3. Same gating as the ask guard: only fires when the session actually has an agent-chat channel, so terminal TUI and plain `claude` runs are unaffected. Escape hatch `SWE_ALLOW_ARTIFACTS=1` (or `AGENT_CHAT_DISABLE=1`).
> 4. `make build golden-update` — 41 entrypoint.sh goldens, diff is exactly the new heredoc + matcher + comment text, nothing else.
> 5. `make test` all green (dockerless hook test extended to assert the stale-`Artifact`-entry replacement). CHANGELOG + `docs/configuration.md` updated.
>
> One thing I did not do: the guard fires at tool-call time, not at prompt time, so the agent still burns a turn discovering the block. If you want it cheaper, the next step is a line in the entrypoint-seeded CLAUDE.md telling agents up front to use App Preview — about 10 minutes.
>
> Next: reply "commit" to land this, or "prompt too" to add the CLAUDE.md nudge first.

[Quick replies]
- commit
- prompt too
- show me the guard script
- revert it

**USER**

> > write the HTML into the workspace, serve it, open App Preview
>
> write the HTML into the workspace mockups/, serve it on PORT env, and give user a link to click that opens in their Preview

<small>took 2m 54s</small><br>
**AGENT**

> Guard message rewritten to your 3 steps. Still uncommitted — say "commit" to land it.
>
> The block now tells the agent, verbatim:
>
> 1. Write the page to `mockups/<name>.html` (create `mockups/` if needed).
> 2. Serve it on the session's `$PORT`: `python3 -m http.server "$PORT" --directory mockups` (or a Procfile entry via `/swe-swe:procfile`).
> 3. Put `http://localhost:$PORT/<name>.html` in the `send_message` text, with the real port substituted.
>
> Step 3 works because `agent-chat-dist/app.js:52` already intercepts localhost/`*.lvh.me` links in chat bubbles and posts `agent-chat-open-preview` to the parent — the user gets one click straight into App Preview, no new tab. So this needed no frontend change.
>
> Verified: all three guard branches (block / `AGENT_CHAT_DISABLE=1` / no agent-chat channel) behave; `make test` green; goldens regenerated and all 49 generated `entrypoint.sh` files pass `sh -n` (the guard body is a heredoc inside a heredoc, so that check mattered).
>
> Next: reply "commit" — 46 files, one commit.

[Quick replies]
- commit
- show me the full block message
- use swe-npx serve instead of python3
- hold off

**USER**

> I don’t think we need to be prescriptive on exact python command line

<small>took 1m 57s</small><br>
**AGENT**

> Dropped the python3 command. Step 2 now reads:
>
> > Serve that directory on the session's $PORT with whatever static server suits the project (background it, or add a Procfile entry -- see /swe-swe:procfile).
>
> Same edit applied to `docs/configuration.md`. Rebuilt, goldens regenerated, `make test` green, all 49 generated entrypoints parse.
>
> Still uncommitted: 46 files (guard script, `templates.go`, `entrypoint.sh`, `dockerless.go` + its test, 41 goldens, CHANGELOG, docs).
>
> Next: reply "commit" to land it.

[Quick replies]
- commit
- show me the final block message
- hold off

**USER**

> commit

<small>took 25.8s</small><br>
**AGENT**

> Committed: `7206038b4` — 48 files, local only (not pushed).
>
> Live at the next container rebuild + reboot; dockerless picks it up on the next `swe-swe init`. This session's own hooks were snapshotted at start, so Artifact is still callable here until then.
>
> Next: reply "push" if you want it on origin/main now — otherwise it rides along with the next release.

[Quick replies]
- push
- leave it local
- wrap up the session

**USER**

> push

**USER**

> /wrap-up and /reboot-light

<small>took 27.2s</small><br>
**AGENT**

> Pushed. `origin/main` is now at `bf6182aa8`, with `7206038b4` (the Artifact guard) under it.
>
> Two things worth knowing:
>
> 1. A sibling session's commit rode along — `bf6182aa8 fix(terminal-ui): send one Esc, not two, on agent-chat interrupt`. It was already on local main, so pushing main necessarily published it. I checked the diff first: terminal-ui.js + goldens + CHANGELOG, nothing sensitive.
> 2. No SHA rewrite this time — timestamp-shifter reported "All timestamps OK" and pushed on the first attempt, so no second push was needed.
>
> `agent-chats/index.html` is still modified in the working tree — that's the chat-log exporter regenerating it live, not anything I changed. I stashed it to clear the push, and dropped the stash after confirming byte-identical content.
>
> Next: reply "wrap up" to run the end-of-session chat-log commit, or leave it and the guard ships at the next rebuild.

