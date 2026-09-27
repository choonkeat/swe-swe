# Multi-agent smoke test: fixes

## Status: IN PROGRESS - fixes 1 and 2 verified live, fix 3 done

## Context

2026-09-26 smoke test of Codex, OpenCode and Pi, one agent at a time, driven
from another session via `create_session` + `send_chat_message` (no browser
attached). Same 4 tests per agent, long wait last:

| Test                      | Codex                | OpenCode             | Pi                          |
|---------------------------|----------------------|----------------------|-----------------------------|
| 1. Start + chat           | pass                 | pass                 | pass                        |
| 2. `/ck:run-marp`         | pass                 | fail                 | fail, then pass with a hint |
| 3. Stop own marp server   | pass                 | fail                 | fail (false "stopped")      |
| 4. 7 min silent, then msg | fail, then recovered | fail, then recovered | pass                        |

Versions: codex-cli 0.157.1 (gpt-6-astra), opencode 1.18.325 (Claude Sonnet
4.5), pi (openai-codex gpt-5.5). Recordings: the three `smoke-*` sessions
ending around 09:25 UTC.

What already works: in every agent, a new chat message wakes an agent that
gave up waiting, and no message was lost.

## Fixes, ranked

### 1. Wake-up nudge fires while the agent is busy (swe-swe bug)

**DONE** in 455520677; verified live on Pi after reboot 2026-09-26
(`/ck:run-marp` ran to completion, no stray nudge).

- **Where:** `wakeAgentForQueuedChat`,
  `cmd/swe-swe/templates/host/swe-swe-server/main.go:9650`
- **Symptom:** Pi was working on `/ck:run-marp` when
  `check_messages; reply me with a send_message` was typed into its terminal.
  `check_messages` returned empty, Pi replied "No pending chat messages." and
  dropped the task. Pi got the same stray nudge again in tests 2 and 3.
  OpenCode's screen also showed the nudge QUEUED during work.
- **Cause:** after `chatNudgeDelay` (4s) it checks `agentIsParkedOnChat`. A
  message pushed while the agent is parked in `send_message` is consumed
  right away (as the reply to that call). 4s later the agent is busy with it,
  so it is not parked, so the nudge fires. The question it asks ("is anyone
  parked?") is not the one that matters ("is this message still unread?").
- **Fix:** before typing, check that the pushed message is still unconsumed.
  Either (a) look for a `userMessagesConsumed` event with that id in chat
  history, or (b) add a `queue_pending` orchestrator tool to agent-chat.
  (a) needs no agent-chat release. Unit test: message consumed by a parked
  `send_message` -> no nudge; message still queued -> nudge.
- **Estimate:** about 2 hours.

### 2. Codex stops waiting for a chat reply after 5 minutes

**DONE** in b0ebe9035; verified live after reboot 2026-09-26: Codex sat in
`send_message` for 7m14s with no timeout, then the next chat message was
consumed by that same `send_message` call and it answered.

- **Where:** `cmd/swe-swe/mcpspec.go:186` (`mcpCodexTOML`) and `:208`
  (`mcpCodexFlags`)
- **Symptom:** `send_message` failed with
  `timed out awaiting tools/call after 300s` and Codex ended its turn. The next
  message woke it (via the nudge) and it answered.
- **Fix:** emit `tool_timeout_sec = 86400` for the `swe-swe-agent-chat` server
  only (TOML line + `-c mcp_servers.swe-swe-agent-chat.tool_timeout_sec=86400`).
  Then `make build golden-update`.
- **Estimate:** about 20 minutes.

### 3. OpenCode stops waiting after about 1 minute and repeats its last message

**DONE**: OpenCode's MCP client passes the per-server `timeout` (ms) to
`callTool`, falling back to `experimental.mcp_timeout`, then the MCP SDK
default of 60000ms. swe-swe set neither, hence the ~60s give-up. Now
`"timeout": 86400000` on `swe-swe-agent-chat` only (`mcpOpencodeSpecs`,
shared `ToolTimeoutSec` field with the Codex fix); `opencode debug config`
(1.18.32) accepts it.

- **Symptom:** `send_message` re-sent the same text about 63s later (the user
  sees it twice), then `check_messages`, then idle. The next message woke it.
- **Cause:** unknown. Its MCP config (`~/.config/opencode/opencode.json`) sets
  no timeout; find which opencode setting bounds a tool call.
- **Estimate:** about 1 hour to find, plus a small config change.

### 4. OpenCode permission prompt is invisible from chat

- **Symptom:** reading `~/.config/opencode/command/ck/run-marp.md` raised
  "Permission required: Access external directory" in the TUI only. A chat
  user would wait forever.
- **Fix:** pre-allow the command dirs (and `~/.swe-swe/commands`) in the
  opencode config swe-swe writes.
- **Estimate:** about 30 minutes.

### 5. `/ck:run-marp` instructions are unsafe for OpenCode and Pi

- **Where:** `run-marp.md` in the separate `ck` command repo (not swe-swe)
- **Symptoms:**
  - OpenCode never looked up the command at first and ran a one-off
    conversion instead. With a hint it ran the server in the foreground and
    froze until two Esc presses in the TUI (which also killed the server).
  - Pi killed the `nohup` wrapper PID, but the `node .../marp --server` child
    kept serving 200 on port 3001. Its port check printed nothing, and Pi
    reported "stopped".
- **Fix:** tell the agent to start it with
  `nohup ... > /tmp/marp-$PORT.log 2>&1 &`, record the PID, stop it by
  process group (or by the PID found listening on `$PORT`), then check with
  `curl` that the port no longer answers.
- **Estimate:** about 20 minutes.

## Later (not ranked)

- Chat messages sent right after `create_session` fail with
  `connection refused` on the agent-chat port. It worked a few seconds later.
  `create_session` could wait until agent-chat is ready.
- OpenCode and Pi don't treat a chat message starting with `/name` as a slash
  command to look up. Codex does. swe-swe could append the resolved command
  file path when a chat message starts with a known command.
