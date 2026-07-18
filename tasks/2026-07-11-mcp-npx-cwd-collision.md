# Fix /mcp launch failure when project cwd is the agent-chat (or agent-reverse-proxy) repo

> STATUS (2026-07-18): SUPERSEDED, do not execute. The swe-npx task
> (tasks/2026-07-18-swe-npx-node-free-helpers.md, merged as f8921260b) replaced
> every `npx -y @choonkeat/<pkg>` launch with `swe-npx -y @choonkeat/<pkg>`, a
> registry-resolving Go helper that cannot hit the npx project-cwd name
> collision this plan fixes -- the `@latest` pin below is moot for those call
> sites. The one remaining npx launch, `@playwright/mcp`, already carries the
> `@latest` pin. Kept for the root-cause analysis (npx name-collision 127).

Executable task plan for `/swe-swe:execute-step-by-step` (via
`/swe-swe:execute-in-worktree tasks/2026-07-11-mcp-npx-cwd-collision.md`).
Log convention: `tasks/2026-07-11-mcp-npx-cwd-collision.md-phase{N}.log`.

Origin: agent-chat troubleshooting session 2026-07-11 ("MCP failed to load").
Root cause was reproduced and the fix verified against the live npm registry
(0.8.9). Decisions are settled below; read the whole Design before Phase 1.

## Ground rules for the executing agent

- ASCII only in all code/markdown (no em-dashes, no smart quotes, no non-ASCII).
- Run tests with `make test`, never bare `go test` / `go vet`.
- The MCP launch command is emitted in TWO source spots plus embedded copies in
  golden fixtures. After ANY change under `cmd/swe-swe/templates.go` or files
  that feed golden output: `make build golden-update`, then
  `git add cmd/swe-swe/testdata/golden` and review
  `git diff --cached -- cmd/swe-swe/testdata/golden` before committing. The
  golden fixtures embed the generated launch strings, so they WILL change --
  that is expected; verify the diff is ONLY the `@latest` additions.
- Stage explicit paths by name. NEVER `git add -A`.
- If any verification fails and a workaround is tempting: STOP and ask via
  send_message. No silent compromises.

## Design (settled -- do not relitigate)

### Symptom
A Claude Code session's `swe-swe-agent-chat` MCP server fails to load in `/mcp`,
so the chat tools (`send_message`, `check_messages`, ...) are absent and the
agent can only talk into the invisible TUI. The session's `AGENT_CHAT_PORT` has
nothing listening on it while other sessions are healthy.

### Root cause (reproduced)
swe-swe launches the server as a stdio MCP:
`sh -c "exec npx -y @choonkeat/agent-chat ..."`. Claude Code spawns MCP servers
with cwd = the project directory. When that project dir is a checkout of the
agent-chat repo itself, its own `package.json` name (`@choonkeat/agent-chat`)
collides with the npx target. npx treats it as the current project, looks for
`node_modules/.bin/agent-chat` (which does not exist -- the repo has no
self-install; only `node_modules/@choonkeat/agent-chat-<platform>` may be
present, with no `.bin` shim), and falls back to running the bare command
`agent-chat` via the shell:

    $ cd /repos/agent-chat/workspace
    $ sh -c 'exec npx -y @choonkeat/agent-chat -v'
    sh: 1: agent-chat: not found      # exit 127 -- server never starts

From a neutral cwd the exact same command works. Other sessions work only
because their cwd is not this repo root.

The SAME latent bug exists for `@choonkeat/agent-reverse-proxy` (the
`swe-swe-preview` and `swe-swe` MCP servers): a session whose project dir is the
agent-reverse-proxy repo would hit the identical name-collision 127.

### Settled fix: pin the package spec to `@latest`
Changing `npx -y @choonkeat/agent-chat ...` to
`npx -y @choonkeat/agent-chat@latest ...` makes npx resolve from the registry
instead of the same-named local package, so the collision cannot occur
regardless of cwd. Verified from inside the agent-chat repo:

    $ cd /repos/agent-chat/workspace
    $ sh -c 'exec npx -y @choonkeat/agent-chat@latest -v'
    agent-chat 0.8.9 (1e4428c)        # exit 0

Why this and not "cd to a neutral dir first": a `cd` would move cwd away from the
project dir, and agent-chat's `--filepath-roots` DEFAULTS to "cwd +
/repos,/workspace,/worktrees" -- so cd-ing would silently drop the project dir
from `@` filepath autocomplete unless we also captured and re-passed the original
`$PWD`. The `@latest` fix touches nothing but package resolution: cwd is
unchanged, filepath roots stay correct, and normal (non-repo) sessions behave
exactly as before (bare `-y <name>` already resolves latest from the registry).

Apply the same `@latest` pin to `@choonkeat/agent-reverse-proxy` in the same two
files, for the preview and swe-swe proxy servers.

### Known tradeoff (accept, do not "fix")
With `@latest`, a developer running a session INSIDE the agent-chat repo no
longer gets their unpublished local build via the MCP server -- they always get
the published version. This is acceptable: that local-build path was already
broken (exit 127), so this is not a regression from any working state. Local
dev-build testing has a separate mechanism (a `node_modules/.bin/agent-chat`
symlink to `bin/agent-chat.js`, added ad hoc); it is out of scope here.

### Anchors (grep the string; line numbers approximate)
- `cmd/swe-swe/dockerless.go` ~L332: dockerless `.mcp.json` generation. The
  `sh(...)` helper is defined ~L330:
  `sh := func(script string) mcpServerSpec { ... "sh", ["-c", script] }`.
  Entries to edit: `swe-swe-agent-chat` (~L332, agent-chat),
  `swe-swe-preview` (~L334, agent-reverse-proxy),
  `swe-swe` (~L336, agent-reverse-proxy).
- `cmd/swe-swe/templates.go` ~L823-827: the `claude mcp add ... -- sh -c 'exec
  npx -y @choonkeat/... '` lines for the same three servers.
- Golden copies that will regenerate: e.g.
  `cmd/swe-swe/testdata/golden/**/swe-swe-server/mcp_less.go` (`Argv: shExec("npx
  -y @choonkeat/agent-chat ...")`) and any `.mcp.json` fixtures.

## Phase 1 -- Edit the two source spots

1. In `cmd/swe-swe/dockerless.go`, change the three `npx -y @choonkeat/<pkg>`
   occurrences to `npx -y @choonkeat/<pkg>@latest`:
   - `@choonkeat/agent-chat` -> `@choonkeat/agent-chat@latest`
   - `@choonkeat/agent-reverse-proxy` (both `swe-swe-preview` and `swe-swe`) ->
     `@choonkeat/agent-reverse-proxy@latest`
   Change ONLY the package spec; leave every flag, env var, and quoting exactly
   as-is.
2. In `cmd/swe-swe/templates.go`, make the identical three edits in the
   `claude mcp add` lines.
3. Grep to confirm no un-pinned `npx -y @choonkeat/` launch strings remain in
   these two files:
   `grep -n "npx -y @choonkeat/" cmd/swe-swe/dockerless.go cmd/swe-swe/templates.go`
   -- every hit should now carry `@latest`.

Log: `...-phase1.log`.

## Phase 2 -- Regenerate golden + build

1. `make build golden-update`.
2. `git add cmd/swe-swe/testdata/golden` and review
   `git diff --cached -- cmd/swe-swe/testdata/golden`: the ONLY changes must be
   `@latest` appended to the three package specs across the embedded copies.
   If anything else moved, STOP and ask via send_message.

Log: `...-phase2.log`.

## Phase 3 -- Test + verify behavior

1. `make test` (unit + whatever e2e the swe-swe repo runs). Must be green.
2. Behavioral proof of the fix (run from a dir that shadows the package name):
   - Repro of the OLD bug (should be 127 without the pin):
     `cd /repos/agent-chat/workspace && sh -c 'exec npx -y @choonkeat/agent-chat -v'`
   - Proof the NEW spec works from the same dir (should print a version, exit 0):
     `cd /repos/agent-chat/workspace && sh -c 'exec npx -y @choonkeat/agent-chat@latest -v'`
   Capture both in the log.
3. Sanity: a normal (non-repo) project dir still launches fine -- pick any other
   dir and run the `@latest` command; exit 0.

Log: `...-phase3.log`.

## Phase 4 -- Commit

1. Stage explicit paths only:
   `git add cmd/swe-swe/dockerless.go cmd/swe-swe/templates.go cmd/swe-swe/testdata/golden`
2. Commit:
   `fix(mcp): pin npx package specs to @latest so /mcp survives in-repo cwd`
   Body: one paragraph summarizing the name-collision root cause and the pin.
3. Do NOT push unless the user asks.

Log: `...-phase4.log`.

## Done criteria
- `/mcp` launch cannot 127 due to project-cwd name collision for agent-chat or
  agent-reverse-proxy.
- `make test` green; golden diff is `@latest`-only.
- Behavior proof (127 before, 0 after) captured in phase3 log.
