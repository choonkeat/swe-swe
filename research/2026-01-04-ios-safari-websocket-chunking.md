# iOS Safari WebSocket Large Message Issue & Chunking Solution

**Date**: 2026-01-04
**Related**: ADR-0015 (Chunked WebSocket snapshots), Task `2026-01-04-remove-polling-code.md`

## Problem Discovery

While testing mobile viewing of swe-swe terminal sessions, iOS Safari exhibited WebSocket instability:
- WebSocket connections disconnected every 2-5 seconds
- Close code 1005 "no status" (abnormal closure)
- Auto-reconnect worked, but timer kept resetting
- Desktop clients on same session remained stable

Initially suspected:
- iOS Safari background tab behavior
- Network issues (WiFi/cellular)
- Power management / low power mode

## Isolation Testing

Created minimal WebSocket test server (`/workspace/tmp/ws-test/`) to isolate the issue.

### Test 1: Minimal WebSocket (small text messages)
- **Result**: Stable connection, no disconnects
- **Conclusion**: iOS Safari WebSocket itself is fine

### Test 2: Same container/port setup as swe-swe
- **Result**: Still stable
- **Conclusion**: Docker/networking not the issue

### Test 3: Mimic swe-swe-server patterns
Added:
- `binaryType = 'arraybuffer'`
- `ReadBufferSize: 1024, WriteBufferSize: 1024` upgrader config
- Binary snapshot on connect
- JSON status messages

Results by binary message size:
| Size | Result |
|------|--------|
| 1KB | Stable |
| 5KB | Stable |
| 6KB | Unstable (sometimes) |
| 8KB | Unstable (timeout, error, close loop) |
| 10KB | Unstable |

**Root Cause**: iOS Safari has issues with single large binary WebSocket messages (~6-8KB+)

## Solution: Chunking + Compression

### Protocol Design

Chunk marker byte `0x02` followed by index, total, and compressed data:
```
[0x02, chunkIndex, totalChunks, ...gzipCompressedData]
```

### Server-Side (Go)
```go
func sendChunked(conn *websocket.Conn, data []byte, chunkSize int) error {
    compressed := gzipCompress(data)
    totalChunks := (len(compressed) + chunkSize - 1) / chunkSize

    for i := 0; i < totalChunks; i++ {
        chunk := []byte{0x02, byte(i), byte(totalChunks)}
        chunk = append(chunk, compressed[start:end]...)
        conn.WriteMessage(websocket.BinaryMessage, chunk)
    }
    return nil
}
```

### Client-Side (JavaScript)
```javascript
let chunks = [];
let expectedChunks = 0;

async function handleChunk(data) {
    const arr = new Uint8Array(data);
    if (arr[0] !== 0x02) return; // Not a chunk

    const chunkIndex = arr[1];
    const totalChunks = arr[2];
    chunks[chunkIndex] = arr.slice(3);

    if (chunks.filter(c => c).length === totalChunks) {
        const compressed = reassemble(chunks);
        const decompressed = await decompress(compressed); // DecompressionStream API
        // Use decompressed data
    }
}
```

### Compression Results

| Data Type | Original | Compressed | Ratio |
|-----------|----------|------------|-------|
| Random bytes | 20KB | 20KB | 100% (no compression) |
| Terminal text + ANSI | 20KB | ~2KB | 10% |
| Repeating pattern | 20KB | 112B | 0.6% |

Real terminal data compresses very well (5-10x typical).

### Chunk Size Testing

With chunking enabled:
| Chunk Size | Chunks for 500KB | Result |
|------------|------------------|--------|
| 4KB | 125 | Stable |
| 8KB | 62 | Stable |

**Conclusion**: 8KB chunks work reliably, even for large payloads (tested up to 500KB).

## Adaptive Chunk Sizing (Optional Enhancement)

Track delivery success per session:
1. Default chunk size: 8KB
2. If client disconnects within 3 seconds of chunk send → reduce by 15%
3. Minimum chunk size: 512 bytes
4. Never auto-increase (avoid disconnect cycles)

```go
type Session struct {
    ChunkSize     int
    LastChunkSent time.Time
    StableCount   int
}

func (s *Session) markDeliveryFailed() {
    if time.Since(s.LastChunkSent) < 3*time.Second {
        s.ChunkSize = int(float64(s.ChunkSize) * 0.85)
        if s.ChunkSize < 512 {
            s.ChunkSize = 512
        }
    }
}
```

## Why Remove Polling?

HTTP polling was added as fallback for WebSocket failures. With chunking:
1. WebSocket works reliably on iOS Safari
2. Polling adds ~600 lines of client code, ~225 lines of server code
3. Polling has worse UX (latency, no streaming, textarea input)
4. Chunking is simpler and works everywhere

## Recommendations

1. **Implement chunked snapshots** with 8KB default chunk size
2. **Add gzip compression** for snapshots
3. **Remove HTTP polling** code entirely
4. **Consider adaptive sizing** for edge cases (optional, low priority)
5. **Increase VT10x scrollback buffer** to 500KB for better mobile scrolling

## Test Artifacts

Test server code preserved in `/workspace/tmp/ws-test/` for future reference.
