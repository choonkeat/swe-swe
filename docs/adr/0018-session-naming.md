# ADR-018: Session naming

**Status**: Accepted
**Date**: 2026-01-06

## Context

Sessions are identified by UUIDs which are difficult to remember and distinguish. Users working with multiple sessions need a way to identify them quickly, especially when returning to the homepage or sharing session links.

## Decision

Add optional user-assigned names to sessions:

- **WebSocket message**: `rename_session` with `name` field
- **Status broadcast**: Include `sessionName` and `uuidShort` in status messages
- **Validation**: Max 32 chars, alphanumeric + spaces + hyphens + underscores only
- **Storage**: In-memory only (names don't persist across server restarts)
- **Display**: Homepage shows name (or short UUID fallback), status bar shows name

## Consequences

Good:
- Users can identify sessions at a glance ("feature-x", "debugging-api")
- Short UUID fallback ensures sessions are always identifiable
- Simple validation prevents injection attacks
- No database required - names are session-scoped

Bad:
- Names don't persist if server restarts (acceptable for ephemeral sessions)
- No uniqueness enforcement (two sessions can have same name)
- Character restrictions may frustrate users wanting special characters
