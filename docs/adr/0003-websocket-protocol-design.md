# ADR-003: WebSocket protocol design

**Status**: Accepted
**Date**: 2025-12-23
**Research**: [docs/websocket-protocol.md](../websocket-protocol.md)

## Context
Terminal sessions need bidirectional communication for I/O, resize events, file uploads, chat, and status updates.

## Decision
- **Binary frames**: Terminal I/O (raw bytes), with prefix bytes for special messages:
  - `0x00` = terminal resize
  - `0x01` = file upload
  - `0x02` = chunked message (for iOS Safari compatibility, see ADR-015)
- **Text frames**: JSON control messages:
  - `ping/pong` - heartbeat
  - `chat` - in-session chat
  - `status` - session status broadcast (viewers, size, assistant, sessionName)
  - `file_upload` - upload response
  - `rename_session` - session rename request (see ADR-018)
  - `exit` - process exit notification

## Consequences
Good: Efficient binary I/O, extensible JSON control plane, clear separation of concerns.
Bad: Protocol requires documentation, prefix bytes must be carefully chosen to avoid conflicts.
