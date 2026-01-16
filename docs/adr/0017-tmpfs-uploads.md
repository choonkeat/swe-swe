# ADR-017: Tmpfs for file uploads

**Status**: Accepted
**Date**: 2026-01-05
**Task**: [tasks/2026-01-05-simplify-uploads-tmpfs.md](../../tasks/2026-01-05-simplify-uploads-tmpfs.md)

## Context

Users can upload files to the container via the web UI. These files are saved to `.swe-swe/uploads/` inside the container for the AI assistant to access.

The original implementation used a bind mount from the host, but this caused permission failures on macOS Docker Desktop:

1. Host user creates `.swe-swe/uploads/` with their UID (e.g., 501 on macOS)
2. Container app user has UID 1000
3. App user cannot write to directory owned by different UID
4. Original fix attempted `chown -R app:app /workspace/.swe-swe` in entrypoint.sh
5. **Problem**: macOS Docker Desktop uses VirtioFS, which doesn't support `chown` → EPERM error → container crash

## Decision

Use tmpfs (memory-based filesystem) for the uploads directory:

```yaml
# docker-compose.yml
volumes:
  - type: tmpfs
    target: /workspace/.swe-swe/uploads
    tmpfs:
      size: 100M
      mode: 0777
```

Key design choices:

1. **Tmpfs only, no host pre-creation**: The uploads directory is created entirely by Docker at container startup. We explicitly rejected pre-creating the directory on the host because:
   - Tmpfs shadows any host directory completely
   - Pre-creation misleads users into expecting files to persist on host
   - Pre-creation provides no fallback value (if tmpfs fails, container fails to start)

2. **Ephemeral by design**: Uploads are session-scoped and lost when container stops. Users who need persistence should move files to `/workspace` (which is bind-mounted from host).

3. **100MB size limit**: Prevents unbounded memory usage from file uploads. Reasonable for typical use cases (documents, images, code files).

4. **Mode 0777**: Ensures directory is writable regardless of container user context.

5. **Universal approach**: Same configuration for all platforms (Linux, macOS, Windows). We rejected OS-detection because:
   - Tmpfs works everywhere
   - Generated docker-compose.yml remains portable
   - Avoids complexity of conditional code paths
   - Edge cases (remote Docker, WSL2) make host OS detection unreliable

6. **UX feedback**: Upload button tooltip and success message indicate files are temporary.

## Consequences

**Good:**
- Works reliably on macOS Docker Desktop (VirtioFS) without permission hacks
- Works on Linux and Windows without modification
- Simple, single code path for all platforms
- No host filesystem pollution (uploads don't appear on host)
- Clear user expectations via UI hints

**Bad:**
- Uploads are ephemeral (lost on container restart) - mitigated by UI feedback
- 100MB limit may be restrictive for some use cases - can be manually adjusted in docker-compose.yml
- Uses RAM instead of disk - negligible for typical upload sizes

## Alternatives Considered

1. **Bind mount with chown in entrypoint**: Original approach, failed on macOS VirtioFS.

2. **Host pre-creation + tmpfs hybrid**: Pre-create directory on host, then mount tmpfs. Rejected because pre-creation adds no value when tmpfs shadows it, and misleads users about persistence.

3. **OS detection to choose bind vs tmpfs**: Detect host OS at `swe-swe init` time, use bind mount on Linux, tmpfs on macOS/Windows. Rejected because:
   - Adds complexity and maintenance burden
   - Generated docker-compose.yml becomes non-portable
   - Edge cases (remote Docker daemon, WSL2) break detection
   - Tmpfs works fine on Linux too

4. **Named Docker volume**: Would persist across restarts but adds complexity and isn't needed for session-scoped uploads.

## References

- [WebSocket protocol - file uploads](../websocket-protocol.md)
- [Original tmpfs implementation task](../../tasks/2026-01-05-tmpfs-uploads.md)
