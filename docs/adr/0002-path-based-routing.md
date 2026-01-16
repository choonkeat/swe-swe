# ADR-002: Path-based routing over subdomains

**Status**: Accepted
**Date**: 2025-12-27
**Research**: [research/2025-12-27-path-based-vscode-routing.md](../../research/2025-12-27-path-based-vscode-routing.md), [research/2025-12-24-path-based-routing-edge-cases.md](../../research/2025-12-24-path-based-routing-edge-cases.md)

## Context
Services (terminal, VSCode, Chrome VNC) need to be accessible via a single entry point. Subdomain routing (`vscode.lvh.me`) requires DNS wildcards and doesn't work with raw IPs.

## Decision
Use path-based routing: `/` for terminal, `/vscode` for code-server, `/chrome` for noVNC. All services accessible via `http://host:9899/path`.

## Consequences
Good: Works with any hostname including `localhost` and raw IPs, no DNS configuration needed, single port.
Bad: Requires reverse proxy path rewriting, some services need nginx sidecar for redirect handling (see ADR-006).
