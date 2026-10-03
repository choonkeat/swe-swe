<!-- agent-chat export
title: Block Askuserquestion Hook
date: 2026-06-27
index: 01
slug: block-askuserquestion-hook
agent: claude
version: 0.8.1 (0df944f)
-->

# Block Askuserquestion Hook

_2026-06-27 · 01 · claude · agent-chat 0.8.1 (0df944f)_

**USER**

> # SWE-SWE-TODO
>
> Notes for the **swe-swe maintainers** — things to fold back into swe-swe itself.
> Each item is self-contained; an agent working in the swe-swe repo can pick one up
> and implement it there.
>
> ---
>
> ## Block the built-in `AskUserQuestion` tool in agent-chat sessions (make it a default install)
>
> ### The problem
>
> Claude Code ships a built-in `AskUserQuestion` tool that renders a multiple-choice
> menu **in the local terminal TUI** ("1. Red / 2. Blue / Enter to select…").
>
> In an agent-chat session the user is talking to the agent through the **web chat
> UI**, not the TUI — so that menu is invisible to them. When the agent calls
> `AskUserQuestion`, the menu appears only in the terminal, the chat pane just sits
> at "Connecting to chat…", and the agent blocks forever waiting on input the user
> can never give. (Screenshots of both the broken and correct behaviour exist in the
> session that filed this.)
>
> The correct channel in agent-chat is the `send_message` MCP tool — its `text` +
> `first_quick_reply` / `more_quick_replies` render as a real message with clickable
> quick-reply buttons in the web UI, and it returns the user's answer.
>
> ### The fix (verified working)
>
> A global **PreToolUse hook** matched to `AskUserQuestion` that denies the call
> (exit 2, which blocks the tool and feeds stderr back to the agent) unless the
> session has explicitly opted out via an env var.
>
> Add to the **global** `~/.claude/settings.json` that swe-swe provisions for each
> session (merge into existing `hooks`, don't clobber other settings):
>
> ```json
> {
>   "hooks": {
>     "PreToolUse": [
>       {
>         "matcher": "AskUserQuestion",
>         "hooks": [
>           {
>             "type": "command",
>             "command": "[ \"$AGENT_CHAT_DISABLE\" = \"1\" ] && exit 0; echo 'BLOCKED: do not use the built-in AskUserQuestion tool — its menu renders only in the local TUI, which the user may not see (e.g. an agent-chat session). Ask via the agent-chat send_message tool instead (question -> text, primary option -> first_quick_reply, rest -> more_quick_replies). To allow the built-in tool, set AGENT_CHAT_DISABLE=1.' >&2; exit 2"
>           }
>         ]
>       }
>     ]
>   }
> }
> ```
>
> It's an **inline command** — no separate script file to ship or chmod.
>
> ### The switch: `AGENT_CHAT_DISABLE`
>
> | `AGENT_CHAT_DISABLE` | behaviour                                   |
> | -------------------- | ------------------------------------------- |
> | `1`                  | **allow** the built-in tool (exit 0)        |
> | unset                | **block** → force `send_message` (exit 2)   |
> | `0`, `true`, other   | **block** (only the literal `1` allows)     |
>
> Rationale for defaulting to *block*: agent-chat is the common case in swe-swe, and
> a wrongly-shown menu hard-hangs the agent (bad failure), whereas a wrongly-blocked
> menu just nudges the agent to `send_message` (harmless). So fail safe = block, and
> make the TUI menu opt-in.
>
> ### Why `exit 2` (not `exit 1`)
>
> For a Claude Code PreToolUse hook:
> - **exit 2** → blocks the tool call and feeds **stderr** back to the agent (this is
>   what makes the agent switch to `send_message`).
> - exit 1 / any other non-zero → non-blocking error: stderr is shown but the tool
>   **still runs**. Would not stop the menu.
>
> So the message must go to stderr (`>&2`) and the block must be exit 2.
>
> ### Decisions / things considered
>
> - **Earlier approach gated on `AGENT_CHAT_PORT`** (set by the harness only when
>   agent-chat is live). Works, but couples the hook to a harness-owned var name and
>   would need a paired check for an agent-chat-provisioned-but-disabled mode. The
>   `AGENT_CHAT_DISABLE=1`-to-allow switch is simpler and puts the control in one
>   obvious knob. swe-swe should **export `AGENT_CHAT_DISABLE=1`** in any session
>   flavour where the TUI is the real user surface (plain terminal sessions), and
>   leave it unset for agent-chat sessions.
> - **Hooks are snapshotted at session start** — Claude Code won't pick up
>   settings.json changes mid-session (security guard against live hook injection).
>   Since swe-swe writes the global settings before launching the agent, that's fine;
>   just don't expect to toggle it within a running session.
>
> ### TODO for swe-swe
>
> 1. Bake the PreToolUse hook above into the global `~/.claude/settings.json` that
>    swe-swe writes at session provisioning (idempotent merge — preserve existing
>    keys like `skipDangerousModePermissionPrompt`, `tui`, and any other hooks).
> 2. Export `AGENT_CHAT_DISABLE=1` for session flavours where the user interacts via
>    the **terminal TUI** (so the built-in menu still works there); leave it unset
>    for agent-chat / web-UI sessions.
> 3. Document the `AGENT_CHAT_DISABLE` contract wherever swe-swe documents its other `AGENT_CHAT*` / `SWE_*` env vars.

<small>took 20.4s</small><br>
**AGENT**

> Starting — locating where swe-swe provisions the global ~/.claude/settings.json so I can bake in the AskUserQuestion PreToolUse hook.

<small>took 9m 6s</small><br>
**AGENT**

> Done — the `AskUserQuestion` guard is now baked into swe-swe's session provisioning. All three TODO items addressed:
>
> **1. Hook baked into `~/.claude/settings.json`** — `cmd/swe-swe/templates/host/entrypoint.sh` (the `{{IF CLAUDE}}` block). It writes the exact PreToolUse hook you specified (matcher `AskUserQuestion`, `exit 2` block to stderr, `AGENT_CHAT_DISABLE=1` opt-out). The write is an **idempotent jq merge**: if a settings.json already exists it drops any prior `AskUserQuestion` matcher and appends ours, preserving every other key and PreToolUse entry; if none exists it writes fresh; if jq is somehow absent and a file exists it warns and leaves it untouched (never clobbers). Added a `{{CHOWN_CLAUDE}}` placeholder + `chownClaude` in `templates.go` so the dir is chowned to `app` in Docker mode.
>
> **2. `AGENT_CHAT_DISABLE=1` for terminal sessions** — already implemented. `buildSessionEnv` in `swe-swe-server/main.go:566-567` sets it for non-chat sessions and leaves it unset for agent-chat, which is exactly the contract. No change needed.
>
> **3. Documented the contract** — added `AGENT_CHAT_DISABLE` to the injected-env table plus a behaviour table (`1`=allow, unset/anything-else=block) in `docs/configuration.md`.
>
> **Verification:**
> - `make build golden-update` ran; golden entrypoint.sh diffs staged (39 claude-enabled variants; Docker variants correctly emit `chown -R app: /home/app/.claude`, non-Docker leave it blank).
> - `bash -n` on the generated script passes; the embedded JSON validates with `jq`; the merge filter parses.
> - `make test` — **all pass**.
> - ASCII-only kept (used `--` / `->` in the hook message and docs).
>
> One design note worth flagging: hooks are snapshotted at session start, but the hook *reads* `$AGENT_CHAT_DISABLE` at tool-call time — so the static file (written once at container start) + the per-session env var compose correctly. No mid-session toggling, as expected.
>
> Changes are unstaged (source) / staged (golden) but **not committed** — want me to commit them?

[Quick replies]
- Commit it
- Open a PR
- Leave uncommitted

**USER**

> export chat logs and git commit it + our changes in this session

