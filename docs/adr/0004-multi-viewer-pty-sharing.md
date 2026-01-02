# ADR-004: Multi-viewer PTY sharing

**Status**: Accepted
**Date**: 2025-12-23
**Research**: [docs/connection-lifecycle.md](../connection-lifecycle.md)

## Context
Multiple users may view the same terminal session simultaneously (pair programming, demos). Each viewer may have different terminal dimensions.

## Decision
- Single PTY per session, shared by all connected WebSocket clients
- PTY size = minimum(rows) x minimum(cols) across all clients
- Late-joining clients receive a screen snapshot via VT100 emulation (ADR-011)
- Broadcast all PTY output to all clients

## Consequences
Good: True screen sharing, no drift between viewers, smallest viewport ensures all viewers see full content.
Bad: Large terminals constrained by smallest viewer, all input visible to all viewers.
