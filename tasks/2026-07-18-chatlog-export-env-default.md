# Chat-log auto-export: swe-swe wiring (env-var default + opt-out)

## Goal

When agent-chat ships streaming chat-log auto-export (see
`/repos/agent-chat/workspace/tasks/2026-07-18-streaming-chatlog-auto-export.md`),
swe-swe's entire integration is: **default `AGENT_CHAT_EXPORT_DIR` to
`{workDir}/agent-chats` for chat sessions, respecting user overrides as the
opt-out.** Every chat session then archives its conversation (markdown +
assets + index.html) into the repo it is working on — worktree sessions
included, since workDir *is* the worktree — with no agent action required.

Never auto-commit (confirmed with user 2026-07-18): the export sits in the
working tree; committing stays with the agent/user.

## Background (current behavior — read before changing)

- `materializeSession` (templates/host/swe-swe-server/main.go:5260-5275): for
  `SessionMode == "chat"`, appends `AGENT_CHAT_EVENT_LOG={recordingsDir}/....events.jsonl`
  to the session env. This is the exact pattern to extend.
- Env layering in `buildSessionEnv` (main.go:737-811): base env → per-session
  Settings-textarea vars (`sessionEnvVars`, main.go:793-799) → `.swe-swe/env`
  file **last, wins** (main.go:807-809). The `AGENT_CHAT_EVENT_LOG` append at
  :5273 happens *after* all of that, so a plain append there would override
  user values — the default must be presence-checked instead (see decisions).
- `reservedEnvKeys` (env_store.go:25): `AGENT_CHAT_PORT`/`AGENT_CHAT_DISABLE`
  are reserved; `AGENT_CHAT_EXPORT_DIR` is **not** — so both the Settings
  textarea and `.swe-swe/env` can already set/override it. Do NOT add it to
  the reserved list; that override IS the opt-out mechanism.
- agent-chat is spawned by the mcp-less fleet (`mcp_less.go:65`) via
  `swe-npx -y @choonkeat/agent-chat ...` with `cmd.Env = session env` and
  `cmd.Dir = workDir`. So the env var reaches agent-chat with cwd = workDir;
  a *relative* export dir would also work, but pass it absolute for clarity.
- Terminal sessions set `AGENT_CHAT_DISABLE=1` and never launch agent-chat —
  nothing to do for them.
- Dockerless mode runs the same swe-swe-server template, so the change covers
  it automatically (verify in Step 3).

## Design decisions (confirmed with user 2026-07-18)

- **Default ON** for all chat sessions: `AGENT_CHAT_EXPORT_DIR={workDir}/agent-chats`.
- **Presence-checked append**: only append the default when the composed env
  does not already contain an `AGENT_CHAT_EXPORT_DIR=` entry — *presence*,
  not non-empty value. A user-set empty value (`AGENT_CHAT_EXPORT_DIR=`) is
  an explicit opt-out (agent-chat treats empty as disabled); a user-set path
  is a relocation. Needs an `envHas(env, key)` helper — `envLookup` returns
  `""` for both missing and empty, which is not enough.
- **Opt-out surfaces (no new server plumbing):**
  - per-workspace: `AGENT_CHAT_EXPORT_DIR=` line in `.swe-swe/env`
    (checked in = team-wide policy);
  - per-session at spawn: Settings panel env textarea / new-session EnvRaw;
  - mid-session: agent-chat's `chatlog_optout` tool (conversational — that
    side owns it, nothing here);
  - relocation: set the var to a different path (same cwd-escape rules
    enforced by agent-chat).
- **Toggle UI = new-session dialog checkbox** ("Archive chat log into repo"),
  default checked; unchecking stages `AGENT_CHAT_EXPORT_DIR=` into the
  dialog's existing EnvRaw blob. Spawn-time is the only honest place for a
  toggle — env is materialized at spawn, so a Session Settings switch could
  not affect the running agent-chat (the textarea's documented
  "next session/PTY restart" semantics already cover late changes). No new
  state store, no new API: the checkbox is sugar over EnvRaw.
- **Agent guidance, not automation, for commits**: seed one line into the
  AGENTS.md template — when committing, include `agent-chats/` changes
  (in the same commit or a trailing `docs(agent-chats):` commit). No hooks,
  no server-side git.
- **Rollout is order-independent**: current agent-chat ignores the unknown
  env var, so this can ship before or after the agent-chat feature. The
  fleet resolves `@choonkeat/agent-chat` latest via swe-npx at spawn, so the
  feature activates as soon as the new agent-chat version is published — no
  swe-swe rebuild needed at that point.

## Non-goals

- No mid-session toggle (impossible without env-var hot-reload; the
  conversational opt-out covers it).
- No init flag: this is server runtime behavior, not an `init` template
  option — the CLAUDE.md two-commit flag convention does not apply.
- No changes to recordings/JSONL/homepage Chat listing (explicitly untouched
  by the agent-chat design).
- No backfill of historical sessions (separate follow-up, agent-chat side).

## Steps

Per project CLAUDE.md: `make test` (never `go test` directly); template edits
need `make build golden-update` and a staged-golden-diff review.

### Step 1 — Presence-checked default append
- **Test (red):** in the server template tests, cover the new helper (e.g.
  `defaultChatExportEnv(env []string, workDir string) []string` or an
  `envHas` + call-site pair):
  - chat-session env without the key → gains `AGENT_CHAT_EXPORT_DIR={workDir}/agent-chats`;
  - env already containing `AGENT_CHAT_EXPORT_DIR=/custom` → unchanged;
  - env containing the *empty* `AGENT_CHAT_EXPORT_DIR=` → unchanged (opt-out
    preserved);
  - terminal sessions never gain the key (call site is inside the
    `SessionMode == "chat"` block next to `AGENT_CHAT_EVENT_LOG`).
- **Impl (green):** add `envHas` + the append at main.go:5273's block.
- `make build golden-update`; verify the staged golden diff shows only the
  server-template change.

### Step 2 — New-session dialog checkbox
- Checkbox in `static/new-session-dialog.js` (chat mode only), default
  checked, labeled "Archive chat log into repo (agent-chats/)"; unchecked →
  append `AGENT_CHAT_EXPORT_DIR=` to the staged EnvRaw blob before create.
  Keep it dumb: no round-trip, no new params on SessionParams.
- Test at whatever level the dialog currently has coverage; otherwise assert
  the EnvRaw staging in a unit test of the blob builder if one exists.
- `make build golden-update` again (template asset changed).

### Step 3 — Docs + guidance + verification
- AGENTS.md template: one line of commit guidance (include `agent-chats/`).
- Docs: note the env var + opt-out surfaces where `.swe-swe/env` is
  documented; CHANGELOG entry.
- Verify dockerless path inherits the behavior (same template server) — code
  inspection + note; live dockerless smoke optional.
- Live e2e (after the agent-chat feature is published): boot the test
  container (docs/dev/test-container-workflow.md), start a chat session in a
  repo, exchange a couple of turns with an image, verify
  `{workDir}/agent-chats/*.md` grows per-event and index.html lists it; then
  create a session with the checkbox off and verify nothing is written.
  Tear the container down.

## Follow-ups

- Integration test for fork/resume: forked session (same workDir,
  prepopulated JSONL) should continue its file, not mint a duplicate —
  driven by agent-chat's `session:` header identity; revisit once that
  ships to pin down the identity key across forks.
- Consider surfacing the effective export state (on/off/path) read-only in
  Session Settings once real usage shows whether people look for it there.
