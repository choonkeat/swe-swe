# ADR-024: Debug injection proxy security model

**Status**: Accepted
**Date**: 2026-01-22

## Context

The debug injection proxy allows the agent to receive console logs, errors, and network requests from the user's app preview iframe. It also allows the agent to send DOM queries to the iframe.

This creates two WebSocket endpoints:
- `/__swe-swe-debug__/ws` - iframe connects here (sender)
- `/__swe-swe-debug__/agent` - agent connects here (receiver, can send queries)

The agent endpoint has higher privilege than read-only preview access because it can send DOM queries that execute in the user's browser context.

## Decision

### SSL Mode (Production)

External access to debug endpoints goes through Traefik with `forwardauth` middleware:
```
External → Traefik:${SWE_PORT} → forwardauth → blocked without valid session cookie
```

The agent CLI (`swe-swe-server --debug-listen`) connects internally via localhost, bypassing Traefik:
```
Agent (inside container) → ws://localhost:9899/__swe-swe-debug__/agent → works
```

This is secure because:
1. External attackers cannot connect without a valid session cookie
2. The agent runs inside the container and connects via localhost
3. No credentials need to be passed to the agent CLI

### NO_SSL Mode (Local Development)

The preview proxy port is directly mapped to the host, bypassing Traefik:
```yaml
# docker-compose.yml (NO_SSL mode only)
ports:
  - "1${SWE_PORT:-1977}:9899"
```

This means debug endpoints are accessible to anyone who can reach the host port. This is **intentional** and consistent with the existing preview exposure in NO_SSL mode.

NO_SSL mode assumes a trusted local network (typically localhost or private LAN during development).

### Threat Model Comparison

| Asset | Preview (existing) | Debug Channel (new) |
|-------|-------------------|---------------------|
| Risk | Attacker sees user's app | Attacker can query DOM in user's browser |
| SSL mode | Protected by forwardauth | Protected by forwardauth |
| NO_SSL mode | Exposed (trusted network) | Exposed (trusted network) |

The debug channel does not introduce new exposure patterns - it inherits the existing preview security model.

## Consequences

**Good:**
- Agent can debug user's app without visual access to preview
- No additional auth mechanism needed for agent CLI
- Consistent with existing security model

**Bad:**
- NO_SSL mode exposes debug channel to local network (same as preview)
- DOM query capability is more powerful than read-only preview
- If NO_SSL exposure becomes a concern, both preview and debug channel need addressing together
