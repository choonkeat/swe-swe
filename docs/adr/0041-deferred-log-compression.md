# ADR-0041: Deferred Log Compression

**Status**: Accepted
**Date**: 2026-03-31

## Context

Terminal recordings from `script` can grow large (1-2 GB for long Claude Code sessions) because the TUI generates highly repetitive screen redraws. Compression achieves ~100x reduction, so we wanted gzip compression.

The initial approach (a447adcb6) used a real-time FIFO pipeline:

```
mkfifo session.log.pipe
gzip > session.log.gz < session.log.pipe &
script -O session.log.pipe -c cmd
```

This produced 0-byte `.log.gz` files. Investigation revealed the root cause: GNU gzip buffers compressed output internally and only writes to the output file on EOF. With ~1.2 MB read from the FIFO and 0 bytes written to `.log.gz`, gzip was accumulating data but never flushing during the session lifetime.

When sessions ended via `killSessionProcessGroup`, the sequence was:
1. SIGTERM to the process group
2. 3-second grace period, then SIGKILL
3. Descendant cleanup (SIGKILL to escaped processes)

Even with `setsid` isolating gzip from the process group SIGTERM (ec843caf4), the descendant cleanup SIGKILLed gzip before it could flush. The FIFO approach was fundamentally incompatible with gzip's buffering model and our process termination strategy.

## Decision

Decouple compression from the recording pipeline entirely:

1. **During session**: `script -O session.log` writes uncompressed (the original, proven approach)
2. **After session ends**: The cleanup scheduler (`cleanupRecentRecordings`, runs every minute) compresses `.log` to `.log.gz` and removes the original
3. **Playback**: `resolveLogPath` checks `.log.gz` first, falls back to `.log`; `openLogReader` transparently decompresses based on gzip magic bytes

Compression uses atomic temp file + rename to avoid partial `.log.gz` files if the server is interrupted mid-compression.

## Consequences

Good:
- Eliminates all FIFO/setsid/gzip pipeline complexity from session startup
- Session end is instant (no compression delay)
- Compression is reliable because it runs after all writes are complete
- No process coordination issues (no race conditions, no signal handling)
- Playback works seamlessly for both recent (uncompressed) and older (compressed) recordings

Bad:
- Disk usage is higher temporarily (uncompressed logs live up to ~1 minute)
- A 2 GB session log could briefly consume disk before the next scheduler tick
- If the server crashes between session end and the next scheduler run, the `.log` file remains uncompressed until the next run

## Alternatives Considered

- **Background goroutine at session end**: Adds concurrency complexity, and large logs (1.5 GB) take ~13 seconds to compress, tying up a goroutine
- **Streaming compression with `pigz` or custom flushing**: Over-engineering for a problem that deferred compression solves simply
- **Real-time FIFO with `gzip --rsyncable` or `Z_SYNC_FLUSH`**: GNU gzip CLI doesn't expose flush control; would require a custom compression binary
