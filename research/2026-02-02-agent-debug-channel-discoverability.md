# Agent Debug Channel Discoverability

**Date**: 2026-02-02
**Status**: Research

## Problem

When asked "what's showing on my preview?", agents reach for MCP Playwright's `browser_snapshot` instead of the debug channel (`swe-swe-server --debug-query`). The browser tool sees Chrome's own UI (often a blank incognito tab), not the preview iframe content. The debug channel returns the actual DOM the user sees.

Agents only use the debug channel when explicitly told to read `app-preview.md`. The docs exist but aren't consulted at tool-selection time.

## Root Cause

Tool selection happens by matching intent to available tools. "What's on the page?" maps directly to `browser_snapshot` — it's a browser tool that shows page content. The debug channel is a bash command documented in files the agent hasn't read yet. There's no competing signal when the agent decides which tool to use.

## What Each Agent Sees at Decision Time

| Layer | Claude Code | OpenCode | Codex | Gemini | Aider/Goose |
|-------|------------|----------|-------|--------|-------------|
| **Always loaded** | CLAUDE.md (if exists) | — | — | — | — |
| **Skill list** | system-reminder lists `/debug-with-app-preview` | — | slash commands | slash commands | — |
| **MCP tools** | `swe-swe-playwright` browser tools | `swe-swe-playwright` browser tools | `swe-swe-playwright` browser tools | `swe-swe-playwright` browser tools | no MCP |
| **On-demand docs** | `.swe-swe/docs/` | `.swe-swe/docs/` | `.swe-swe/docs/` | `.swe-swe/docs/` | `@`-mentionable |

Every MCP-capable agent sees `browser_snapshot` in its tool list. The debug channel only exists as a bash command in unread docs.

## Agent Platform Discovery Mechanisms

### Claude Code

Strongest discovery path — skills appear in system-reminder every turn.

**Skill description** (current): `Debug web apps using the App Preview debug channel`

This is generic. It doesn't compete with `browser_snapshot` at decision time. A sharper description would plant the right association:

```
Inspect App Preview page content — use instead of browser tools for preview
```

**CLAUDE.md**: Created by the agent during `/setup`. The setup prompt could instruct the agent to include a preview directive, but CLAUDE.md content is agent-generated — fragile.

### OpenCode / Codex / Gemini (slash-command agents)

These have slash commands but no equivalent of Claude's always-visible skill list. They see `debug-with-app-preview` only if they search for commands or the user invokes it.

These agents need the signal in their project instructions file or system prompt — the equivalent of CLAUDE.md. But each platform uses a different file (OpenCode: `instructions.md`, Codex: `AGENTS.md`, Gemini: `GEMINI.md`), making per-platform injection fragile.

### Aider / Goose (file-mention agents)

No MCP tools, no slash commands. They run bash commands and `@`-mention files. Since they have no `browser_snapshot` bias (no MCP), the gap is smaller — they'd reach for `swe-swe-server` commands if they knew about them.

## Possible Approaches

### 1. Sharpen skill description (Claude only)

Change the `/debug-with-app-preview` description to explicitly compete with browser tools:

```
Inspect App Preview page content — use instead of browser tools for preview
```

- **Effort**: One-line change in `.md` and `.toml`
- **Coverage**: Claude Code only
- **Reliability**: Medium — depends on model reading skill list before choosing tools

### 2. Per-platform instructions file

During container setup (entrypoint.sh), append a directive to each platform's auto-loaded file:

| Platform | Auto-loaded file |
|----------|-----------------|
| Claude | CLAUDE.md (via setup prompt) |
| OpenCode | workspace `instructions.md` |
| Codex | `AGENTS.md` at workspace root |
| Gemini | `GEMINI.md` |

Directive (2 lines):
```
To inspect App Preview content, use `swe-swe-server --debug-query` (not browser tools).
See `.swe-swe/docs/app-preview.md` for details.
```

- **Effort**: Medium — different file per platform, setup prompt changes
- **Coverage**: All slash-command agents
- **Reliability**: Fragile — each platform's auto-discovery file differs and may change

### 3. Expose debug channel as MCP tools

Register `debug-query` and `debug-listen` as MCP tools served by swe-swe-server. Agents would see tools like:

- `swe_preview_query(selector)` — "Returns DOM content from the App Preview iframe. Use this to inspect what the user sees in their preview panel."
- `swe_preview_listen()` — "Streams console logs, errors, and network requests from the App Preview iframe in real-time."

These appear alongside `browser_snapshot` in the tool list, competing directly at decision time.

- **Effort**: Higher — implement MCP server endpoint in swe-swe-server
- **Coverage**: All MCP-capable agents (Claude, OpenCode, Codex, Gemini, Goose)
- **Reliability**: High — tools compete at decision time, which is exactly when agents choose

## Comparison

| Approach | Effort | Coverage | Reliability |
|----------|--------|----------|-------------|
| Sharpen skill description | Low | Claude only | Medium |
| Per-platform instructions | Medium | All slash-command agents | Fragile |
| MCP tools for debug channel | Higher | All MCP-capable agents | High |

## Recommendation

Do both:

1. **Now**: Sharpen the skill description (one-line change, immediate improvement for Claude)
2. **Next**: Expose debug-query and debug-listen as MCP tools

The MCP approach is the proper long-term fix. It puts the signal exactly where agents look when choosing tools — in the tool list itself. Tool descriptions compete directly with `browser_snapshot`, and every MCP-capable platform benefits without per-platform config hacks.
