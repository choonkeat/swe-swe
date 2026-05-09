<!-- agent-chat export
title: Codex Mcp Env Fix
date: 2026-05-09
index: 01
slug: codex-mcp-env-fix
agent: claude
version: 0.6.0 (0c45ca3)
-->

# Codex Mcp Env Fix

_2026-05-09 · 01 · claude · agent-chat 0.6.0 (0c45ca3)_

**USER**

> codex could not use the mcp servers we set. codex fixed it. please see and compare with our @cmd/swe-swe/templates/host/entrypoint.sh 
>
> discuss
>
> [entrypoint.upstream.sh](./assets/2026-05-09-01-1.sh)
> [entrypoint.sh](./assets/2026-05-09-01-2.sh)

<small>took 1m 16s</small><br>
**AGENT**

> Compared the three. Our `cmd/swe-swe/templates/host/entrypoint.sh` is byte-identical to `d50501e0-entrypoint.upstream.sh` (the broken version). The fix in `6e7e42a5-entrypoint.sh` only touches the **`# {{IF CODEX}}`** block (lines 67–93). All other sections — OpenCode/Gemini/Goose/Claude — are unchanged.
>
> ### Root cause
>
> Codex sandboxes MCP server processes and **does not** forward env vars by default. Our config wraps everything in `sh -c "exec npx … $SWE_SERVER_PORT …"`. Codex launches `sh` with a stripped env, so `$SWE_SERVER_PORT`, `$SESSION_UUID`, `$MCP_AUTH_KEY`, `$BROWSER_CDP_PORT` all expand to **empty strings** → the MCP child ends up dialing `http://localhost:/api/...` and silently fails.
>
> OpenCode/Gemini/Goose don't sandbox env this way, which is why they kept working with the same `sh -c` pattern.
>
> ### Codex's fix
>
> Two changes per server entry:
>
> 1. **Drop the `sh -c "exec …"` wrapper.** Use `command = "npx"` (or `mcp-lazy-init`) with args as a real array. Codex does `${VAR}` substitution on args itself.
> 2. **Declare `env_vars = [...]`** — the whitelist of env names Codex must propagate into the MCP child.
>
> Example (agent-chat):
> ```toml
> # before
> command = "sh"
> args = ["-c", "exec npx -y @choonkeat/agent-chat … $SWE_SERVER_PORT …"]
>
> # after
> command = "npx"
> args = ["-y", "@choonkeat/agent-chat", "--theme-cookie", "swe-swe-theme",
>         "--autocomplete-triggers", "/=slash-command",
>         "--autocomplete-url",
>         "http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY"]
> env_vars = ["AGENT_CHAT_PORT", "SWE_SERVER_PORT", "SESSION_UUID", "MCP_AUTH_KEY"]
> ```
>
> Per-server `env_vars` lists in the fix:
> - `swe-swe-agent-chat` → `[AGENT_CHAT_PORT, SWE_SERVER_PORT, SESSION_UUID, MCP_AUTH_KEY]`
> - `swe-swe-playwright` → `[SWE_SERVER_PORT, SESSION_UUID, MCP_AUTH_KEY, BROWSER_CDP_PORT]`
> - `swe-swe-preview` → `[SWE_SERVER_PORT, SESSION_UUID]`
> - `swe-swe-whiteboard` → none (no vars referenced)
> - `swe-swe` → `[SWE_SERVER_PORT, MCP_AUTH_KEY]`
>
> ### Notes / nits before we adopt verbatim
>
> 1. **`AGENT_CHAT_PORT` for agent-chat** — not referenced in the URL, but listed. Probably defensive in case `@choonkeat/agent-chat` reads it from env directly. Worth keeping.
> 2. **The MCP-Auth note in `cmd/swe-swe/host_setup.go`** (if any) and TDSpec docs may still describe the `sh -c` form — those will need a follow-up update.
> 3. **Asymmetry** — only Codex changes; OpenCode/Gemini/Goose stay on the `sh -c` pattern. That's fine, but we should leave a comment in the Codex block explaining *why* it's different (sandboxed env), so the next person doesn't "normalize" it back.
> 4. **Golden tests** — `make build golden-update` will regen `cmd/swe-swe/testdata/golden/`. Should be a small TOML-only diff.
> 5. **One stylistic call**: codex used `"http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY"` (double-quoted, no extra concatenation tricks) for the swe-swe entry. Ours had `'http://...$SWE_SERVER_PORT/mcp?key='$MCP_AUTH_KEY` because we needed sh quoting. Without the sh wrapper, the simple form is cleaner — adopt as-is.
>
> Want me to apply the patch (Codex-block-only change + a short `# Codex sandboxes env vars …` comment), then `make build golden-update`?

[Quick replies]
- Yes, apply patch and regen goldens
- Apply patch only, I'll regen
- Wait — let me check something first

**USER**

> Yes, apply patch and regen goldens

<small>took 6.1s</small><br>
**AGENT**

> Applying Codex-block patch to cmd/swe-swe/templates/host/entrypoint.sh…

<small>took 2m 32s</small><br>
**AGENT**

> Done. Patch applied + goldens regenerated + tests pass.
>
> ### What changed
> - **`cmd/swe-swe/templates/host/entrypoint.sh`** — `# {{IF CODEX}}` block only:
>   - Replaced `sh -c "exec npx ..."` wrappers with direct `command = "npx"` (or `mcp-lazy-init`) and split args into a real array.
>   - Added `env_vars = [...]` whitelists per server so Codex propagates env into the sandboxed MCP child *and* substitutes `$VAR` references in args.
>   - Added a 5-line comment above the `mkdir -p /home/app/.codex` explaining *why* this block is shaped differently from the others (Codex sandboxes env). Future readers won't try to "normalize" it back.
> - **`cmd/swe-swe/testdata/golden/**/entrypoint.sh`** — 26 golden variants regenerated, identical Codex-block diff in each. Roughly `+25 / -16` per file.
>
> ### Verification
> - `make build golden-update` — clean.
> - `make test` — all packages pass (`cmd/swe-swe`, `cmd/mcp-lazy-init`, `swe-swe`, `git-sign-swe-swe`).
> - Spot-checked diff on `init-ask` golden — only Codex section touched, no stray edits to OpenCode/Gemini/Goose/Claude blocks.
> - Grepped for stale `command = "sh"` references in docs/markdown/Go — none.
>
> ### Per-server env_vars (final)
> | Server | env_vars |
> |---|---|
> | `swe-swe-agent-chat` | `AGENT_CHAT_PORT, SWE_SERVER_PORT, SESSION_UUID, MCP_AUTH_KEY` |
> | `swe-swe-playwright` | `SWE_SERVER_PORT, SESSION_UUID, MCP_AUTH_KEY, BROWSER_CDP_PORT` |
> | `swe-swe-preview` | `SWE_SERVER_PORT, SESSION_UUID` |
> | `swe-swe-whiteboard` | (none) |
> | `swe-swe` | `SWE_SERVER_PORT, MCP_AUTH_KEY` |
>
> ### Files staged for review
> ```
> M cmd/swe-swe/templates/host/entrypoint.sh
> M cmd/swe-swe/testdata/golden/**/entrypoint.sh   (26 files)
> ```
>
> Want me to commit this as a single `fix(codex)` commit, or do you want to inspect first?

[Quick replies]
- Commit it as fix(codex)
- Show me the entrypoint.sh diff first
- I'll commit it myself

**USER**

> Commit it as fix(codex)

**USER**

> export chat logs and git commit amend

<small>took 8.2s</small><br>
**AGENT**

> Committed as `8603db6aa` on `main`.
>
> ```
> fix(codex): use direct command + env_vars whitelist for MCP servers
> 27 files changed, 459 insertions(+), 216 deletions(-)
> ```
>
> Scope was limited to:
> - `cmd/swe-swe/templates/host/entrypoint.sh` (Codex block + 5-line comment)
> - 26 regenerated `cmd/swe-swe/testdata/golden/**/entrypoint.sh`
>
> Pre-existing unrelated changes left untouched in working tree:
> - `D .claude/commands/run-md-serve.md`
> - `M e2e/tests/credentials.spec.js`
> - 11 screenshot/report PNGs (untracked)
>
> Nothing pushed.

