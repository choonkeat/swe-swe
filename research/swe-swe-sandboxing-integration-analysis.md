# swe-swe: Lighter Sandboxing Options Analysis

## Current swe-swe Architecture (What We're Replacing)

**6 containers:**
1. `swe-swe` - Go WebSocket terminal server + PTY + agent execution
2. `chrome` - Xvfb + Chromium + nginx (CDP proxy) + x11vnc + websockify
3. `code-server` - VS Code in browser
4. `vscode-proxy` - nginx for VS Code routing
5. `traefik` - reverse proxy + path routing
6. `auth` - ForwardAuth service

**What's "heavy":**
- 6 containers for a dev session
- Chrome container alone runs 5 processes (supervisord-managed)
- Multi-stage Docker build from source
- Traefik + auth overhead for single-user scenarios

**Hard requirement:** Chrome container (or equivalent) - provides both CDP for agent automation AND VNC for human observation. This is the irreducible core for browser-based agent work.

---

## Option 1: Fly.io Sprites

### How It Could Work

```
┌─ Cloud (Fly.io) ─────────────────────┐
│                                      │
│  Sprite (persistent VM)              │
│  ├─ Agent (claude, aider, etc.)      │
│  ├─ Project files                    │
│  └─ Dev tools installed once         │
│                                      │
└──────────────────────────────────────┘
           ↓ SSH/WebSocket
┌─ Local ──────────────────────────────┐
│                                      │
│  Chrome container (still needed)     │
│  └─ CDP + VNC                        │
│                                      │
│  Thin client (terminal UI only)      │
│                                      │
└──────────────────────────────────────┘
```

### Benefits
- **Persistent state** - agents don't rebuild environments each session
- **Instant checkpoints** - snapshot before risky operations, restore on failure
- **Lighter local footprint** - offload compute to cloud
- **No Docker build time** - Sprites boot in 1-2 seconds
- **100GB storage** - room for large projects, node_modules, etc.

### Challenges
- **Chrome container still needed locally** - browser automation requires low-latency CDP
- **Networking complexity** - Sprite needs to reach local chrome container (reverse tunnel?)
- **Not self-hostable** - Fly.io only, vendor lock-in
- **Cost** - cloud compute vs local Docker (free)
- **Latency** - terminal I/O over internet vs local
- **Offline unusable** - requires internet

### Could Chrome Run in Sprite Too?
Theoretically yes, but:
- VNC over internet = laggy visual observation
- CDP over internet = slower automation
- Would need public URL or tunnel for developer to watch

### Verdict: **Interesting but clunky**

The split architecture (cloud agent + local browser) creates complexity. If both run in Sprite, latency hurts UX. Main benefit (persistence) could be achieved locally with volume mounts.

**Worth it?** Maybe for teams wanting shared cloud dev environments. Not for "lighter local setup."

---

## Option 2: packnplay

### How It Could Work

```
┌─ packnplay container ────────────────┐
│                                      │
│  Agent (claude, aider, etc.)         │
│  Git worktree (auto-managed)         │
│  Devcontainer spec support           │
│                                      │
└──────────────────────────────────────┘
           +
┌─ Chrome container (unchanged) ───────┐
│  CDP + VNC (still needed)            │
└──────────────────────────────────────┘
```

Replace swe-swe's agent container with packnplay, keep chrome container.

### Benefits
- **Worktree management built-in** - matches swe-swe's session isolation model
- **Devcontainer support** - leverage existing project configs
- **Credential forwarding** - Git, SSH, AWS SSO handled automatically
- **Simpler than full swe-swe** - no traefik, no auth, no code-server

### Challenges
- **No WebSocket terminal server** - packnplay runs agent directly in TTY
- **No multi-viewer support** - swe-swe allows multiple devs watching one session
- **No web UI** - packnplay is CLI-only, swe-swe has browser-based terminal
- **No session recording/playback** - swe-swe has this
- **Still need chrome orchestration** - packnplay doesn't manage multi-container setups
- **No authentication** - fine for local, problem for remote/shared access

### What We'd Lose
- Browser-based terminal UI (the whole point of swe-swe for sharing)
- Session recording and playback
- Multi-developer collaboration
- Integrated VS Code
- Single-port access to all services

### Verdict: **Different use case**

packnplay solves "run agent in container safely." swe-swe solves "share agent sessions with teammates via browser." They're complementary, not replacements.

**Worth it?** No - doesn't provide what swe-swe users need (shareable web UI).

---

## Option 3: Leash (StrongDM)

### How It Could Work

```
┌─ Leash manager container ────────────┐
│  Policy engine (Cedar)               │
│  Syscall monitoring                  │
│  MCP observer                        │
│  Audit UI (localhost:18080)          │
└──────────────────────────────────────┘
           ↓ monitors
┌─ Agent container ────────────────────┐
│  Agent (claude, aider, etc.)         │
│  All syscalls captured               │
│  MCP calls intercepted               │
└──────────────────────────────────────┘
           +
┌─ Chrome container (unchanged) ───────┐
└──────────────────────────────────────┘
           +
┌─ swe-swe-server (unchanged) ─────────┐
└──────────────────────────────────────┘
```

### Benefits
- **Policy enforcement** - Cedar policies for what agent can/can't do
- **Full audit trail** - every file access, network call logged
- **MCP observer** - see exactly what tools agent is calling
- **Enterprise compliance** - prove what agent did for audits

### Challenges
- **Adds complexity, not removes it** - now 7+ containers instead of 6
- **Performance overhead** - syscall monitoring has cost
- **Integration work** - need to wire Leash into swe-swe's container setup
- **Overkill for most users** - useful for enterprise, not indie devs

### Verdict: **Additive, not reductive**

Leash is valuable for enterprises needing audit/compliance. It doesn't make swe-swe lighter - it makes it heavier but more auditable.

**Worth it?** Yes, as an optional add-on for enterprise deployments. Not for "lighter" goal.

---

## Alternative: Simplify swe-swe Itself

Instead of external solutions, consider consolidating:

### Current (6 containers)
```
swe-swe + chrome + code-server + vscode-proxy + traefik + auth
```

### Simplified (2-3 containers)
```
┌─ swe-swe-unified ────────────────────┐
│  Terminal server (existing Go code)  │
│  Built-in auth (no separate service) │
│  Built-in reverse proxy (no traefik) │
│  Optional: embedded code-server      │
└──────────────────────────────────────┘
           +
┌─ chrome (unchanged - irreducible) ───┐
│  Xvfb + Chromium + VNC               │
└──────────────────────────────────────┘
```

### What This Saves
- **No traefik** - Go stdlib can do path routing
- **No auth container** - embed in main server
- **No vscode-proxy** - handle in main server
- **Optional code-server** - many users don't need browser IDE

### Implementation Effort
- Moderate - refactor existing Go code to handle routing/auth
- Keep chrome container as-is (it's already optimized)

---

## Recommendation Summary

| Option | Lighter? | Chrome Still Needed? | Worth It? |
|--------|----------|---------------------|-----------|
| Sprites | Local yes, overall no | Yes (local or laggy cloud) | For cloud-first teams only |
| packnplay | No | Yes | No - wrong use case |
| Leash | No (heavier) | Yes | Enterprise add-on only |
| **Simplify swe-swe** | **Yes** | **Yes** | **Best path forward** |

### Key Insight

**The chrome container is irreducible** - you need CDP + VNC together for agent browser automation with human observation. None of the external solutions eliminate this.

**The real bloat is orchestration** - traefik, auth, vscode-proxy are conveniences that could be consolidated into the main Go server.

### Recommended Path

1. **Short term**: Make code-server optional (flag to disable)
2. **Medium term**: Embed auth + routing in swe-swe-server (eliminate 3 containers)
3. **Long term**: Consider Leash integration as enterprise option
4. **Skip**: Sprites/packnplay don't fit the use case

### Minimal Viable swe-swe

```
┌─ swe-swe (Go server) ────────────────┐
│  WebSocket terminal                  │
│  HTTP routing (stdlib)               │
│  Cookie auth (embedded)              │
│  Static file serving                 │
└──────────────────────────────────────┘
           +
┌─ chrome ─────────────────────────────┐
│  CDP + VNC (irreducible)             │
└──────────────────────────────────────┘

Total: 2 containers (down from 6)
```

This is the lightest architecture that still provides swe-swe's core value: shareable browser-based agent sessions with visual browser automation.

---

*Analysis: 2026-01-12*
