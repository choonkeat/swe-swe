---
description: Create a worktree session and execute a task plan in it
---

$ARGUMENTS

Create a new agent session on a worktree branch and execute a task plan file in it.

## Steps

1. **Parse the task file path** from the arguments (e.g., `tasks/2026-03-12-per-session-chrome-vnc.md`)
2. **Derive a branch name** from the task filename:
   - Strip the `tasks/` prefix and `.md` suffix
   - Remove the date prefix (e.g., `2026-03-12-`)
   - Result: `per-session-chrome-vnc`
3. **Commit the task file** so the new worktree can see it. A fresh `git worktree` only contains *committed* content -- an uncommitted or untracked task file is invisible to the spawned session and it will waste turns hunting for it. If the task file is not yet committed on the current branch, run `git add {task file path} && git commit -m "..."` before continuing. (Skip if it is already committed.)
4. **Create a new session** using the `create_session` MCP tool:
   - `assistant`: same agent as current session (e.g., `claude`)
   - `name`: the derived branch name
   - `branch`: the derived branch name
   - `extra_args`: always pass `--channels server:agent-chat` so the spawned session gets a working chat channel wired to the UI. (Note: the agent-chat MCP tools may *appear* available by default even without a channel -- passing `--channels` is what actually wires chat to the UI, so do not skip it.)
5. **Wait 15 seconds** for the agent to initialize
6. **Send the task command with the directive appended, as ONE input** using `send_session_input`. The slash command MUST come first (so the TUI recognises and expands it); the directive text after the task path flows into the command's `$ARGUMENTS` and is read in the same turn:
   - Text: `/swe-swe:execute-step-by-step {task file path} -- IMPORTANT: The user is watching the agent chat UI, not your terminal. Use send_message / send_progress for ALL user-visible output -- do not rely on the TUI.\n`
   - Keep it on a SINGLE line with one trailing `\n` -- an embedded newline submits the command early, and the rest queues as a separate draft that never runs.
   - Do NOT send the directive as a separate message before the command. A separate input starts its own turn; the agent finishes it with "no pending instruction" and the real command stays queued and never executes.
7. **Report back via chat UI**: Use `send_message` (not just terminal text) to tell the user the session UUID, branch name, and that work has started. The user cannot see your terminal output -- always communicate via `send_message` or `send_progress`.

## Example

Given: `/swe-swe:execute-in-worktree tasks/2026-03-12-per-session-chrome-vnc.md`

Creates session `per-session-chrome-vnc` on branch `per-session-chrome-vnc` and sends:
```
/swe-swe:execute-step-by-step tasks/2026-03-12-per-session-chrome-vnc.md
```
