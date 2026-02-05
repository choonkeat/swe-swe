# ADR-026: MCP debug channel tools

**Status**: Accepted
**Date**: 2026-02-02
**Research**: `research/2026-02-02-agent-debug-channel-discoverability.md`

## Context

When asked "what's showing on my preview?", agents reach for MCP Playwright's `browser_snapshot` instead of the debug channel. The browser tool sees Chrome's own UI (often a blank incognito tab), not the preview iframe content. The debug channel returns the actual DOM the user sees.

The debug channel existed as bash commands (`swe-swe-server --debug-query`, `--debug-listen`) documented in `.swe-swe/docs/app-preview.md`. However, agents only consulted this documentation when explicitly told to — tool selection happens by matching intent to available tools, and "what's on the page?" maps directly to `browser_snapshot`.

Every MCP-capable agent sees `browser_snapshot` in its tool list. The debug channel was invisible at decision time.

## Decision

Expose the debug channel as MCP tools served by `swe-swe-server --mcp`:

- **`browser_debug_preview`**: "Capture a snapshot of the Preview content by CSS selector. Returns the text, HTML, and visibility of matching elements in the Preview. This is the correct tool for inspecting the Preview — browser_snapshot cannot see Preview content."

- **`browser_debug_preview_listen`**: "Returns console logs, errors, and network requests from the Preview. Listens for the specified duration and returns all messages. This is the correct tool for debugging the Preview — browser_console_messages cannot see Preview output."

The tool descriptions explicitly state these are the correct tools for Preview inspection, competing directly with browser tools at decision time.

### Implementation

1. Add `--mcp` flag to `swe-swe-server` that runs a stdio MCP server (JSON-RPC over stdin/stdout)
2. Wire `swe-swe-preview` as a second MCP server in every agent platform's config (Claude, OpenCode, Codex, Gemini, Goose)
3. Tools connect to the existing debug WebSocket endpoints internally

### Alternatives Considered

| Approach | Effort | Coverage | Reliability |
|----------|--------|----------|-------------|
| Sharpen skill description | Low | Claude only | Medium |
| Per-platform instructions file | Medium | All slash-command agents | Fragile |
| **MCP tools (chosen)** | Higher | All MCP-capable agents | High |

The MCP approach puts the signal exactly where agents look when choosing tools — in the tool list itself.

## Consequences

**Good:**
- Agents discover preview tools alongside browser tools
- Tool descriptions compete at decision time
- Works for all MCP-capable platforms without per-platform hacks
- No external dependencies (hand-rolled JSON-RPC, protocol surface is small)

**Bad:**
- Another MCP server process per session
- Tool naming must be distinct from Playwright tools to avoid confusion
