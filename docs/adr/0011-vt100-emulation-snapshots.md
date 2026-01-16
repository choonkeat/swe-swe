# ADR-011: VT100 emulation for snapshots

**Status**: Accepted
**Date**: 2025-12-23
**Research**: `cmd/swe-swe/templates/host/swe-swe-server/main.go:103`

## Context
When a new client joins an existing session, they need to see the current screen state. Simply replaying all historical output would be slow and waste bandwidth.

## Decision
Use `hinshun/vt10x` library to maintain a virtual terminal that tracks screen state:
- All PTY output written to VT100 emulator
- On new client connect, generate snapshot from VT100 state
- Send snapshot as binary message before live streaming

## Consequences
Good: Instant catch-up for late joiners, accurate screen state, handles scrollback and cursor position.
Bad: Memory overhead for VT100 state, must keep VT100 size in sync with PTY size.
