# Comparison: AI Agent Sandboxing Solutions

## Overview

| Aspect | Fly.io Sprites | packnplay | Leash (StrongDM) |
|--------|----------------|-----------|------------------|
| **Type** | Cloud-based disposable VMs | Local Docker wrapper | Policy enforcement + monitoring |
| **Primary Goal** | Persistent dev environments | Container isolation | Access control + audit |
| **License** | Commercial (Fly.io) | MIT | Apache-2.0 |
| **Language** | N/A (service) | Go | Go, TypeScript, Swift |

---

## 1. Fly.io Sprites

**Source:** https://fly.io/blog/code-and-let-live/

**What it is:** Cloud-based "disposable computers" that persist across sessions.

**Key differentiators:**
- **1-2 second boot time** (SSH-like latency)
- **Instant checkpoints** - snapshot/restore in ~1 second (like version control for entire system state)
- **Persistent state** - survives sessions until explicitly destroyed
- **100GB storage**, full root shell, HTTPS URLs via anycast
- **Not Docker-based** - entirely different storage/orchestration stack

**Philosophy:** Argues that ephemeral sandboxes are misaligned with how agents work best. Agents benefit from persistent environments where they can install tools, configure systems, and maintain state.

**Use case:** Small-scale "dev-as-prod" scenarios. Not designed for multi-tenant or million-user deployments.

**Limitation:** No detailed security isolation model published yet.

---

## 2. packnplay

**Source:** https://github.com/obra/packnplay

**What it is:** Lightweight local containerization wrapper for AI coding assistants.

**Key differentiators:**
- **Worktree management** - auto-creates isolated Git worktrees per session
- **Devcontainer support** - 100% Microsoft spec compliant
- **Credential forwarding** - Git, SSH, GPG, npm, AWS (including SSO), macOS Keychain
- **7 AI assistants supported** - Claude, Gemini, Copilot, etc.
- **Smart user detection** - intelligently determines container user

**Security model:**
- Credentials mount **read-only**
- Only whitelisted env vars pass through (`TERM`, `LANG`, `LC_*`)
- `IS_SANDBOX=1` marker for detection
- **No introspection or access control** - isolation only

**Philosophy:** Simple, lightweight isolation without policy enforcement. Explicitly defers to Leash for access control needs.

**Best for:** Developers wanting quick container isolation without complex policy setup.

---

## 3. Leash (StrongDM)

**Source:** https://github.com/strongdm/leash

**What it is:** Comprehensive policy enforcement and monitoring system for AI agents.

**Key differentiators:**
- **Two-container architecture** - agent container + Leash manager container
- **Cedar policy engine** - define/enforce custom security policies in real-time
- **Full syscall monitoring** - captures every filesystem access & network connection
- **MCP observer** - intercepts Model Context Protocol tool calls
- **Web UI** - real-time monitoring at `localhost:18080` with audit trails
- **Telemetry correlation** - links MCP requests to actual system behavior

**Security model:**
- Policies operate on **actual behavior**, not assumptions
- Can enforce based on filesystem, network, and tool-use patterns
- Complete audit trail of agent activities

**Installation:** npm, Homebrew, or pre-built binaries

**Best for:** Organizations needing visibility, audit trails, and granular policy control over agent actions.

---

## Summary: When to Use Each

| Scenario | Best Choice |
|----------|-------------|
| Need persistent cloud dev environment | **Sprites** |
| Quick local isolation, minimal setup | **packnplay** |
| Enterprise policy enforcement + auditing | **Leash** |
| Running untrusted agent code | **Leash** (monitoring) or **Sprites** (isolation) |
| Dev workflow with Git worktrees | **packnplay** |
| Need to understand what agent actually did | **Leash** |

## Complementary Usage

packnplay explicitly mentions Leash as complementary - you could use packnplay for container management and Leash for policy enforcement, or use Sprites for cloud-based persistence when local containers aren't sufficient.

---

*Research compiled: 2026-01-12*
