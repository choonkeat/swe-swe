# Research: Claude Code Session JSONL for Agent Chat Permission Management

**Date**: 2026-02-10
**Status**: Research complete

## Context

When a user operates via Agent Chat instead of the TUI, they can't approve/deny tool permissions. We need a way to detect pending permission prompts and present them in the Agent Chat UI. This is **Claude Code specific** — other agent CLIs would need different mechanisms.

## Claude Code Session JSONL

Each Claude Code session writes a JSONL transcript to:
```
~/.claude/projects/{sanitized-cwd}/{session-id}.jsonl
```

Path derivation:
- CWD `/workspace` → sanitized as `-workspace`
- Session ID is a UUID, e.g. `9178fbfb-576c-4c3c-bd91-cb8560952cf6`
- Full path: `~/.claude/projects/-workspace/9178fbfb-576c-4c3c-bd91-cb8560952cf6.jsonl`

## How to Find the Current Session File

**Robust approach** (recommended):
1. Send the agent a message containing a random UUID marker via PTY stdin (e.g. `say d4f7a2b1-...`)
2. List most recently modified `.jsonl` files: `ls -t ~/.claude/projects/-workspace/*.jsonl | head -5`
3. Search those files for the UUID marker (e.g. `grep d4f7a2b1-... *.jsonl`) to confirm the right session
4. This handles edge cases where multiple sessions are active or files from other sessions were recently updated

**Simple approach** (less robust):
- `ls -t ~/.claude/projects/-workspace/*.jsonl | head -1` — most recently modified file is usually the active session

## JSONL Entry Types

| Type | Description |
|------|-------------|
| `assistant` | Model responses, contains `tool_use` blocks |
| `user` | User messages and `tool_result` responses |
| `system` | System messages |
| `progress` | Subtypes: `hook_progress`, `mcp_progress`, `waiting_for_task` |
| `queue-operation` | Task notifications (enqueue/dequeue) |
| `file-history-snapshot` | File change tracking |

Every entry includes:
- `sessionId` — the session UUID
- `type` — one of the above
- `uuid` — unique entry ID
- `parentUuid` — links to the previous entry
- `timestamp` — ISO 8601
- `message` — the actual content (for `assistant` and `user` types)

## How Permission Prompts Appear

When Claude wants to use a tool and needs permission:

1. An `assistant` entry is written with `tool_use` in `message.content`
2. **GAP** — Claude Code TUI shows the permission prompt, waits for user y/n keystroke
3. A `user` entry is written with `tool_result` content (either the actual result or a denial)

If denied, the `tool_result` content says:
> "The user doesn't want to proceed with this tool use. The tool use was rejected (eg. if it was a file edit, the new_string was NOT written to the file). STOP what you are doing and wait for the user to tell you how to proceed."

## Tool Use Entry Structure

From the `assistant` JSONL entry, `message.content` is an array containing:
```json
{
  "type": "tool_use",
  "id": "toolu_01abc...",
  "name": "Bash",
  "input": { "command": "git status", "description": "Show working tree status" }
}
```

Tool names observed: `Bash`, `Read`, `Write`, `Edit`, `Glob`, `Grep`, `Task`, various MCP tools (`mcp__agent-chat__send_message`, etc.)

## Detection Algorithm

```
tail -f {session.jsonl}
  → parse each line as JSON
  → track pending tool_use IDs (from assistant entries)
  → when a tool_result arrives, remove the corresponding tool_use ID
  → if a tool_use ID stays pending for >1 second, it's likely a permission prompt
  → show in Agent Chat UI: "Claude wants to run {tool_name}: {description} — Allow / Deny?"
  → on user response, write keystroke to PTY stdin via swe-swe-server
```

### Writing Approval to PTY

When user approves/denies in Agent Chat:
1. Agent Chat POSTs to swe-swe-server: `POST /api/pty-input` with `{ "sessionID": "...", "input": "y\n" }` or `"n\n"`
2. swe-swe-server writes to the session's PTY stdin
3. Claude Code receives the keystroke and proceeds

## Notes

- In yolo mode (`--dangerously-skip-permissions`), there is no permission gap — tool_result follows tool_use immediately. The detection algorithm naturally produces no prompts.
- Multiple `tool_use` blocks can appear in a single assistant entry (parallel tool calls). Each gets its own `tool_result`.
- The `progress` entries (e.g. `mcp_progress` with `status: "started"`) appear between tool_use and tool_result and can be used to distinguish "tool is running" from "waiting for permission".
