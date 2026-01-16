# ADR-009: syscall.Exec for swe-swe up

**Status**: Accepted
**Date**: 2025-12-24
**Research**: [tasks/2025-12-24-syscall-exec.md](../../tasks/2025-12-24-syscall-exec.md)

## Context
`swe-swe up` runs `docker compose up`. When using subprocess, Ctrl+C signals go to swe-swe first, requiring signal forwarding. This is fragile and adds latency.

## Decision
On Unix/Linux/macOS, use `syscall.Exec()` to replace the swe-swe process with docker compose. On Windows (where Exec unavailable), fall back to subprocess with signal forwarding.

## Consequences
Good: Signals go directly to docker compose, cleaner process tree, no zombie processes.
Bad: swe-swe process disappears (can't run post-up logic), Windows uses different code path.
