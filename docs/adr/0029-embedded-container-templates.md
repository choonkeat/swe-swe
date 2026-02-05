# ADR-029: Embedded container templates

**Status**: Accepted
**Date**: 2026-01-30

## Context

External repo clones and new projects lack swe-swe files (`.mcp.json`, `.swe-swe/docs/*`, `swe-swe/setup`). Previously, only `/workspace` received these files via `swe-swe init` on the host.

This meant:
- Agents in external repos had no MCP configuration
- Documentation files were missing
- Setup commands weren't available

Users had to manually copy files or run setup commands, breaking the seamless experience.

## Decision

Implement a two-tier system for swe-swe file distribution:

### Tier 1: Embed templates in server binary

1. During `swe-swe init`, copy container template files into `{metadataDir}/swe-swe-server/container-templates/`
2. Server binary embeds these via `//go:embed container-templates/*`
3. Add `setupSweSweFiles(destDir, agents)` function that writes templates to destination
4. Call `setupSweSweFiles` in session prepare handlers:
   - `handleRepoPrepareClone`: after successful clone/fetch
   - `handleRepoPrepareCreate`: after git init + empty commit

### Tier 2: Symlink from base repo to worktrees

Replace `copyUntrackedFiles` and `copySweSweDocsDir` with unified `ensureSweSweFiles(srcDir, destDir)`:

1. For each swe-swe file/directory in source (dotfiles, `CLAUDE.md`, `AGENTS.md`, `swe-swe/`)
2. Skip if tracked in git (worktree already has it)
3. Skip if destination exists (idempotent)
4. Create absolute symlink from worktree to base repo

### File List

- `.mcp.json` — MCP server configuration
- `.swe-swe/docs/AGENTS.md` — agent documentation
- `.swe-swe/docs/browser-automation.md`
- `.swe-swe/docs/app-preview.md`
- `.swe-swe/docs/docker.md`
- `swe-swe/setup` — setup command (conditional on agent type)

### New Project Handling

New projects also get:
- Empty initial commit (with git user fallback if not configured)
- No worktree creation (work directly in `/repos/{name}/workspace`)

## Consequences

**Good:**
- All session types get consistent swe-swe files
- Agents have MCP configuration immediately
- Symlinks keep worktrees lightweight
- Idempotent — safe to call multiple times

**Bad:**
- Server binary is slightly larger (embedded templates)
- Symlinks require base repo to exist (not an issue in practice)
- Template changes require rebuild + reinit
