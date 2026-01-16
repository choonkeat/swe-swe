# ADR-006: nginx sidecar for VSCode

**Status**: Accepted
**Date**: 2025-12-27
**Research**: [research/2025-12-27-path-based-vscode-routing.md:85-137](../../research/2025-12-27-path-based-vscode-routing.md)

## Context
code-server generates relative redirects (`./login`) which break under path-based routing. Traefik's StripPrefix doesn't rewrite Location headers, and code-server ignores `X-Forwarded-Prefix`.

## Decision
Run nginx between Traefik and code-server:
- Traefik routes `/vscode*` to nginx (no StripPrefix)
- nginx strips `/vscode` prefix before proxying
- nginx rewrites Location headers via `proxy_redirect ~^/(.*)$ /vscode/$1`

## Consequences
Good: Redirects work correctly, code-server unmodified, pattern reusable for other services.
Bad: Extra container, slightly more complex architecture.
