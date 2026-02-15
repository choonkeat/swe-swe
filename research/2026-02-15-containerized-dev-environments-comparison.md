# Containerized Development Environments Comparison

**Research Date:** 2026-02-15
**Platforms Compared:** swe-swe, Coder, DevPod, Daytona, GitHub Codespaces, Gitpod/Ona

---

## Executive Summary

This research compares six containerized development environment platforms. While all provide isolated, reproducible dev environments, they occupy very different niches:

| Platform | Core Approach | Open Source | Self-Hosted | AI Focus |
|----------|--------------|-------------|-------------|----------|
| **swe-swe** | Container orchestrator for AI CLI tools | Yes (MIT) | Yes (Docker Compose) | AI-first (multi-agent) |
| **Coder** | Self-hosted CDE control plane with Terraform templates | Yes (AGPL-3.0) | Yes (only option) | Agent governance layer |
| **DevPod** | Client-only devcontainer launcher | Yes (MPL-2.0) | N/A (no server) | None built-in |
| **Daytona** | AI sandbox infrastructure (SDK-first) | Yes (AGPL-3.0) | Yes (Docker Compose) | AI-native (SDK/MCP) ([daytona.io/docs/en/mcp](https://www.daytona.io/docs/en/mcp/)) |
| **GitHub Codespaces** | Managed cloud dev environments for GitHub | No (proprietary) | No (SaaS only) | Copilot add-on |
| **Gitpod/Ona** | AI agent orchestration platform | Legacy AGPL; new platform proprietary | Yes (self-hosted runners) | AI-native (agents) |

---

## 1. Core Philosophy & Approach

### swe-swe

**Philosophy:** Be an orchestrator, not an agent. Let developers use their preferred AI CLI tools (Claude Code, Gemini CLI, Codex, Aider, Goose, OpenCode) within a consistent containerized environment with browser-based terminal, session recordings, and team collaboration.

**Key Design Decisions:**
- Metadata stored outside project directory to keep workspaces clean
- Multi-container architecture with Traefik for path-based routing
- Auto-detects installed AI assistants at runtime
- BYOK (Bring Your Own Keys) — no vendor lock-in

### Coder

**Philosophy:** Self-hosted Cloud Development Environment control plane. Organizations maintain full control — code never leaves company infrastructure. Tagline: "Secure environments for developers and their agents" ([coder.com](https://coder.com/)).

**Key Design Decisions:**
- Terraform-based workspace templates — anything Terraform can provision becomes a workspace ([coder.com/docs/about/contributing/templates](https://coder.com/docs/about/contributing/templates))
- WireGuard-encrypted tunnels for all connectivity (Tailscale-derived DERP relay fallback) ([coder.com/docs/admin/infrastructure/architecture](https://coder.com/docs/admin/infrastructure/architecture))
- Control plane (coderd) + provisioners (provisionerd) + workspace agents architecture ([coder.com/docs/admin/infrastructure/architecture](https://coder.com/docs/admin/infrastructure/architecture))
- Since late 2025, pivoted toward "Enterprise AI Development Infrastructure & Governance" ([coder.com/solutions/ai-coding-agents](https://coder.com/solutions/ai-coding-agents))

([github.com/coder/coder](https://github.com/coder/coder) — AGPL-3.0, 10.6k stars)

### DevPod

**Philosophy:** "Codespaces but open-source, client-only and unopinionated" ([devpod.sh/docs/what-is-devpod](https://devpod.sh/docs/what-is-devpod)). No server-side infrastructure required. The devcontainer spec is the API — DevPod is just the glue between your IDE and any machine.

**Key Design Decisions:**
- Client-only architecture — no backend to deploy or maintain ([devpod.sh/docs/how-it-works/overview](https://devpod.sh/docs/how-it-works/overview))
- Provider model abstracts infrastructure (Docker, K8s, AWS, GCP, Azure, SSH) ([devpod.sh/docs/managing-providers/what-are-providers](https://devpod.sh/docs/managing-providers/what-are-providers))
- Follows the devcontainer.json standard — switch between DevPod and Codespaces without config changes
- Created by Loft Labs (now vCluster Labs)

**Maintenance note:** Development activity dropped sharply in April 2025. vCluster Labs paused DevPod Pro commercialization to focus on vCluster. The OSS project is "not abandoned" but active maintenance is minimal. A community fork exists ([github.com/loft-sh/devpod/issues/1915](https://github.com/loft-sh/devpod/issues/1915)).

([github.com/loft-sh/devpod](https://github.com/loft-sh/devpod) — MPL-2.0, 14.7k stars)

### Daytona

**Philosophy:** "Give Every Agent a Computer" ([prnewswire.com — Series A announcement](https://www.prnewswire.com/news-releases/daytona-raises-24m-series-a-to-give-every-agent-a-computer-302680740.html)). Secure, programmable sandbox infrastructure specifically for AI agents to execute code. Originally a Development Environment Manager (founded 2023, creators of Codeanywhere); pivoted to AI runtime infrastructure in February 2025 ([daytona.io/dotfiles/from-dev-environments-to-ai-runtimes](https://www.daytona.io/dotfiles/from-dev-environments-to-ai-runtimes)).

**Key Design Decisions:**
- SDK-first approach (Python, TypeScript, Ruby, Go) ([daytona.io/docs/en/getting-started](https://www.daytona.io/docs/en/getting-started/))
- Stateful sandboxes by default (file changes persist across sessions)
- Warm sandbox pools for sub-90ms startup ([daytona.io/dotfiles/from-dev-environments-to-ai-runtimes](https://www.daytona.io/dotfiles/from-dev-environments-to-ai-runtimes))
- OCI container-based with optional Kata Containers (VM-level) isolation

([github.com/daytonaio/daytona](https://github.com/daytonaio/daytona) — AGPL-3.0)

### GitHub Codespaces

**Philosophy:** Zero-friction cloud development environments, tightly integrated with GitHub. Every codespace is a VM running a Docker container defined by devcontainer.json ([docs.github.com — deep dive](https://docs.github.com/en/codespaces/about-codespaces/deep-dive)).

**Key Design Decisions:**
- Fully managed SaaS on Azure infrastructure
- Each codespace gets its own VM (never co-located for security) ([docs.github.com — security](https://docs.github.com/en/codespaces/reference/security-in-github-codespaces))
- GitHub co-authors the devcontainer spec — first-class support
- Prebuilds via GitHub Actions for fast startup ([docs.github.com — prebuilds](https://docs.github.com/en/codespaces/prebuilding-your-codespaces/about-github-codespaces-prebuilds))

([github.com/features/codespaces](https://github.com/features/codespaces))

### Gitpod / Ona

**Philosophy (original Gitpod):** Ephemeral, automated dev environments. "Dev environments should be like CI pipelines — fresh, reproducible, and automated."

**Philosophy (Ona, post-rebrand Sep 2025):** "Mission control for your personal team of software engineering agents." Environments are now the substrate for AI agents, not the primary product ([ona.com/stories/gitpod-is-now-ona](https://ona.com/stories/gitpod-is-now-ona)). CEO: "IDEs defined the last era. Agents define the next" ([infoq.com](https://www.infoq.com/news/2025/09/gitpod-ona/)).

**Key events:**
- Oct 2024: Launched Gitpod Flex, abandoned Kubernetes after 6 years ([devclass.com](https://devclass.com/2024/11/06/gitpod-discontinues-journey-of-experiments-failures-and-dead-ends-with-kubernetes/))
- Sep 2025: Rebranded to Ona ([theregister.com](https://www.theregister.com/2025/09/03/gitpod_rebrands_as_ona/))
- Oct 2025: Sunset Gitpod Classic pay-as-you-go ([ona.com/stories/gitpod-classic-payg-sunset](https://ona.com/stories/gitpod-classic-payg-sunset))

([ona.com](https://ona.com/), legacy: [github.com/gitpod-io/gitpod](https://github.com/gitpod-io/gitpod))

---

## 2. Architecture

| Aspect | swe-swe | Coder | DevPod | Daytona | Codespaces | Gitpod/Ona |
|--------|---------|-------|--------|---------|------------|------------|
| **Orchestration** | Docker Compose | Terraform + K8s/Docker | Client-side provider model | Custom container orchestrator | Managed VMs (Azure) | Custom (abandoned K8s in 2024) |
| **Workspace type** | Multi-container stack | Configurable (container/VM/pod) | Devcontainer | OCI container sandbox | VM + container | VM + container |
| **State** | PTY sessions + recordings | Terraform state + PostgreSQL | Stateless client | Stateful (persistent FS) | Persistent `/workspaces` dir | Ephemeral by design |
| **Networking** | Traefik reverse proxy | WireGuard tunnels + DERP relay | SSH over vendor tunnel | REST API + SDK | TLS tunnels | Runner-based |
| **Config format** | `swe-swe init` flags | Terraform HCL templates | devcontainer.json | SDK code / Docker images | devcontainer.json | devcontainer.json (was .gitpod.yml) |
| **Database** | None | PostgreSQL | None | Internal | Managed | Internal |

### Execution Model Comparison

```
swe-swe:                              Coder:
┌────────────────────────────┐        ┌──────────────────────────────┐
│  Docker Compose            │        │  coderd (Control Plane)      │
│  ┌────────┐ ┌──────┐      │        │    ↕ PostgreSQL              │
│  │swe-swe │ │chrome│      │        │  provisionerd (Terraform)    │
│  │terminal│ │(CDP) │      │        │    ↓                         │
│  └───┬────┘ └──┬───┘      │        │  ┌──────────────────┐       │
│      └────┬────┘          │        │  │ Workspace Agent   │       │
│        Traefik            │        │  │ (in container/VM) │       │
└────────────────────────────┘        │  └──────────────────┘       │
                                      └──────────────────────────────┘

DevPod:                               Daytona:
┌──────────────┐                      ┌────────────────────────────┐
│  Local CLI   │                      │  Daytona Cloud / Self-host │
│  ┌────────┐  │                      │  ┌──────────────────────┐  │
│  │Provider│──┼──→ Docker/K8s/VM     │  │  Sandbox Pool (warm) │  │
│  └────┬───┘  │                      │  │  ┌────┐┌────┐┌────┐ │  │
│       ↓      │                      │  │  │ S1 ││ S2 ││ S3 │ │  │
│   SSH tunnel │                      │  │  └────┘└────┘└────┘ │  │
│       ↓      │                      │  └──────────────────────┘  │
│   Local IDE  │                      │         ↑ SDK/API/MCP      │
└──────────────┘                      └────────────────────────────┘
```

---

## 3. AI Integration

| Feature | swe-swe | Coder | DevPod | Daytona | Codespaces | Gitpod/Ona |
|---------|---------|-------|--------|---------|------------|------------|
| **AI tools bundled** | Claude, Gemini, Codex, Aider, Goose, OpenCode | None (runs any via templates) | None | None (SDK for agents) | Copilot ([docs.github.com](https://docs.github.com/en/codespaces/reference/using-github-copilot-in-github-codespaces)) | Ona Agents (core product) |
| **Agent governance** | YOLO mode toggle per agent | AI Bridge + Agent Boundaries ([coder.com/solutions/ai-coding-agents](https://coder.com/solutions/ai-coding-agents)) | None | SOC 2, sandboxing | Org policies | Guardrails (RBAC, audit, deny-lists) |
| **MCP support** | Yes (Playwright, agent-chat, whiteboard) | No | No | Yes (MCP server for sandboxes) ([daytona.io/docs/en/mcp](https://www.daytona.io/docs/en/mcp/)) | No | No |
| **AI sandbox** | Per-session container | Per-workspace isolation | Per-workspace container | Per-sandbox isolation (sub-90ms) | Per-codespace VM | Per-agent ephemeral environment |
| **Multi-agent** | Yes (switch agents in same session) | Yes (Tasks: Claude Code, Aider, Q) ([coder.com/docs/ai-coder/tasks](https://coder.com/docs/ai-coder/tasks)) | No | Agent-agnostic SDK | Copilot only | Ona Agents (proprietary) |
| **Debug channel** | Yes (console/network forwarding) | No | No | No | No | No |

**Surprise Finding:** The landscape has bifurcated. Coder, Daytona, and Ona are all pivoting toward "AI agent infrastructure" — but from different angles. Coder adds governance to existing CDE. Daytona provides raw sandbox compute via SDK. Ona makes agents the primary user. swe-swe is the only platform that orchestrates multiple existing AI CLI tools rather than building its own agent framework.

---

## 4. Developer UX

### Interface Types

| Platform | CLI | Web Terminal | Web IDE | VS Code | JetBrains | Cursor/Windsurf |
|----------|-----|-------------|---------|---------|-----------|-----------------|
| **swe-swe** | Yes | Yes (primary) | code-server | Via code-server | No | No |
| **Coder** | Yes | Yes (xterm.js) | code-server | Extension + Remote SSH | Gateway plugin | Via SSH/extension |
| **DevPod** | Yes | No | openvscode-server | Remote SSH | Gateway | Via SSH |
| **Daytona** | Yes | No | No | No | No | No |
| **Codespaces** | gh CLI | Yes | VS Code Web | Extension | Gateway | No |
| **Gitpod/Ona** | Yes | Yes | VS Code Web | Desktop connection | Gateway (beta) | Yes |

### Desktop Integration

| Platform | Desktop App | How it Works |
|----------|-------------|--------------|
| **swe-swe** | No | Browser-based (http://localhost:1977) |
| **Coder** | Yes (macOS/Windows) | Coder Connect creates VPN-like tunnel; workspaces accessible as hostnames ([coder.com](https://coder.com/)) |
| **DevPod** | Yes (Tauri-based, cross-platform) | GUI for workspace/provider management; calls CLI under the hood |
| **Daytona** | No | SDK/API-driven |
| **Codespaces** | No | VS Code extension connects to cloud |
| **Gitpod/Ona** | Yes (local runner option) | Environments can run locally |

---

## 5. Collaboration

| Feature | swe-swe | Coder | DevPod | Daytona | Codespaces | Gitpod/Ona |
|---------|---------|-------|--------|---------|------------|------------|
| **Multi-user sessions** | Yes (real-time terminal sharing) | Shared ports; no real-time pairing built-in | No | No | Live Share (VS Code) | Agent-human handoff |
| **Session recordings** | Yes (terminal recordings with playback) | No | No | No | No | No |
| **Team chat** | Yes (in-session agent chat) | No | No | No | No | No |
| **RBAC** | Password-based | Yes (Premium: custom roles, groups) | No | Yes (API key scoped) | Org policies | Yes (Guardrails) |
| **SSO/OIDC** | No | Yes (any OIDC provider) ([coder.com/docs/admin/infrastructure/architecture](https://coder.com/docs/admin/infrastructure/architecture)) | No | Yes (Dex/Auth0) | GitHub auth | Yes |
| **Audit logging** | No | Yes (Premium) | No | Yes | No | Yes (Guardrails) |

**Surprise Finding:** swe-swe is the only platform with built-in terminal recording and playback, and the only one where multiple humans can watch/interact with the same AI agent session in real-time. This is a unique collaboration model — pairing with an AI agent rather than with another developer.

---

## 6. Pricing & Cost Model

| Platform | Free Tier | Paid Plans | Cost Model |
|----------|-----------|------------|------------|
| **swe-swe** | Fully free | N/A | BYOK (your cloud costs + API keys) |
| **Coder** | Community: free, unlimited | Premium: per-user/year (contact sales) | Self-hosted (your infra costs) ([coder.com/pricing](https://coder.com/pricing)) |
| **DevPod** | Fully free | Pro: paused indefinitely | Your infra costs only |
| **Daytona** | $200 free credits | Pay-per-second ($0.05/vCPU-hr) | Usage-based + self-hosted option ([daytona.io/pricing](https://www.daytona.io/pricing)) |
| **Codespaces** | 120 core-hours/mo + 15 GB storage | $0.18/hr (2-core) to $2.88/hr (32-core) | Per-hour + per-GB ([docs.github.com — billing](https://docs.github.com/billing/managing-billing-for-github-codespaces/about-billing-for-github-codespaces)) |
| **Gitpod/Ona** | 40 OCUs (one-time) | Core: 80-2200 OCUs/mo; Enterprise: custom | Credit-based (OCUs) ([ona.com/pricing](https://ona.com/pricing)) |

### Cost Comparison: 160 hours/month on a 4-core machine

| Platform | Monthly Cost | Notes |
|----------|-------------|-------|
| **swe-swe** | $0 + infra | Free; you provide Docker host |
| **Coder** | $0 + infra (Community) | Free; you provide K8s/cloud VMs |
| **DevPod** | $0 + infra | Free; you provide Docker/cloud |
| **Daytona** | ~$32 compute + memory + storage | Pay-per-second, 4 vCPU ([daytona.io/pricing](https://www.daytona.io/pricing)) |
| **Codespaces** | ~$58 compute + storage | $0.36/hr × 160 hrs ([docs.github.com — billing](https://docs.github.com/billing/managing-billing-for-github-codespaces/about-billing-for-github-codespaces)) |
| **Gitpod/Ona** | Varies by OCU consumption | Credit-based, less predictable |

---

## 7. Deployment & Infrastructure

| Aspect | swe-swe | Coder | DevPod | Daytona | Codespaces | Gitpod/Ona |
|--------|---------|-------|--------|---------|------------|------------|
| **Self-hosted** | Yes (Docker Compose) | Yes (only option) | N/A (client-only) | Yes (Docker Compose) | No | Yes (runners) |
| **Cloud managed** | No | No | No | Yes (multi-region) | Yes (only option) | Yes (multi-tenant) |
| **Air-gapped** | Yes (all bundled in container) | Yes (full support) ([coder.com/docs/install/airgap](https://coder.com/docs/install/airgap)) | Limited (workarounds) | Yes (self-hosted) | No | No |
| **Kubernetes** | No | Yes (Helm, recommended) | Yes (provider) | No | N/A (managed) | No (abandoned K8s) ([devclass.com](https://devclass.com/2024/11/06/gitpod-discontinues-journey-of-experiments-failures-and-dead-ends-with-kubernetes/)) |
| **Setup time** | `swe-swe init && swe-swe up` | Helm install or binary | `devpod up` | Docker Compose or API key | Click "Create Codespace" | Runner setup (~3 min) |

---

## 8. Security

| Feature | swe-swe | Coder | DevPod | Daytona | Codespaces | Gitpod/Ona |
|---------|---------|-------|--------|---------|------------|------------|
| **Workspace isolation** | Docker containers | Container/VM/pod (configurable) | Container/VM (provider-dependent) | Container + optional Kata (VM-level) ([northflank.com](https://northflank.com/blog/daytona-vs-e2b-ai-code-execution-sandboxes)) | Dedicated VM per codespace ([docs.github.com — security](https://docs.github.com/en/codespaces/reference/security-in-github-codespaces)) | OS-level per environment |
| **Encryption** | HTTPS (optional SSL modes) | WireGuard tunnels (always) | Provider-dependent tunnels | At rest + in transit | TLS tunnels | TLS |
| **Secrets management** | Env vars in docker-compose.yml | Template parameters + env vars | Devcontainer env vars | API keys + env vars | Repo-scoped secrets | Env vars |
| **Compliance** | None claimed | SOC 2 (Premium) ([coder.com/pricing](https://coder.com/pricing)) | None | SOC 2, ISO 27001, GDPR ([daytona.io](https://www.daytona.io/)) | GitHub Enterprise compliance | Enterprise guardrails |
| **Enterprise certs** | Auto-detected (NODE_EXTRA_CA_CERTS, SSL_CERT_FILE) | Via templates | Via devcontainer config | N/A | N/A | N/A |

---

## 9. Where Each Platform Thrives

### swe-swe: Best For
- Developers who already use Claude Code, Gemini CLI, or similar tools
- Teams wanting shared, real-time AI agent sessions with recordings
- Corporate environments with SSL/proxy certificate requirements
- Browser automation development (MCP Playwright integration)
- Multi-agent workflows in a single environment

### Coder: Best For
- Enterprises needing self-hosted, air-gapped development environments
- Organizations with strict data sovereignty requirements
- Platform engineering teams managing developer environments at scale (2,000+ users)
- Teams wanting Terraform-based infrastructure-as-code for workspaces ([coder.com/docs/about/contributing/templates](https://coder.com/docs/about/contributing/templates))
- AI agent governance (AI Bridge, Agent Boundaries) ([coder.com/solutions/ai-coding-agents](https://coder.com/solutions/ai-coding-agents))

### DevPod: Best For
- Developers wanting Codespaces-like UX without vendor lock-in
- Teams already using devcontainer.json who want provider flexibility
- Cost-conscious teams (no management overhead — use bare VMs)
- Local-first workflows with optional cloud burst

### Daytona: Best For
- AI agent developers needing programmatic sandbox creation
- Platforms building AI-powered code execution features
- Teams wanting SDK-driven environment management (Python/TS/Go/Ruby) ([daytona.io/docs/en/getting-started](https://www.daytona.io/docs/en/getting-started/))
- Use cases requiring sub-100ms environment startup ([daytona.io/dotfiles/from-dev-environments-to-ai-runtimes](https://www.daytona.io/dotfiles/from-dev-environments-to-ai-runtimes))
- OpenHands users (purpose-built Daytona runtime available)

### GitHub Codespaces: Best For
- Teams fully committed to the GitHub ecosystem
- Organizations wanting zero-ops managed environments
- Projects needing seamless GitHub Actions + Copilot integration ([docs.github.com — Copilot in Codespaces](https://docs.github.com/en/codespaces/reference/using-github-copilot-in-github-codespaces))
- Quick onboarding (new contributors productive in minutes)

### Gitpod/Ona: Best For
- Teams wanting AI agents as primary "developers"
- Organizations exploring autonomous software engineering
- Enterprises needing agent guardrails (RBAC, audit trails, command deny-lists)
- Teams willing to adopt a newer, rapidly evolving platform

---

## 10. Summary Recommendations

### Choose swe-swe if:
You want to keep using your existing AI CLI tools but need a better environment — containerized, browser-accessible, with team visibility into what the AI is doing.

### Choose Coder if:
You need enterprise-grade, self-hosted development environments with Terraform-based templates, SSO/RBAC, and you're evaluating AI agent governance tooling.

### Choose DevPod if:
You want a free, open-source, no-server-needed devcontainer launcher that works with any infrastructure provider and any IDE. Be aware of reduced maintenance.

### Choose Daytona if:
You're building an AI product that needs to execute code in sandboxes programmatically. The SDK-first approach and sub-90ms startup are ideal for agent infrastructure.

### Choose GitHub Codespaces if:
You want a fully managed, zero-ops cloud dev environment deeply integrated with GitHub, and you're comfortable with the per-hour pricing model.

### Choose Gitpod/Ona if:
You want AI agents to be first-class citizens in your development workflow, and you're ready to adopt a platform that is betting its future entirely on autonomous AI engineering.

---

## Sources

### Coder
- [Coder Homepage](https://coder.com/)
- [Coder GitHub](https://github.com/coder/coder)
- [Architecture Docs](https://coder.com/docs/admin/infrastructure/architecture)
- [AI Coding Agents Solutions](https://coder.com/solutions/ai-coding-agents)
- [Coder Tasks Docs](https://coder.com/docs/ai-coder/tasks)
- [Pricing](https://coder.com/pricing)
- [Air-Gapped Deployments](https://coder.com/docs/install/airgap)
- [Templates Docs](https://coder.com/docs/about/contributing/templates)

### DevPod
- [DevPod Homepage](https://devpod.sh/)
- [DevPod GitHub](https://github.com/loft-sh/devpod)
- [Architecture Overview](https://devpod.sh/docs/how-it-works/overview)
- [What is DevPod?](https://devpod.sh/docs/what-is-devpod)
- [Providers Docs](https://devpod.sh/docs/managing-providers/what-are-providers)
- [Maintenance Discussion](https://github.com/loft-sh/devpod/issues/1915)

### Daytona
- [Daytona Homepage](https://www.daytona.io/)
- [Daytona GitHub](https://github.com/daytonaio/daytona)
- [From Dev Environments to AI Runtimes](https://www.daytona.io/dotfiles/from-dev-environments-to-ai-runtimes)
- [Series A Announcement ($24M, Feb 2026)](https://www.prnewswire.com/news-releases/daytona-raises-24m-series-a-to-give-every-agent-a-computer-302680740.html)
- [SDK Documentation](https://www.daytona.io/docs/en/getting-started/)
- [MCP Server Docs](https://www.daytona.io/docs/en/mcp/)
- [Pricing](https://www.daytona.io/pricing)
- [Daytona vs E2B (Northflank)](https://northflank.com/blog/daytona-vs-e2b-ai-code-execution-sandboxes)

### GitHub Codespaces
- [Deep Dive into Codespaces](https://docs.github.com/en/codespaces/about-codespaces/deep-dive)
- [Codespaces Billing](https://docs.github.com/billing/managing-billing-for-github-codespaces/about-billing-for-github-codespaces)
- [Codespaces Prebuilds](https://docs.github.com/en/codespaces/prebuilding-your-codespaces/about-github-codespaces-prebuilds)
- [Security in Codespaces](https://docs.github.com/en/codespaces/reference/security-in-github-codespaces)
- [Using Copilot in Codespaces](https://docs.github.com/en/codespaces/reference/using-github-copilot-in-github-codespaces)
- [Copilot Coding Agent Announcement](https://github.com/newsroom/press-releases/coding-agent-for-github-copilot)

### Gitpod / Ona
- [Gitpod is now Ona](https://ona.com/stories/gitpod-is-now-ona)
- [Gitpod Rebrands as Ona (The Register)](https://www.theregister.com/2025/09/03/gitpod_rebrands_as_ona/)
- [Gitpod Rebrands to Ona (InfoQ)](https://www.infoq.com/news/2025/09/gitpod-ona/)
- [Gitpod Classic PAYG Sunset](https://ona.com/stories/gitpod-classic-payg-sunset)
- [Ona Pricing](https://ona.com/pricing)
- [Gitpod Discontinues Kubernetes (DevClass)](https://devclass.com/2024/11/06/gitpod-discontinues-journey-of-experiments-failures-and-dead-ends-with-kubernetes/)
- [Legacy GitHub Repo](https://github.com/gitpod-io/gitpod)

### swe-swe
- Local codebase — authoritative, verified by direct inspection.

---

*Research conducted by Claude Code on 2026-02-15*
