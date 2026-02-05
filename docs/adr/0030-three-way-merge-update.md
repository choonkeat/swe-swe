# ADR-030: Three-way merge for swe-swe file updates

**Status**: Accepted
**Date**: 2026-02-02

## Context

After upgrading swe-swe, workspace files (`.mcp.json`, `.swe-swe/docs/*`) may be stale. However, users may have customized these files:
- Added custom MCP servers to `.mcp.json`
- Written notes in `AGENTS.md` "Current Setup" section
- Modified documentation for their workflow

A naive replacement would destroy user customizations. Asking users to manually merge is error-prone.

## Decision

The `/swe-swe:update-swe-swe` slash command uses three-way merge with baseline tracking:

### Three Versions

- **Baseline**: `.swe-swe/baseline/<path>` — snapshot from original init
- **Current**: `<path>` — what's on disk now (possibly user-modified)
- **New**: Latest template from binary (via `swe-swe-server --dump-container-templates`)

### Merge Rules

| Baseline | Current | New | Action |
|----------|---------|-----|--------|
| missing | exists | exists | Show diff, ask before replacing |
| = current | any | any | Auto-replace (user hasn't modified) |
| = new | any | any | Skip (template unchanged) |
| ≠ current ≠ new | — | — | Three-way merge with agent judgment |

### Special Cases

**`.mcp.json`**: JSON object merge
- Keep user-added server entries
- Update/replace entries from swe-swe (compare with baseline)
- Add new entries from template

**`AGENTS.md`**: Section-aware merge
- Replace "Commands table" and "Documentation list" from template
- Preserve "Current Setup" section entirely (agent-written)

### Workflow

1. Extract latest templates: `swe-swe-server --dump-container-templates /workspace/.swe-swe/updated`
2. For each file, apply merge rules
3. Update baselines: copy new template to `.swe-swe/baseline/<path>`
4. Clean up: `rm -rf /workspace/.swe-swe/updated`
5. Report: summarize updates, skips, and manual merge decisions

## Consequences

**Good:**
- User customizations preserved
- Template updates applied automatically when safe
- Baseline tracking enables future merges
- Agent can make intelligent merge decisions

**Bad:**
- Requires baseline files from init (older installs may lack them)
- Three-way merge logic is complex for structured files (JSON, Markdown sections)
- Agent judgment in conflict cases may vary
