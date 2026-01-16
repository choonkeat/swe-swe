# ADR-010: Session restart behavior

**Status**: Accepted
**Date**: 2025-12-23
**Research**: [docs/connection-lifecycle.md:141-208](../connection-lifecycle.md)

## Context
When the shell process exits, the session could either restart automatically or terminate. Different exit codes indicate different situations.

## Decision
- **Exit code 0** (success): Do not restart. User intentionally exited (typed `exit`). Show `[Process exited successfully]`.
- **Non-zero exit** (error/crash): Restart after 500ms. Show `[Process exited with code X, restarting...]`.
- **No clients connected**: Never restart (no one to use the session).

## Consequences
Good: Clean exits stay clean, crashes auto-recover, no infinite restart loops on intentional exit.
Bad: If user wants to restart after clean exit, must reconnect or refresh.
