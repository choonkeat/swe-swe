# ADR-023: Unique MCP server name to avoid config conflicts

**Status**: Accepted
**Date**: 2026-01-12

## Context

Claude Code merges MCP server configurations from multiple sources:
- User-level config (`~/.claude/settings.json`, `~/.claude.json`)
- Project-level config (`.mcp.json` in workspace)

When a user has an existing `playwright` MCP server configured in their home directory (e.g., for local browser automation), it conflicts with swe-swe's containerized playwright setup. The configs get merged, and the user's config (without `--cdp-endpoint`) may take precedence, causing the MCP server to try launching a local browser instead of connecting to the Chrome container.

Symptoms of this conflict:
```
Error: browserType.launchPersistentContext: Chromium distribution 'chrome' is not found at /opt/google/chrome/chrome
```

The MCP server ignores `--cdp-endpoint` because a conflicting config from `~/.claude/*` is being used.

## Decision

Use `swe-swe-playwright` as the MCP server name instead of `playwright`.

```json
{
  "mcpServers": {
    "swe-swe-playwright": {
      "command": "npx",
      "args": ["-y", "@playwright/mcp@latest", "--cdp-endpoint", "http://chrome:9223"]
    }
  }
}
```

This means tool names change from `mcp__playwright__*` to `mcp__swe-swe-playwright__*`.

## Consequences

Good:
- No naming conflicts with user's existing MCP configurations
- Clear indication that these are swe-swe-specific browser tools
- Users can have both local playwright and swe-swe playwright configured simultaneously

Bad:
- Longer tool names (`mcp__swe-swe-playwright__browser_navigate` vs `mcp__playwright__browser_navigate`)
- Documentation must reference the full tool names
- Existing swe-swe installations need to regenerate their `.mcp.json` (rebuild container)
