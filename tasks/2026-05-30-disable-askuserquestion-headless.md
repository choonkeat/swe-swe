# Hard-disable AskUserQuestion for agent-chat-attached Claude sessions

**Date**: 2026-05-30
**Status**: 🟡 READY TO IMPLEMENT — design complete, grounded in swe-swe source. Awaiting go-ahead.
**Owner repo**: swe-swe (this repo) — the launcher that assembles the `claude` argv.
**Supersedes**: the 2026-05-24 draft that lived (wrongly) in the agent-chat repo. That draft
was blocked on "need swe-swe source"; this rewrite resolves every open question because it
was written against the actual source.

## Problem

Claude Code's built-in **AskUserQuestion** tool renders a multiple-choice picker in the
**TUI only**. When the user is driving via agent-chat's web UI, that picker renders into an
invisible terminal — the question is silently lost, and the user is forced to the TUI to
answer (see the real incident write-up, summarized below).

Unlike permission prompts, Claude Code exposes **no channel** to intercept or relay the MCQ.
Confirmed against the current channels reference (research preview, v2.1.80+,
<https://code.claude.com/docs/en/channels-reference>): the only Claude-Code→channel event a
channel can receive is `notifications/claude/channel/permission_request`, and the docs state
relay "covers tool-use approvals like Bash, Write, and Edit" — **not** AskUserQuestion. So
there is nothing to subscribe to. agent-chat already uses that permission relay
(`claude/channel/permission`, ADR 2026-04-06 in the agent-chat repo); no analogous hook
exists for the MCQ.

### Observed failure (the motivating incident)

In a real session the agent, after a long autonomous Bash→Read investigation chain, reached
for AskUserQuestion instead of `send_message`. The JSONL showed a full report→ask→act loop
happen entirely in the invisible TUI channel (plain-text findings, an AskUserQuestion call
with options that mapped 1:1 onto a `send_message` + quick-replies array, then a Write to a
skill file) — none of it visible in agent-chat until the *next* turn, when the agent
self-healed back onto `send_message`. The leak is specifically **the first user-facing action
after a long tool chain**, when the routing reminder in the system-reminder is stalest and the
native tool's affordance out-pulls it.

## Levers (only three exist)

1. **Soft prompt nudge — already shipped.** agent-chat embeds, in `prompts/agent-reply.tmpl`
   and the per-turn wrapper: *"The TUI is invisible to the user (so don't ever call the
   built-in AskUserQuestion tool)."* (agent-chat commit `31f6d9d`.) It works most of the time
   but is a system-reminder, which has weaker pull than a first-class native tool — hence the
   residual leak above. **Keep this as the portable fallback** regardless of what we do here.
2. **Hard-disable via argv — this task.** A bare `--disallowedTools AskUserQuestion` removes
   the tool from the model's context entirely (not a call-time denial). Per the docs, deny /
   `disallowedTools` are **not** bypassed by `--dangerously-skip-permissions` (which our
   sessions use), so the lever still bites.
3. **Detect + inject (rejected).** We *could* tail the session JSONL (the AskUserQuestion call
   *is* written there) and inject the answer via PTY keystroke — the pre-channel mechanism the
   old permission-detection used. Rejected: more code, more fragile, and every MCQ we've seen
   is a perfectly-shaped quick-reply question that `send_message` already handles natively.
   Supporting the native tool would be rebuilding a worse version of what we already have.

**Decision: lever 2 (suppress), with lever 1 kept as fallback.**

## The implementation point (verified in source)

`cmd/swe-swe/templates/host/swe-swe-server/main.go:5594`:

```go
// Pure function -- no PTY, no env -- intended to be unit tested.
func buildAgentArgv(shellCmd, extraArgs string) (string, []string) {
	cmdName, cmdArgs := parseCommand(shellCmd)
	if extra := strings.Fields(extraArgs); len(extra) > 0 {
		cmdArgs = append(cmdArgs, extra...)
	}
	return cmdName, cmdArgs
}
```

This is the single, pure, already-unit-tested chokepoint where every assistant's argv is
assembled. The agent-chat dev-channel is **not hardcoded** — it arrives through `extraArgs`
(user/config-supplied) as:

```
--dangerously-load-development-channels server:swe-swe-agent-chat
```

(The channel registers under the name `swe-swe-agent-chat`; cf. the `claude mcp add ... swe-swe-agent-chat`
in `cmd/swe-swe/templates.go:876`.)

So the discriminator the 2026-05-24 draft was hunting for is already in hand inside this
function. The conditional is: **append `--disallowedTools AskUserQuestion` iff (a) the agent
is claude AND (b) the agent-chat channel is attached.**

### Why BOTH conditions are mandatory

`buildAgentArgv` serves **all** assistants — `main.go` defines ShellCmd entries for claude,
gemini, codex, goose, aider, opencode, and pi (lines ~356–417). `--disallowedTools` is a
**claude-only** flag; passing it to aider/goose/codex/etc. would corrupt their argv and break
the session. Gating on `cmdName == "claude"` is therefore not optional. (The 2026-05-24 draft
missed this — it assumed a claude-only code path.)

`cmdName` comes from `parseCommand(shellCmd)`, so it is `"claude"` for `"claude"`,
`"claude --continue"`, and `"claude --dangerously-skip-permissions"` alike — a reliable
discriminator.

## Proposed patch

```go
func buildAgentArgv(shellCmd, extraArgs string) (string, []string) {
	cmdName, cmdArgs := parseCommand(shellCmd)
	if extra := strings.Fields(extraArgs); len(extra) > 0 {
		cmdArgs = append(cmdArgs, extra...)
	}
	// When agent-chat is the front-end (its dev-channel is attached) the TUI is
	// invisible, so Claude Code's AskUserQuestion picker renders into a terminal
	// nobody can see and the question is silently lost. Remove the tool from the
	// model's context so it routes decisions through send_message + quick-replies
	// instead. Claude-only: --disallowedTools is a claude flag and would corrupt
	// other assistants' argv. (Belt-and-suspenders with agent-chat's prompt nudge.)
	if cmdName == "claude" && strings.Contains(extraArgs, "swe-swe-agent-chat") {
		cmdArgs = append(cmdArgs, "--disallowedTools", "AskUserQuestion")
	}
	return cmdName, cmdArgs
}
```

Notes:
- Match on `"swe-swe-agent-chat"` (the channel name), not the whole flag string — robust to
  `--channels` vs `--dangerously-load-development-channels` and to arg ordering.
- Append **after** `extraArgs` so a power user could still re-enable via their own flags if we
  ever want an escape hatch (not added now; see open question 1).

## Tests to add (`session_argv_test.go`)

The existing `TestBuildAgentArgv` table is the natural home. Add cases:

| name | shellCmd | extraArgs | expect `--disallowedTools AskUserQuestion`? |
|------|----------|-----------|---------------------------------------------|
| claude + agent-chat channel | `claude --dangerously-skip-permissions` | `--dangerously-load-development-channels server:swe-swe-agent-chat` | **yes**, appended last |
| claude, no channel | `claude` | `` | no |
| claude, unrelated channel | `claude` | `--channels server:telegram` | no |
| aider + agent-chat channel | `aider` | `--dangerously-load-development-channels server:swe-swe-agent-chat` | **no** (not claude) |
| goose + agent-chat channel | `goose session` | `... server:swe-swe-agent-chat` | **no** (not claude) |

Also extend `TestAgentArgvThroughWrapWithScript` (or add a sibling) to assert the flag
survives `wrapWithScript` into the final `bash -c` string for the claude+channel case — that
test exists precisely because the slice gets re-flattened and the unit test alone is
"misleading."

Run with `make test` (repo rule: never `go test`/`go vet` directly).

## Residual trade-off (needs sign-off)

The block is **per-session, not per-surface**. A user who launches a session *with* the
agent-chat channel attached but *also* sits at that session's TUI loses AskUserQuestion there
too. Judged acceptable — they opted the session into chat — but call it out before merge.
This is the same caveat the interim per-workspace deny carried.

## Open questions

1. **Escape hatch?** Should the disable be opt-out (env var / per-instance setting) for power
   users who want AskUserQuestion even in chat (accepting they'll miss the questions)? Default:
   no — add only if asked.
2. **Empirical confirmation** that `--disallowedTools AskUserQuestion` survives
   `--dangerously-skip-permissions` in the real interactive + dev-channels launch. Docs say
   yes; confirm with one live session before relying on it. (A `-p`/`--print` probe can't
   isolate it — `-p` may drop AskUserQuestion independently.)

## Clean-up done as part of this move

- Interim stopgaps applied 2026-05-24 in the agent-chat dev box (`~/.claude/settings.json` and
  `.claude/settings.local.json` → `permissions.deny: ["AskUserQuestion"]`) carry the same
  per-session caveat and are **not** the real fix. Once this patch ships, revisit whether to
  drop them (they also disable MCQ for any TUI use of that workspace).

## References

- agent-chat commit `31f6d9d` — the soft prompt nudge (fallback layer).
- agent-chat ADR `2026-04-06-channel-permission-relay.md` — the relay that exists for
  permissions (and the absence of one for AskUserQuestion).
- Channels reference: <https://code.claude.com/docs/en/channels-reference> — confirms no MCQ
  relay channel exists.
- `cmd/swe-swe/templates/host/swe-swe-server/main.go:5594` — `buildAgentArgv` (the patch site).
- `cmd/swe-swe/templates/host/swe-swe-server/session_argv_test.go` — existing tests.
- agent-chat memory note: `askuserquestion-not-interceptable`.
