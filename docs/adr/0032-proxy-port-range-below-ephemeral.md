# ADR-032: Proxy port range below ephemeral

**Status**: Accepted
**Date**: 2026-02-15

## Context

ADR-025 introduced per-session preview ports with derived proxy ports using `5{PORT}` for preview (50000 + port) and `4{PORT}` for agent chat (40000 + port). This placed proxy ports in the 44000-44019 and 53000-53019 ranges.

Users reported intermittent `swe-swe up` failures:

```
Error response from daemon: failed to set up container networking:
  ...failed to bind host port for 0.0.0.0:53004:172.19.0.7:53004/tcp:
  address already in use
```

The port appeared free when checked with `lsof` — the conflict was transient. The cause: both macOS (49152-65535) and Linux (32768-60999) assign ephemeral ports from ranges that overlap with our proxy ports. When the OS assigns an outbound connection to e.g. 53004, Docker's bind fails. The ephemeral connection may close milliseconds later, making the conflict impossible to reproduce.

## Decision

Change both `previewProxyPort` and `agentChatProxyPort` to use a uniform `20000 + port` offset:

| Service     | App Port  | Old Proxy Port  | New Proxy Port  |
|-------------|-----------|-----------------|-----------------|
| Preview     | 3000-3019 | 53000-53019     | 23000-23019     |
| Agent Chat  | 4000-4019 | 44000-44019     | 24000-24019     |

Both ranges are below 32768 (Linux ephemeral floor) and well below 49152 (macOS ephemeral floor).

The two ranges don't overlap because preview app ports (3000-3019) and agent chat app ports (4000-4019) are disjoint, and both map through the same `20000 + port` formula.

## Consequences

- Existing users must re-run `swe-swe init` (or manually update their generated `docker-compose.yml`) to pick up the new ports.
- The `5{PORT}` and `4{PORT}` conventions documented in ADR-025 are superseded by a uniform `20000 + port`.
- One fewer thing to debug: ephemeral port collisions were transient and hard to reproduce, so eliminating them removes a class of "works on my machine" failures.
- The offset is configurable via `--proxy-port-offset` (default 20000) for environments where the 20000-24019 range conflicts with other services.
