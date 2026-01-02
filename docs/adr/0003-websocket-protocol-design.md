# ADR-003: WebSocket protocol design

**Status**: Accepted
**Date**: 2025-12-23
**Research**: [docs/websocket-protocol.md](../websocket-protocol.md)

## Context
Terminal sessions need bidirectional communication for I/O, resize events, file uploads, chat, and status updates.

## Decision
- **Binary frames**: Terminal I/O (raw bytes), with prefix bytes for special messages (`0x00` = resize, `0x01` = file upload)
- **Text frames**: JSON control messages (`ping/pong`, `chat`, `status`, `file_upload` response)

## Consequences
Good: Efficient binary I/O, extensible JSON control plane, clear separation of concerns.
Bad: Protocol requires documentation, prefix bytes must be carefully chosen to avoid conflicts.
