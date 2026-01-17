# Task: Slash commands for swe-swe bundled commands

**Date**: 2026-01-13
**Status**: In Progress

## Goal

Replace the universal `@swe-swe/` file-mention approach with agent-native slash commands for agents that support them, falling back to file mentions only for agents that don't.

## Agent Categories

| Agent | Format | Install Location | Invocation |
|-------|--------|------------------|------------|
| Claude | `.md` | `~/.claude/commands/swe-swe/` | `/swe-swe:setup` |
| Codex | `.md` | `~/.codex/prompts/swe-swe/` | `/swe-swe:setup` |
| OpenCode | `.md` | `~/.config/opencode/command/swe-swe/` | `/swe-swe:setup` |
| Gemini | `.toml` | `~/.gemini/commands/swe-swe/` | `/swe-swe:setup` |
| Goose | none | `/workspace/swe-swe/` | `@swe-swe/setup` |
| Aider | none | `/workspace/swe-swe/` | `@swe-swe/setup` |

---

## Phase 1: Add SlashCommandFormat to AssistantConfig

### What will be achieved
The swe-swe-server will know which slash command format each agent supports, and the MOTD will show agent-appropriate invocation syntax.

### Small steps

1. **Define the enum** in `swe-swe-server/main.go`:
   ```go
   type SlashCommandFormat string
   const (
       SlashCmdMD   SlashCommandFormat = "md"
       SlashCmdTOML SlashCommandFormat = "toml"
       SlashCmdNone SlashCommandFormat = ""
   )
   ```

2. **Add field to `AssistantConfig`**:
   ```go
   type AssistantConfig struct {
       // ...existing fields...
       SlashCmdFormat SlashCommandFormat
   }
   ```

3. **Set values for each agent** in `assistantConfigs`:
   - Claude: `SlashCmdMD`
   - Codex: `SlashCmdMD`
   - OpenCode: `SlashCmdMD`
   - Gemini: `SlashCmdTOML`
   - Goose: `SlashCmdNone`
   - Aider: `SlashCmdNone`

4. **Update `generateMOTD` signature** to accept `AssistantConfig`:
   ```go
   func generateMOTD(workDir, branchName string, cfg AssistantConfig) string
   ```

5. **Update MOTD content** based on `cfg.SlashCmdFormat`:
   - If `SlashCmdMD` or `SlashCmdTOML`: `"type /swe-swe to see commands available"`
   - If `SlashCmdNone`: `"@swe-swe/ to see commands available"`

6. **Update call site** at line ~2302 to pass `sess.AssistantConfig`

### Verification

- **Manual verification**: After Phase 6, start container with different agents, verify MOTD shows correct syntax
- **Regression**: Golden tests will catch unintended changes to main.go

---

## Phase 2: Create bundled slash command files

### What will be achieved
Bundled `swe-swe` slash commands will be installed to agent-specific directories during container startup, enabling `/swe-swe:setup` for agents that support slash commands.

### Small steps

1. **Create `cmd/swe-swe/slash-commands/swe-swe/setup.md`**:
   ```markdown
   ---
   description: Configure git, SSH, testing, credentials
   ---

   # Setup swe-swe Environment

   [Full instructions, adapted from current /workspace/swe-swe/setup]

   Note: If your agent doesn't support slash commands, use `@swe-swe/setup` instead.
   ```

2. **Create `cmd/swe-swe/slash-commands/swe-swe/setup.toml`** (for Gemini):
   ```toml
   description = "Configure git, SSH, testing, credentials"

   prompt = """
   # Setup swe-swe Environment

   [Same instructions as setup.md]
   """
   ```

3. **Update entrypoint.sh template** to install `.toml` files to Gemini's directory:
   - Current logic copies to `~/.claude/commands/`, `~/.codex/prompts/`, `~/.config/opencode/command/`
   - Add: copy `.toml` files to `~/.gemini/commands/swe-swe/`

4. **Update Dockerfile template** (if needed):
   - Ensure bundled slash-commands are extracted to `/tmp/slash-commands/swe-swe/`

5. **Verify embed directive** in `main.go` includes both `.md` and `.toml` files:
   ```go
   //go:embed all:slash-commands
   ```

### Verification

- **Golden tests**: Will show new files in `home/.claude/commands/swe-swe/setup.md`, `home/.gemini/commands/swe-swe/setup.toml`, etc.
- **Manual test**: Start container, verify `/swe-swe:setup` appears in Claude's slash command list
- **Regression**: Existing user-provided slash commands (`--with-slash-commands`) should still work

---

## Phase 3: Conditional `/workspace/swe-swe/` directory

### What will be achieved
The `/workspace/swe-swe/` directory (with file-based commands) will only be created when non-slash-command agents (Goose, Aider) are enabled. Slash-command agents won't see this directory cluttering their workspace.

### Small steps

1. **Add helper functions to `main.go`** (swe-swe CLI, not server):
   ```go
   func (c *InitConfig) HasNonSlashAgents() bool {
       // Returns true if Goose or Aider enabled
   }

   func (c *InitConfig) HasSlashAgents() bool {
       // Returns true if Claude, Codex, OpenCode, or Gemini enabled
   }
   ```

2. **Make `swe-swe/` directory conditional in container templates**:
   - Currently: `cmd/swe-swe/templates/container/swe-swe/setup` always created
   - Change: Wrap in template condition `{{if .HasNonSlashAgents}}`

3. **Update template execution** to pass these helper results to template context

4. **Update worktree creation logic** (if applicable):
   - Worktree copies `swe-swe/` from main workspace
   - If main workspace doesn't have it, worktree shouldn't either

### Verification

- **Golden test: `claude-only`**: Should NOT have `/workspace/swe-swe/` directory
- **Golden test: `aider-only`**: Should have `/workspace/swe-swe/setup`
- **Golden test: `default`** (mixed agents): Should have `/workspace/swe-swe/setup` (because Aider/Goose included)
- **Regression**: Existing tests with mixed agents should still pass

---

## Phase 4: Simplify `.swe-swe/docs/AGENTS.md`

### What will be achieved
The AGENTS.md file will be simplified to just list available commands without specifying invocation syntax. The MOTD (Phase 1) handles teaching the correct syntax per agent.

### Small steps

1. **Update `cmd/swe-swe/templates/container/.swe-swe/docs/AGENTS.md`**:

   Before:
   ```markdown
   Use `@swe-swe/<command>` to invoke:
   | Command | Description | Where |
   ...
   When user mentions `@swe-swe/<command>`, read that file...
   ```

   After:
   ```markdown
   # swe-swe

   ## Commands

   | Command | Description |
   |---------|-------------|
   | `setup` | Configure git, SSH, testing, credentials |

   ## Current Setup

   <!-- Agent: Update this section when setup changes -->
   - Git: (not configured)
   - SSH: (not configured)
   - Testing: (not configured)

   ## Documentation

   - `browser-automation.md` - MCP browser at /chrome/
   - `docker.md` - Docker access from container
   ```

2. **Remove the "For Agents" section** that references `@swe-swe/<command>` syntax

3. **Remove worktree commands from the table** (they're being removed in Phase 5)

### Verification

- **Golden tests**: All `target/.swe-swe/docs/AGENTS.md` files will show simplified content
- **Manual test**: MOTD teaches syntax, AGENTS.md just lists what's available
- **Regression**: No functional change - just documentation simplification

---

## Phase 5: Remove worktree commands

### What will be achieved
The `merge-this-worktree` and `discard-this-worktree` commands will be removed, along with the worktree-specific `swe-swe/` installation code. Worktrees will inherit `swe-swe/` from the main workspace via normal worktree mechanics (or not have it at all for slash-command agents).

### Small steps

1. **Delete template files**:
   - `cmd/swe-swe/templates/worktree/swe-swe/merge-this-worktree.tmpl`
   - `cmd/swe-swe/templates/worktree/swe-swe/discard-this-worktree.tmpl`

2. **Delete the worktree swe-swe directory** (if now empty):
   - `cmd/swe-swe/templates/worktree/swe-swe/` (entire directory)

3. **Remove worktree swe-swe installation code** in swe-swe-server:
   - Find code that copies/generates `swe-swe/` for worktrees
   - Remove it (worktrees don't need special handling)

4. **Update ADR-022** (`docs/adr/0022-simplified-worktree-exit.md`):
   - Change status to "Superseded" or update content
   - Remove references to `merge-this-worktree` and `discard-this-worktree`
   - Document that worktree `swe-swe/` is no longer specially installed

5. **Verify worktree behavior**:
   - For non-slash agents: worktree can access main workspace's `/workspace/swe-swe/setup` if needed
   - For slash agents: `/swe-swe:setup` works anywhere (home directory based)

### Verification

- **Golden tests**: Worktree-related paths should not include `swe-swe/` files
- **Grep verification**: `grep -r "merge-this-worktree\|discard-this-worktree" .` should only find ADR/history
- **Grep verification**: Search for worktree swe-swe installation code and confirm removed
- **Regression**: Worktree creation should still work

---

## Phase 6: Golden tests and verification

### What will be achieved
All changes will be validated through golden tests, ensuring no unintended regressions and confirming the expected diffs.

### Small steps

1. **Run `make build golden-update`**:
   ```bash
   make build golden-update
   ```

2. **Review golden test diffs** by category:

   a. **MOTD changes in `swe-swe-server/main.go`**:
      - All golden variants should show updated `generateMOTD` function
      - Verify `SlashCmdFormat` field added to `AssistantConfig`

   b. **New slash command files**:
      - `home/.claude/commands/swe-swe/setup.md` (for variants with Claude)
      - `home/.codex/prompts/swe-swe/setup.md` (for variants with Codex)
      - `home/.config/opencode/command/swe-swe/setup.md` (for variants with OpenCode)
      - `home/.gemini/commands/swe-swe/setup.toml` (for variants with Gemini)

   c. **Conditional `/workspace/swe-swe/` presence**:
      - `claude-only`: NO `target/swe-swe/` directory
      - `aider-only`: HAS `target/swe-swe/setup`
      - `goose-only`: HAS `target/swe-swe/setup`
      - `default`: HAS `target/swe-swe/setup` (mixed agents)

   d. **Simplified `AGENTS.md`**:
      - All `target/.swe-swe/docs/AGENTS.md` should show simplified content

   e. **Removed worktree commands**:
      - No `merge-this-worktree` or `discard-this-worktree` in any golden output

3. **Run tests**:
   ```bash
   make test
   ```

4. **Manual smoke test** (user will perform):
   - Build and start a test container
   - Verify MOTD shows correct syntax for the agent
   - Verify slash commands appear in agent's command list

### Verification

- **Golden diff review**: `git diff --cached -- cmd/swe-swe/testdata/golden` shows only expected changes
- **Test pass**: `make test` passes
- **No unexpected files**: No stray files created outside expected locations

---

## Notes

- ADR-014 (`docs/adr/0014-slash-commands-cloning.md`) documents the slash command format research
- ADR-022 will be updated to reflect removal of worktree commands
- Gemini uses `.toml` format, others use `.md` format
- Goose and Aider have no slash command support, so they use file mentions
