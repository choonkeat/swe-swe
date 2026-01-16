# ADR-015: Chunked WebSocket Snapshots

**Status**: Accepted
**Date**: 2026-01-04
**Research**: `research/2026-01-04-ios-safari-websocket-chunking.md`

## Context

iOS Safari fails to receive large binary WebSocket messages (>6-8KB), causing connection instability. The VT10x terminal snapshot sent to new clients can exceed 10KB, especially with scrollback buffer.

HTTP polling was added as a fallback but adds complexity (~825 lines) and degrades UX.

## Decision

Replace single large binary snapshots with chunked, compressed messages:

1. **Compression**: gzip snapshots before sending (terminal text compresses 5-10x)
2. **Chunking**: Split into 8KB chunks with header `[0x02, index, total, ...data]`
3. **Client reassembly**: Collect chunks, decompress with DecompressionStream API
4. **Remove polling**: Chunking makes WebSocket reliable on all platforms

Protocol:
```
[0x02, chunkIndex, totalChunks, ...gzipCompressedData]
```

## Consequences

Good:
- WebSocket works reliably on iOS Safari
- Smaller bandwidth (compression)
- Simpler codebase (remove ~825 lines of polling code)
- Supports large scrollback buffers (500KB tested)

Bad:
- Client must handle chunk reassembly
- Slight complexity in send path
- Older browsers without DecompressionStream need polyfill (rare)
