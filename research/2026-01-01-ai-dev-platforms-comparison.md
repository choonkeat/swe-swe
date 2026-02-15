# AI-Assisted Development Platforms Comparison

**Research Date:** 2026-01-01, **Updated:** 2026-02-15
**Platforms Compared:** swe-swe, OpenHands, Replit, OpenCode

---

## Executive Summary

This research compares four AI-assisted development platforms with fundamentally different approaches:

| Platform | Core Approach | Open Source | Primary Interface |
|----------|--------------|-------------|-------------------|
| **swe-swe** | Container orchestrator for existing AI CLI tools | Yes (MIT) | Terminal + Web UI |
| **OpenHands** | Full-stack AI agent platform with native agent framework | Yes (MIT*) | CLI, GUI, Cloud |
| **Replit** | Cloud IDE with integrated AI agent | No (Proprietary) | Web IDE + Mobile |
| **OpenCode** | Terminal-based AI coding assistant | Yes (MIT) | TUI + Desktop + Web + VS Code |

*OpenHands enterprise directory has separate licensing

### What Changed Since January 2026

| Platform | Key Developments (Jan–Feb 2026) |
|----------|-------------------------------|
| **swe-swe** | Agent Chat MCP, mobile support, light themes, npm distribution, DigitalOcean deploy, OpenCode agent, per-session preview ports. Version 0.1.2. |
| **OpenHands** | OpenHands Index (5-category benchmark), v1.3.0, Daytona runtime partnership, Azure DevOps integration, MiniMax M2.5 evaluation. 67.8k stars. |
| **Replit** | Agent 3 (10x autonomy, 3hr sessions), $9B valuation ($400M round), pricing overhaul (Teams sunset, Pro $100/mo), mobile app builder, Google Cloud + Microsoft partnerships. |
| **OpenCode** | Rewritten from Go to TypeScript/Bun, repo moved to anomalyco/opencode, Tauri desktop app, multi-agent orchestration, 105k stars. Anthropic blocked client spoofing (Jan 9). |

---

## 1. Core Philosophy & Approach

### swe-swe

**Philosophy:** Be an orchestrator, not an agent. Let developers use their preferred AI CLI tools (Claude Code, Gemini CLI, Codex, Aider, Goose, OpenCode) within a consistent containerized environment.

**Key Design Decisions:**
- Metadata stored outside project directory to keep workspaces clean
- Multi-container architecture with Traefik for routing
- Auto-detects installed assistants at runtime
- Four MCP servers included by default: agent-chat, playwright, preview, whiteboard

**Source Assessment:** Local codebase - authoritative, verified by direct inspection.

**Jan–Feb 2026 Updates:**
- **Agent Chat MCP** — In-session chat between humans and AI agents with sidecar process management, auto-activating chat tab, and YOLO mode toggle
- **Mobile & touch support** — Mobile keyboard bar (Ctrl, Tab, Esc), iOS momentum scrolling, visual viewport handling for on-screen keyboards
- **Light/dark/system themes** — VS Code Light+ ANSI palette with WCAG AA contrast, cookie persistence (no FOUC)
- **npm distribution** — Install via `npx swe-swe` or curl installer
- **Per-session preview ports** — Each session gets unique app port with isolated reverse proxy and `PORT` env var injection
- **DigitalOcean deployment** — Packer template with first-boot scripts, password prompt, SSL, hardening
- **OpenCode agent** — Sixth supported agent alongside Claude, Gemini, Codex, Aider, Goose
- **MCP server upsert** — `.mcp.json` preserves user-defined servers during re-init
- **Setup wizard** — Custom env vars step, copy home paths, merge strategy flags
- **Worktree-based isolation** — Each session on separate git worktree for parallel work

### OpenHands

**Philosophy:** Build a complete AI agent platform with its own reasoning-action loop, tool system, and execution model. The CodeActAgent "consolidates LLM agents' actions into a unified code action space" ([docs.openhands.dev/usage/agents](https://docs.openhands.dev/usage/agents)).

**Key Design Decisions:**
- Software Agent SDK with event-sourced state model and deterministic replay ([docs.openhands.dev/sdk](https://docs.openhands.dev/sdk))
- Type-safe Action-Observation pattern with Pydantic models ([docs.openhands.dev/sdk/arch/tool-system](https://docs.openhands.dev/sdk/arch/tool-system))
- Four deployment options: SDK, CLI, Local GUI, Cloud ([github.com/OpenHands/OpenHands](https://github.com/OpenHands/OpenHands))

**Source Assessment:** Official documentation and GitHub - authoritative, well-maintained.

**Jan–Feb 2026 Updates:**
- **OpenHands Index** (Jan 28) — Broad-coverage benchmark evaluating LLMs across 5 SE task categories: issue resolution, greenfield development, frontend, testing, information gathering. Tested 9 models; Claude 4.5 Opus won overall ([openhands.dev/blog/openhands-index](https://openhands.dev/blog/openhands-index))
- **v1.3.0** (Feb 2) — CORS support for remote browser access, host networking mode, display of agent thought content
- **MiniMax M2.5 evaluation** (Feb 11) — First open model exceeding Claude Sonnet; 13x cheaper than Claude Opus, ranked 4th on OpenHands Index
- **Daytona partnership** — Elastic cloud sandboxes with zero-trust security replacing default Docker runtime
- **Azure DevOps integration** — Alongside existing GitHub, GitLab, Bitbucket, Forgejo support
- **Multi-SWE-Bench** — #1 across 8 programming languages

### Replit

**Philosophy:** Zero-setup cloud development with AI deeply integrated. "The fastest way to go from idea to app... no installation or setup required" ([docs.replit.com](https://docs.replit.com/)).

**Key Design Decisions:**
- Browser-first + mobile: entire development environment runs in the cloud
- Proprietary AI agent with credit-based pricing
- Integrated deployment, database, and hosting

**Source Assessment:** Official documentation and blog posts - authoritative.

**Jan–Feb 2026 Updates:**
- **$9B valuation** — $400M round led by Georgian; 3x increase from $3B in Sep 2025. $240M revenue in 2025, targeting $1B in 2026 ([intellectia.ai](https://intellectia.ai/news/stock/replit-secures-400m-funding-valuation-reaches-9b))
- **Agent 3** — 10x more autonomous than Agent 2, 3+ hour continuous sessions, self-healing testing loop, can build other agents ([blog.replit.com/introducing-agent-3](https://blog.replit.com/introducing-agent-3-our-most-autonomous-agent-yet))
- **Pricing overhaul** (effective Feb 20) — Teams plan sunset; new Pro plan at $100/mo (up to 15 builders); Core $20/mo now includes collaboration for 5 people ([blog.replit.com/pro-plan](https://blog.replit.com/pro-plan))
- **New Replit Assistant** — "Agent takes you 0 to 1, Assistant takes you 1 to 10" for refining existing projects
- **Mobile app builder** — React Native/Expo; idea to published iOS/Android app via natural language; QR code preview on physical device
- **Google Cloud expanded** — Available on Google Cloud Marketplace with enterprise co-sell
- **Microsoft partnership** — Available on Azure Marketplace
- **Shell AI** — `replit ai` command for LLM interaction directly in terminal
- **Gemini CLI extension** — Create and interact with projects via Gemini CLI

### OpenCode

**Philosophy:** Privacy-first terminal AI assistant with maximum model flexibility. "Does not store code or context data, enabling use in sensitive environments" ([opencode.ai](https://opencode.ai/)).

**Key Design Decisions:**
- **Rewritten from Go to TypeScript on Bun** (the original Go/Bubble Tea codebase was abandoned; Crush retains it)
- Multi-interface architecture: TUI, desktop (Tauri), VS Code extension, web app
- Session persistence in SQLite
- Multi-agent system with delegation mechanisms

**Source Assessment:** GitHub ([anomalyco/opencode](https://github.com/anomalyco/opencode)) and website - authoritative. The repository moved from `sst/opencode` to `anomalyco/opencode`. [Crush](https://github.com/charmbracelet/crush) (Charm License, proprietary) retains the original Go/Bubble Tea codebase.

**Jan–Feb 2026 Updates:**
- **TypeScript rewrite** — Complete rewrite on Bun runtime; original Go codebase abandoned. Dax rewrote to leverage the Vercel AI SDK (TypeScript-based multi-provider LLM integration), enable easier plugin development, and escape Go TUI performance issues at scale ([sst/opencode#2143](https://github.com/sst/opencode/issues/2143)). Original author joined Charm to work on Crush
- **105k GitHub stars** — Up from 48.9k in Jan 2026 (+115%)
- **Anthropic client spoofing blocked** (Jan 9) — Anthropic deployed client fingerprinting to block OpenCode and similar tools from accessing Claude at consumer subscription rates. Users must now use metered API access ([venturebeat.com](https://venturebeat.com/technology/anthropic-cracks-down-on-unauthorized-claude-usage-by-third-party-harnesses))
- **Tauri desktop app** — SolidJS + Tauri (not Electron); multi-project workspace, drag-drop reordering, file tabs, integrated terminal, auto-updates
- **Multi-agent system** — "Build" and "Plan" primary agents, "General" and "Explore" subagents, three delegation mechanisms (`call_omo_agent`, `delegate-task`, task management)
- **v1.2.2** (Feb 14) — SQLite database migration (flat files → single DB), PartDelta streaming (incremental text changes), adaptive reasoning for Claude Opus 4.6
- **2.5M monthly active users** (up from ~650k)
- **Crush** at ~20k stars (up from 12k), v0.42.0, added MiniMax and Z.ai providers

---

## 2. AI Model Flexibility

| Platform | Model Support | Vendor Lock-in Risk |
|----------|--------------|---------------------|
| **swe-swe** | Delegates to underlying CLI tools (Claude, GPT via Codex, Gemini, OpenCode, any via Aider) | **Low** - switch by changing `--agents` flag |
| **OpenHands** | LiteLLM integration: Anthropic, OpenAI, Google, Azure, MiniMax, local models. Native tool calling for Gemini 3 Pro. | **Low** - provider-agnostic LLM interface |
| **Replit** | Proprietary agent; BYO OpenAI key option; Gemini CLI extension | **Medium** - more key options now, but agent itself remains opaque |
| **OpenCode** | 75+ providers via Models.dev, Claude Opus 4.6 adaptive reasoning, local models via Ollama | **Low** - most flexible model choice (but Anthropic blocked consumer-rate spoofing) |

**Update:** Replit's model opacity has slightly improved with BYO OpenAI key support and Gemini CLI integration, but the core Agent 3 model remains proprietary. OpenCode's model flexibility was disrupted by Anthropic's Jan 9 spoofing block, forcing users to metered API pricing for Claude models.

---

## 3. Developer UX

### Interface Types

| Platform | CLI | GUI | Web IDE | VS Code | Mobile | Desktop App |
|----------|-----|-----|---------|---------|--------|-------------|
| **swe-swe** | Yes (init/up/down) | Web terminal + Agent Chat | Embedded code-server | Via code-server | Yes (touch keyboard, responsive) | No |
| **OpenHands** | Yes | Local React app | Cloud version | No native | No | No |
| **Replit** | `replit ai` shell | No | Primary interface | No | Yes (iOS/Android app) | No |
| **OpenCode** | Primary | TUI (TypeScript/Bun) | Yes (`opencode web`) | Yes (extension, beta) | No | Yes (Tauri, beta) |

### Real-time Visibility

**swe-swe** provides exceptional visibility:
- Multi-client WebSocket terminal with VT100 emulation
- Browser automation viewable via screencast at `/chrome` path
- Agent Chat tab for real-time human-agent communication via MCP
- Per-session preview ports with isolated debug consoles
- YOLO mode toggle for auto-approval workflows

**OpenHands** offers confirmation modes with task tracker:
- Direct mode: Executes immediately (development environments)
- Confirmation mode: Stores as pending, awaits user approval (production)
- TaskTrackerTool for agent progress monitoring

**Replit** now provides Agent 3 with self-healing:
- Agent 3 tests apps in live browser and auto-fixes issues
- Fast Mode, Design Mode, Plan Mode, Build Mode
- New Assistant for iterative refinement of existing projects

**OpenCode** has multi-agent with delegation:
- Parent/child session navigation via keybinds
- Build, Plan, General, and Explore agents with three delegation mechanisms
- 50+ keyboard shortcuts, customizable command palette

---

## 4. Team Composition & Collaboration

| Feature | swe-swe | OpenHands | Replit | OpenCode |
|---------|---------|-----------|--------|----------|
| **Multi-user sessions** | Yes, real-time | Cloud only | Yes | Shareable sessions |
| **Role-based access** | Password-based (ForwardAuth) | Cloud/Enterprise | Core (5 people), Pro (15 builders) | No |
| **Async collaboration** | No | Via integrations | Yes | Session links |
| **Enterprise SSO** | No | Enterprise tier (SAML/SSO) | Enterprise tier | No |

**Update:** Replit's collaboration is now available at the Core tier ($20/mo) for up to 5 people, previously Teams-only. swe-swe added ForwardAuth-based unified authentication with cookie security.

---

## 5. Software Development Constraints

### Language & Framework Support

| Platform | Languages | Frameworks | Constraints |
|----------|-----------|------------|-------------|
| **swe-swe** | Any (Docker-based) | Any | Limited by container resources |
| **OpenHands** | Multi-language (8 languages on Multi-SWE-Bench) | Any | CodeActAgent optimized for Python/bash |
| **Replit** | 50+ languages + React Native/Expo for mobile | Full-stack web + mobile | Browser-based limitations |
| **OpenCode** | Any (LSP-based) | Any | Anthropic consumer spoofing blocked |

### Project Types Best Suited

**swe-swe:**
- Complex multi-service applications requiring browser automation
- Projects needing specific AI tool combinations (6 agents supported)
- Enterprise environments with SSL certificate requirements
- Cloud deployment on DigitalOcean with Packer templates

**OpenHands:**
- Autonomous task execution (top scores on OpenHands Index and Multi-SWE-Bench)
- Projects requiring GitHub/GitLab/Jira/Azure DevOps integration
- Building custom AI agents with Software Agent SDK
- Cost-sensitive teams wanting open-weight models (MiniMax M2.5 at 13x cheaper)

**Replit:**
- Rapid prototyping and MVPs (Agent 3 builds complete apps in 3+ hours autonomously)
- Mobile app development (React Native/Expo, QR code preview)
- Non-technical founders and solo builders (Race to Revenue program)
- Enterprise "vibe coding" via Google Cloud / Azure Marketplace

**OpenCode:**
- Privacy-sensitive codebases
- Projects requiring specific LLM models (75+ providers)
- Teams wanting multi-agent orchestration (Build, Plan, General, Explore agents)
- Desktop-first development (Tauri app)

### Deployment Targets

| Platform | Local | Docker | Cloud Deploy | Self-hosted |
|----------|-------|--------|--------------|-------------|
| **swe-swe** | Via Docker | Native | DigitalOcean (Packer) | Yes |
| **OpenHands** | Yes | Yes (+ Daytona) | OpenHands Cloud | Enterprise (K8s) |
| **Replit** | No | No | One-click + mobile | No |
| **OpenCode** | Yes | Possible | No | Yes |

---

## 6. Pricing & Cost Model

| Platform | Free Tier | Paid Plans | Cost Model |
|----------|-----------|------------|------------|
| **swe-swe** | Fully free | N/A | BYOK (Bring Your Own Keys) |
| **OpenHands** | CLI + Local free; Cloud: 10 conversations/day | Growth $500/mo (unlimited users), Enterprise custom | LLM at-cost (no markup) + Cloud compute |
| **Replit** | Limited (private apps now included) | Core $20/mo (5 collaborators), Pro $100/mo (15 builders) | Credit-based with volume discounts |
| **OpenCode** | Fully free | N/A | BYOK (consumer Claude access blocked by Anthropic) |

**Update:** Replit's pricing has been significantly restructured (effective Feb 20, 2026). Teams plan sunset; new Pro plan at $100/mo replaces it with tiered credits and volume discounts. Core plan gained collaboration for 5 people. OpenHands formalized four pricing tiers with Growth at $500/mo offering unlimited users and centralized billing. OpenCode lost its de facto cheap Claude access when Anthropic blocked consumer-rate client spoofing.

---

## 7. Where Each Platform Thrives

### swe-swe: Best For

**Team Profiles:**
- Solo developers with existing AI CLI tool preferences
- Small teams needing shared development environments
- Enterprises with corporate proxy/SSL requirements
- Mobile-first developers who want terminal access from any device

**Environments:**
- Local development with Docker
- Air-gapped environments (all tools bundled in container)
- Corporate networks requiring certificate injection
- Cloud VMs (DigitalOcean one-click deploy)

**Use Cases:**
- Browser automation development (MCP Playwright integration)
- Multi-agent workflows (switch between Claude, Gemini, Codex, OpenCode, Aider, Goose)
- Pair programming with real-time terminal sharing and Agent Chat
- Worktree-based parallel sessions for tackling multiple issues simultaneously

**Why:** swe-swe doesn't try to replace your AI tools - it makes them better by providing consistent infrastructure. The container isolation means experiments can't break your host system, and the web-based terminal with mobile support means you can work from any device.

### OpenHands: Best For

**Team Profiles:**
- AI researchers building custom agents via Software Agent SDK
- Teams wanting autonomous task execution with task tracking
- Organizations with Jira/Slack/GitHub/Azure DevOps workflow integrations
- Cost-sensitive teams leveraging open-weight models (MiniMax M2.5)

**Environments:**
- CI/CD pipelines (headless agent execution)
- Cloud-native deployments (Kubernetes Enterprise, Daytona sandboxes)
- Research environments needing reproducibility

**Use Cases:**
- Automated issue resolution across GitHub, GitLab, Bitbucket, Forgejo, Azure DevOps
- Large-scale code migrations
- Multi-language SE tasks (8 languages on Multi-SWE-Bench)
- Benchmarking LLMs with the OpenHands Index

**Why:** OpenHands' OpenHands Index demonstrates comprehensive evaluation across 5 SE task categories. The Software Agent SDK with event-sourced state enables building specialized agents. The Daytona partnership provides elastic, secure cloud sandboxes. Open-weight model support (MiniMax M2.5) offers a fully open, cost-effective stack.

### Replit: Best For

**Team Profiles:**
- Students and educators
- Non-technical founders building MVPs and mobile apps
- Solo builders targeting revenue (Race to Revenue program)
- Enterprise teams doing "vibe coding" via cloud marketplaces

**Environments:**
- Chromebook/tablet/phone development (iOS/Android app)
- Shared computer labs
- Environments where local setup is impossible

**Use Cases:**
- Rapid prototyping from natural language (Agent 3 builds complete apps in 3+ hours)
- Mobile app development with QR code preview
- Learning to code with AI assistance (Replit Learn)
- Enterprise deployment via Google Cloud / Azure Marketplace

**Why:** Zero setup friction remains Replit's killer feature, now extended to mobile app development. Agent 3's self-healing testing loop and 3+ hour autonomy enable building complete applications from a single prompt. The $9B valuation and major cloud partnerships signal enterprise adoption. The trade-off is higher costs at scale and less control.

### OpenCode: Best For

**Team Profiles:**
- Privacy-conscious developers
- Terminal and desktop power users (TUI + Tauri app)
- Teams requiring specific LLM providers
- Developers wanting multi-agent orchestration

**Environments:**
- Secure/classified environments
- Companies with strict data policies
- Local-only development requirements

**Use Cases:**
- Working with proprietary codebases
- Projects requiring local LLM inference (Ollama)
- Multi-agent workflows (Build, Plan, General, Explore agents)
- Desktop-first development with Tauri app

**Why:** OpenCode's explosive growth (48.9k → 105k stars, 2.5M MAU) validates strong demand for open-source AI coding tools. The TypeScript rewrite on Bun modernized the stack. Multi-agent orchestration with delegation is a differentiator. However, the Anthropic spoofing block forces users to metered API pricing for Claude models.

---

## 8. Architectural Comparison

### Execution Model

```
swe-swe:
┌──────────────────────────────────────────────┐
│  Host Machine                                │
│  ┌────────────────────────────────────────┐  │
│  │  Docker Compose                        │  │
│  │  ┌──────────┐ ┌────────┐ ┌──────────┐  │  │
│  │  │swe-swe   │ │code-   │ │chrome    │  │  │
│  │  │(Claude,  │ │server  │ │(CDP +    │  │  │
│  │  │Gemini,   │ │(VS     │ │screencast│  │  │
│  │  │Codex,    │ │Code)   │ │viewer)   │  │  │
│  │  │OpenCode, │ │        │ │          │  │  │
│  │  │Aider,    │ │        │ │          │  │  │
│  │  │Goose)    │ │        │ │          │  │  │
│  │  └──────────┘ └────────┘ └──────────┘  │  │
│  │  MCP: agent-chat, playwright,          │  │
│  │       preview, whiteboard              │  │
│  │       ↑           ↑          ↑         │  │
│  │       └───────────┴──────────┘         │  │
│  │         Traefik + ForwardAuth          │  │
│  └────────────────────────────────────────┘  │
└──────────────────────────────────────────────┘

OpenHands:
┌──────────────────────────────────────────────┐
│  OpenHands Runtime (Docker/Daytona/Local)    │
│  ┌────────────────────────────────────────┐  │
│  │     Software Agent SDK (v1.3.0)        │  │
│  │  ┌─────────┐   ┌────────────────┐      │  │
│  │  │   LLM   │──▶│  Tool System   │      │  │
│  │  │Interface│   │(Action→Observe)│      │  │
│  │  │(LiteLLM)│   │+ MCP + Tasks   │      │  │
│  │  └─────────┘   └────────────────┘      │  │
│  │       ▲              │                 │  │
│  │       └──────────────┘                 │  │
│  │    Event-sourced state + condenser     │  │
│  └────────────────────────────────────────┘  │
└──────────────────────────────────────────────┘
```

### Key Architectural Differences

| Aspect | swe-swe | OpenHands | Replit | OpenCode |
|--------|---------|-----------|--------|----------|
| **Agent logic** | External (6 AI tools) | Internal (CodeActAgent) | Internal (Agent 3) | Internal (Build, Plan, General, Explore) |
| **Tool integration** | MCP (4 servers) | Native Action-Observation + MCP | Proprietary | MCP-extensible |
| **State management** | PTY session + worktrees | Event-sourced with condenser | Proprietary | SQLite sessions |
| **Sandboxing** | Docker containers | Docker/Daytona/Local | Cloud VMs | Local process |
| **Runtime** | Go (server) | Python (agent SDK) | Proprietary | TypeScript/Bun |

---

## 9. Summary Recommendations

### Choose swe-swe if:
- You already use Claude Code, Gemini CLI, or similar tools
- You need browser automation with AI (MCP Playwright)
- Your team wants to share a development environment with Agent Chat
- You have corporate SSL/proxy requirements
- You want parallel worktree-based sessions
- You need mobile terminal access

### Choose OpenHands if:
- You want autonomous AI task execution with top benchmark scores
- You're building custom AI agents with the Software Agent SDK
- You need cloud-scale agent deployment (Daytona, Kubernetes)
- Jira/Slack/GitHub/Azure DevOps integration is important
- You want cost-effective open-weight models (MiniMax M2.5)

### Choose Replit if:
- Zero setup is more important than control
- You're building mobile apps from natural language
- You need 3+ hours of autonomous agent work (Agent 3)
- Enterprise procurement via Google Cloud / Azure Marketplace matters
- You're a solo builder targeting first revenue

### Choose OpenCode if:
- Privacy/security is paramount
- You need maximum LLM flexibility (75+ providers)
- You want multi-agent orchestration (Build, Plan, General, Explore)
- You prefer a native desktop app (Tauri) or terminal workflows
- You want the most popular open-source AI coding tool (105k stars)

---

## 10. Market Landscape (Feb 2026)

The AI coding tools market has seen explosive growth and consolidation:

| Platform | GitHub Stars / Users | Valuation / Funding |
|----------|---------------------|---------------------|
| **swe-swe** | Open source (MIT) | Self-funded, open source |
| **OpenHands** | 67.8k stars | $18.8M Series A (Nov 2025) |
| **Replit** | 40M+ users, 150k+ paying | $9B valuation ($400M round, Feb 2026) |
| **OpenCode** | 105k stars, 2.5M MAU | Backed by Anomaly (SST) |
| **Cursor** | — | $29.3B valuation (late 2025) |
| **Lovable** | — | $6.6B valuation |

Notable trends:
- **Anthropic's spoofing crackdown** (Jan 9) disrupted third-party tools relying on consumer Claude pricing, forcing API-rate access
- **Open-weight models catching up**: MiniMax M2.5 (230B params, 10B active) ranked 4th on OpenHands Index while being 13x cheaper than Claude Opus
- **Enterprise adoption accelerating**: Replit on Google Cloud + Azure Marketplace; OpenHands at AMD, Apple, Google, Amazon, Netflix
- **Mobile-first development**: Replit's mobile app builder and swe-swe's touch keyboard support signal a shift toward phone/tablet development
- **"Vibe coding" mainstreaming**: Google Cloud and Microsoft partnering with Replit specifically for enterprise "vibe coding" use cases

---

## Sources

### Web Sources (Updated Feb 2026)
- [OpenHands GitHub](https://github.com/OpenHands/OpenHands) - 67.8k stars
- [OpenHands Docs](https://docs.openhands.dev/) - Official documentation
- [OpenHands Blog - OpenHands Index](https://openhands.dev/blog/openhands-index) - 5-category benchmark (Jan 28, 2026)
- [OpenHands Blog - MiniMax M2.5](https://openhands.dev/blog/minimax-m2-5-open-weights-models-catch-up-to-claude) - Open model evaluation (Feb 11, 2026)
- [OpenHands Pricing](https://openhands.dev/pricing) - Four pricing tiers
- [OpenHands Releases](https://github.com/OpenHands/OpenHands/releases) - v1.2.0 through v1.3.0
- [OpenHands + Daytona](https://openhands.daytona.io/) - Elastic runtime partnership
- [Software Agent SDK](https://github.com/OpenHands/software-agent-sdk) - Agent development framework
- [Replit Blog - Agent 3](https://blog.replit.com/introducing-agent-3-our-most-autonomous-agent-yet) - 10x autonomy agent
- [Replit Blog - Pro Plan](https://blog.replit.com/pro-plan) - Pricing restructure (effective Feb 20, 2026)
- [Replit Blog - New Assistant](https://blog.replit.com/new-ai-assistant-announcement) - Iterative refinement tool
- [Replit Mobile Apps](https://replit.com/mobile-apps) - React Native/Expo mobile builder
- [Replit $9B Valuation](https://intellectia.ai/news/stock/replit-secures-400m-funding-valuation-reaches-9b) - $400M funding round
- [Google Cloud + Replit](https://cloud.google.com/blog/products/ai-machine-learning/bringing-vibe-coding-to-the-enterprise-with-replit) - Enterprise partnership
- [OpenCode GitHub](https://github.com/anomalyco/opencode) - Source repository (~105k stars)
- [OpenCode Website](https://opencode.ai/) - Official website and changelog
- [OpenCode v1.2.0 Release](https://github.com/anomalyco/opencode/releases) - SQLite migration, PartDelta
- [Anthropic Blocks Spoofing](https://venturebeat.com/technology/anthropic-cracks-down-on-unauthorized-claude-usage-by-third-party-harnesses) - Client fingerprinting (Jan 9, 2026)
- [Crush GitHub](https://github.com/charmbracelet/crush) - Go fork (~20k stars, Charm License)
- [OpenHands Series A](https://www.businesswire.com/news/home/20251118768131/en/) - $18.8M funding (Nov 2025)

### Local File Sources
- Local codebase inspection (1043 commits since Jan 1, 2026)
- `git log --since="2026-01-01"` — Feature commits, architectural changes
- docs/ directory — ADRs, changelogs, deployment guides

---

*Research conducted by Claude Code on 2026-01-01, updated 2026-02-15*
