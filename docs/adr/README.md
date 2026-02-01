# Architecture Decision Records

This directory contains Architecture Decision Records (ADRs) for the swe-swe project.

## Format

```markdown
# ADR-NNN: Title

**Status**: Proposed | Accepted | Deprecated | Superseded by ADR-XXX
**Date**: YYYY-MM-DD
**Research**: [optional link to research/ file]

## Context
Why we need to make this decision.

## Decision
What we decided.

## Consequences
Good: ...
Bad: ...
```

## Index

| ADR | Title | Status |
|-----|-------|--------|
| [001](0001-metadata-storage-location.md) | Metadata storage location | Accepted |
| [002](0002-path-based-routing.md) | Path-based routing over subdomains | Accepted |
| [003](0003-websocket-protocol-design.md) | WebSocket protocol design | Accepted |
| [004](0004-multi-viewer-pty-sharing.md) | Multi-viewer PTY sharing | Accepted |
| [005](0005-browser-automation-architecture.md) | Browser automation architecture | Accepted |
| [006](0006-nginx-sidecar-for-vscode.md) | nginx sidecar for VSCode | Accepted |
| [007](0007-conditional-dockerfile-generation.md) | Conditional Dockerfile generation | Accepted |
| [008](0008-forwardauth-unified-auth.md) | ForwardAuth unified authentication | Accepted |
| [009](0009-syscall-exec-for-up.md) | syscall.Exec for swe-swe up | Accepted |
| [010](0010-session-restart-behavior.md) | Session restart behavior | Accepted |
| [011](0011-vt100-emulation-snapshots.md) | VT100 emulation for snapshots | Accepted |
| [012](0012-enterprise-ssl-cert-handling.md) | Enterprise SSL certificate handling | Accepted |
| [013](0013-docker-socket-mounting.md) | Docker socket mounting | Accepted |
| [014](0014-slash-commands-cloning.md) | Slash commands cloning | Accepted |
| [015](0015-chunked-websocket-snapshots.md) | Chunked WebSocket snapshots | Accepted |
| [016](0016-ios-safari-websocket-self-signed-certs.md) | iOS Safari WebSocket with self-signed certs | Accepted |
| [017](0017-tmpfs-uploads.md) | Tmpfs for file uploads | Accepted |
| [018](0018-session-naming.md) | Session naming | Accepted |
| [019](0019-terminal-recording.md) | Terminal recording with script command | Accepted |
| [020](0020-git-worktree-integration.md) | Git worktree integration for sessions | Accepted |
| [021](0021-worktree-mount-path.md) | Worktree mount path | Accepted |
| [022](0022-simplified-worktree-exit.md) | Simplified worktree exit | Accepted |
| [023](0023-unique-mcp-server-name.md) | Unique MCP server name | Accepted |
| [024](0024-debug-injection-proxy-security.md) | Debug injection proxy security | Accepted |
| [025](0025-per-session-preview-ports.md) | Per-session preview ports | Accepted |
