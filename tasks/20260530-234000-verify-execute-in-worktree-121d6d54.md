# Verify execute-in-worktree: report your session config

You are the spawned agent (call yourself **B**), launched by `/swe-swe:execute-in-worktree`
into a fresh git worktree. Your only job is to **report your own session configuration** so the
originating agent (A) and the user can verify that `/swe-swe:execute-in-worktree` wired you up
correctly. Do NOT modify any code.

## Step 1 - Gather and report your config

Report every item below. Send the whole report via `send_message` (agent chat) **and** print it
to the terminal, so it is visible whether or not chat is actually wired:

1. **Agent binary** - which agent are you? (e.g. `claude`, `gemini`, `opencode`). Should match A.
2. **Working directory** - output of `pwd`. Should be a dedicated worktree path, single
   `/worktrees/<branch>` (NOT a doubled `/worktrees/worktrees/<branch>`).
3. **Git branch** - output of `git branch --show-current`. Should be the branch derived from this
   task filename: `verify-execute-in-worktree-121d6d54`.
4. **Worktree vs shared repo** - confirm you are in a git worktree and report the shared git
   common dir: `git rev-parse --is-inside-work-tree` and `git rev-parse --git-common-dir`
   (should resolve to the main repo's `.git`, e.g. `/workspace/.git`).
5. **Agent-chat tools available?** - can you see the `swe-swe-agent-chat` MCP tools
   (`send_message` / `send_progress`)? Report yes/no.
6. **Agent chat actually working?** - this is different from #5. Tool *presence* is the default in
   every claude session even with no channel. The real proof is whether a `send_progress` /
   `send_message` you emit actually reaches the chat UI. Report whether you believe chat is truly
   wired (e.g. via `--channels server:agent-chat`).
7. **Chat-vs-TUI instruction?** - did your initial instruction explicitly tell you to use agent
   chat (`send_message`/`send_progress`) instead of the TUI? Quote it if so. Report yes/no.
8. **Task file present?** - did this task file exist in your worktree when you started, or did you
   have to hunt for it across other directories? Report how you obtained it.

## Step 2 - Stop

After sending the report, stop. Do not make changes, do not commit, do not start other work.
A will read your report, compare it against the expected values below, then terminate you.

---

## Expected values (yardstick for A + the user)

| Check | Expected |
|---|---|
| 1. Agent binary | same as A (`claude`) |
| 2. pwd | single `/worktrees/verify-execute-in-worktree-121d6d54` (no doubling) |
| 3. Branch | `verify-execute-in-worktree-121d6d54` |
| 4. Worktree / common-dir | inside worktree; common-dir = main repo `.git` |
| 5. Chat tools available | yes |
| 6. Chat actually working | yes (channel wired via `--channels server:agent-chat`) |
| 7. Chat-vs-TUI instruction | yes - B was told to prefer chat over TUI |
| 8. Task file present in worktree | yes - no cross-directory hunting needed |
