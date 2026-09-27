# Multi-agent smoke test: fixes

## Status: DONE - fixes 1-6 verified live (Later items open)

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
(1.18.32) accepts it. Verified live 2026-09-27 (runtime opencode.json patched
by hand, same content the next boot writes): OpenCode sat in `send_message`
for 3m14s with no repeat, then that same call consumed the next message.

- **Symptom:** `send_message` re-sent the same text about 63s later (the user
  sees it twice), then `check_messages`, then idle. The next message woke it.
- **Cause:** unknown. Its MCP config (`~/.config/opencode/opencode.json`) sets
  no timeout; find which opencode setting bounds a tool call.
- **Estimate:** about 1 hour to find, plus a small config change.

### 4. OpenCode permission prompt is invisible from chat

**DONE**; verified live 2026-09-27. The container's `opencode.json` now
carries `permission.external_directory` = allow for
`/home/app/.config/opencode/command/*` and `/home/app/.swe-swe/commands/*`
(`opencodeExternalDirs` in `mcpspec.go`). Before: reading `ck/run-marp.md`
raised the prompt. After: `ck/run-marp.md` and `swe-swe/setup.md` (via the
symlink) both read with no prompt. Dockerless is untouched (user's own
permissions).

- **Symptom:** reading `~/.config/opencode/command/ck/run-marp.md` raised
  "Permission required: Access external directory" in the TUI only. A chat
  user would wait forever.
- **Fix:** pre-allow the command dirs (and `~/.swe-swe/commands`) in the
  opencode config swe-swe writes.
- **Estimate:** about 30 minutes.

### 5. `/ck:run-marp` instructions are unsafe for OpenCode and Pi

**DONE** in slash-commands 68e45ab (unpushed); verified live 2026-09-27 on Pi
and OpenCode concurrently: both started it with `setsid nohup ... &`, got
`200`, returned to chat; both stopped it by process group and saw `000`; no
marp process left.

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

### 6. Second Codex session gets the first one's chat env

**DONE** in 0181263b6; verified live 2026-09-27.

- **Symptom:** (reported from another box on origin/main) a Codex session in
  a worktree exported its chat log to `/workspace/agent-chats`. Reproduced
  here: a second concurrent Codex session got NO agent-chat at all
  (`listen tcp 0.0.0.0:4001: bind: address already in use`, env from the
  first session, in `~/.codex/logs_2.sqlite`).
- **Cause:** Codex 0.157 runs one shared `codex app-server --managed-daemon`
  per `CODEX_HOME`. It launches every session's MCP servers with the env of
  whichever session started the daemon, so `env_vars` forwards the wrong
  `AGENT_CHAT_PORT`, `AGENT_CHAT_EXPORT_DIR`, `SESSION_UUID`, ...
- **Fix:** all four Codex commands in `assistantConfigs` pass `--no-daemon`
  (fork/resume inherit it through the base command).
- **Verified:** two concurrent `--no-daemon` sessions (`/workspace` and a
  worktree) each got their own agent-chat, port and export dir; worktree log
  landed in the worktree; chat round trip, 7m40s `send_message` wait, and no
  stray nudge after the reply all still pass.
- **Caveat:** dockerless users on a Codex too old to know `--no-daemon`
  would fail to start; the container always installs the latest Codex.

## Later (not ranked)

- Chat messages sent right after `create_session` fail with
  `connection refused` on the agent-chat port. It worked a few seconds later.
  `create_session` could wait until agent-chat is ready.
  **DONE** in 573050541 (other way round: `pushChatMessage` retries only
  ECONNREFUSED every 1s for up to 60s, for the `send_chat_message` MCP tool
  and commit-log-then-end). Unit-tested; live check pending the next reboot:
  `create_session` then `send_chat_message` immediately -> "message pushed".
- OpenCode and Pi don't treat a chat message starting with `/name` as a slash
  command to look up. Codex does. swe-swe could append the resolved command
  file path when a chat message starts with a known command.
- Codex sessions never get `agent_session_id` captured (seen with and
  without `--no-daemon`): the spawn watch gives up after 10s
  (`agent_session_id.go:237`), but Codex writes its rollout file only at the
  first turn. Fork falls back to fingerprint recovery.
