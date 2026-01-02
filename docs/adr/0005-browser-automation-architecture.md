# ADR-005: Browser automation architecture

**Status**: Accepted
**Date**: 2025-12-26
**Research**: [research/2025-12-26-browser-automation-in-docker.md](../../research/2025-12-26-browser-automation-in-docker.md)

## Context
AI agents need browser control for web automation. Developers need to observe browser state in real-time for debugging.

## Decision
- Chrome runs in sidecar container with Xvfb (virtual framebuffer)
- noVNC exposes the display over WebSocket for observation at `/chrome`
- Chrome DevTools Protocol (CDP) on port 9222 for programmatic control
- MCP Playwright connects to Chrome via CDP from swe-swe-server container

## Consequences
Good: Full visual observability, works on all platforms, same setup for local and CI.
Bad: ~50-100MB RAM overhead for X11/VNC, slightly larger Docker image.
