# ADR-031: Upsert MCP servers in .mcp.json

**Status**: Accepted
**Date**: 2026-02-13

## Context

When `swe-swe init` runs on a brownfield project with an existing `.mcp.json`, it overwrites the file — losing user-defined MCP servers. When `setupSweSweFiles()` runs on a cloned repo that already has `.mcp.json`, it skips entirely — so swe-swe's servers are missing. Both behaviors are wrong.

Additionally, ADR-023 established the `swe-swe-` prefix convention for MCP server names, but the `whiteboard` server was not yet renamed.

## Decision

### Upsert behavior

Both `swe-swe init` and `setupSweSweFiles()` now **upsert** `.mcp.json` instead of overwriting or skipping:

1. Read any existing `.mcp.json` from disk
2. Parse both existing and template as `map[string]any`
3. Copy each template `mcpServers` key into the existing map (overwriting swe-swe servers, preserving user-defined ones)
4. Write the merged result to disk

If no existing file is found (or it contains invalid JSON), the template is used as-is.

The baseline file (`.swe-swe/baseline/.mcp.json`) always stores the raw template content, not the merged result. This preserves correct three-way merge behavior per ADR-030.

### Whiteboard rename

The `whiteboard` MCP server is renamed to `swe-swe-whiteboard`, consistent with ADR-023's naming convention (`swe-swe-playwright`, `swe-swe-preview`).

### Sync comment convention

The `upsertMcpServers()` function is duplicated in two places:
- `cmd/swe-swe/init.go` (runs on host during `swe-swe init`)
- `cmd/swe-swe/templates/host/swe-swe-server/main.go` (runs inside container)

Each copy has a `SYNC:` comment pointing to the other location.

## Consequences

Good:
- User-defined MCP servers survive `swe-swe init` re-runs
- Cloned repos get swe-swe servers even when `.mcp.json` already exists
- All swe-swe MCP servers follow the `swe-swe-` prefix convention
- Three-way merge (ADR-030) continues to work correctly via baseline

Bad:
- `upsertMcpServers()` is duplicated across two files (necessary because init runs on host, server runs inside container)
- JSON re-serialization via `MarshalIndent` changes formatting of the output `.mcp.json` (semantically equivalent, but cosmetically different from hand-formatted template)
