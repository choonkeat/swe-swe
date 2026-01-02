# ADR-007: Conditional Dockerfile generation

**Status**: Accepted
**Date**: 2025-12-24
**Research**: `cmd/swe-swe/main.go:244-317`

## Context
Different users need different AI agents (Claude, Gemini, Codex, Aider, Goose). Installing all agents increases image size and build time unnecessarily.

## Decision
Use template markers in Dockerfile:
```dockerfile
# {{IF CLAUDE}}
RUN npm install -g @anthropic-ai/claude-code
# {{ENDIF}}
```
`swe-swe init --agents=claude,gemini` processes the template, keeping only selected sections.

## Consequences
Good: Minimal images, fast builds, users choose what they need, supports `--apt-get-install` and `--npm-install` for customization.
Bad: Template syntax adds complexity, Dockerfile not directly readable without processing.
