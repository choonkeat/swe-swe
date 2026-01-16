# AI-Assisted Development Platforms Comparison

**Research Date:** 2026-01-01
**Platforms Compared:** swe-swe, OpenHands, Replit, OpenCode

---

## Executive Summary

This research compares four AI-assisted development platforms with fundamentally different approaches:

| Platform | Core Approach | Open Source | Primary Interface |
|----------|--------------|-------------|-------------------|
| **swe-swe** | Container orchestrator for existing AI CLI tools | Yes (MIT) | Terminal + Web UI |
| **OpenHands** | Full-stack AI agent platform with native agent framework | Yes (MIT*) | CLI, GUI, Cloud |
| **Replit** | Cloud IDE with integrated AI agent | No (Proprietary) | Web IDE |
| **OpenCode** | Terminal-based AI coding assistant | Yes (MIT) | Terminal TUI |

*OpenHands enterprise directory has separate licensing

---

## 1. Core Philosophy & Approach

### swe-swe

**Philosophy:** Be an orchestrator, not an agent. Let developers use their preferred AI CLI tools (Claude Code, Gemini CLI, Codex, Aider, Goose) within a consistent containerized environment.

**Key Design Decisions:**
- Metadata stored outside project directory to keep workspaces clean ([README.md:250-254](README.md:250))
- Multi-container architecture with Traefik for routing ([README.md:287-293](README.md:287))
- Auto-detects installed assistants at runtime ([cmd/swe-swe-server/main.go:488-515](cmd/swe-swe-server/main.go:488))

**Source Assessment:** Local codebase - authoritative, verified by direct inspection.

### OpenHands

**Philosophy:** Build a complete AI agent platform with its own reasoning-action loop, tool system, and execution model. The CodeActAgent "consolidates LLM agents' actions into a unified code action space" ([docs.openhands.dev/usage/agents](https://docs.openhands.dev/usage/agents)).

**Key Design Decisions:**
- Agent implements stateless, event-driven architecture ([docs.openhands.dev/sdk/arch/agent](https://docs.openhands.dev/sdk/arch/agent))
- Type-safe Action-Observation pattern with Pydantic models ([docs.openhands.dev/sdk/arch/tool-system](https://docs.openhands.dev/sdk/arch/tool-system))
- Four deployment options: SDK, CLI, Local GUI, Cloud ([github.com/OpenHands/OpenHands](https://github.com/OpenHands/OpenHands))

**Source Assessment:** Official documentation and GitHub - authoritative, well-maintained.

### Replit

**Philosophy:** Zero-setup cloud development with AI deeply integrated. "The fastest way to go from idea to app... no installation or setup required" ([docs.replit.com](https://docs.replit.com/)).

**Key Design Decisions:**
- Browser-first: entire development environment runs in the cloud
- Proprietary AI agent with effort-based pricing
- Integrated deployment, database, and hosting

**Source Assessment:** Official documentation - authoritative for features, but pricing details scattered across multiple sources.

### OpenCode

**Philosophy:** Privacy-first terminal AI assistant with maximum model flexibility. "Does not store code or context data, enabling use in sensitive environments" ([opencode.ai](https://opencode.ai/)).

**Key Design Decisions:**
- Go-based TUI using Bubble Tea framework ([github.com/opencode-ai/opencode](https://github.com/opencode-ai/opencode))
- Session persistence in SQLite
- LSP integration for code diagnostics

**Source Assessment:** GitHub and website - authoritative. Note: Project has transitioned to "Crush" maintained by Charm team.

---

## 2. AI Model Flexibility

| Platform | Model Support | Vendor Lock-in Risk |
|----------|--------------|---------------------|
| **swe-swe** | Delegates to underlying CLI tools (Claude, GPT via Codex, Gemini, any via Aider) | **Low** - switch by changing `--agents` flag |
| **OpenHands** | LiteLLM integration: Anthropic, OpenAI, Google, Azure, local models ([docs.openhands.dev/usage/run-openhands/local-setup](https://docs.openhands.dev/usage/run-openhands/local-setup)) | **Low** - provider-agnostic LLM interface |
| **Replit** | Proprietary agent; "High power model increases Agent's intelligence" ([superblocks.com/blog/replit-pricing](https://www.superblocks.com/blog/replit-pricing)) | **High** - model selection is opaque |
| **OpenCode** | 75+ providers via Models.dev, plus Claude Pro auth, local models ([opencode.ai](https://opencode.ai/)) | **Low** - most flexible model choice |

**Surprise Finding:** Replit's AI model selection is the most opaque. Users can enable "high power mode" but cannot specify which model powers it. This contrasts sharply with OpenHands and OpenCode which expose full model configurability.

---

## 3. Developer UX

### Interface Types

| Platform | CLI | GUI | Web IDE | VS Code Integration |
|----------|-----|-----|---------|---------------------|
| **swe-swe** | Yes (init/up/down) | Web terminal | Embedded code-server | Via code-server |
| **OpenHands** | Yes | Local React app | Cloud version | No native |
| **Replit** | No | No | Primary interface | Partial (Ghostwriter) |
| **OpenCode** | Primary | TUI (Bubble Tea) | No | Yes (extension) |

### Real-time Visibility

**swe-swe** provides exceptional visibility:
- Multi-client WebSocket terminal with VT100 emulation ([cmd/swe-swe-server/main.go:307-364](cmd/swe-swe-server/main.go:307))
- Browser automation viewable via VNC at `/chrome` path ([README.md:273-276](README.md:273))
- In-session chat for team collaboration ([cmd/swe-swe-server/main.go:260-285](cmd/swe-swe-server/main.go:260))

**OpenHands** offers confirmation modes:
- "Direct mode: Executes immediately (development environments)"
- "Confirmation mode: Stores as pending, awaits user approval (production)"
([docs.openhands.dev/sdk/arch/agent](https://docs.openhands.dev/sdk/arch/agent))

**Replit** provides multiple modes: "Fast Mode (up to 5x faster), Design Mode, Plan Mode, and Build Mode" ([docs.replit.com](https://docs.replit.com/)).

**OpenCode** has permission-based tool access control with 50+ keyboard shortcuts for navigation ([github.com/opencode-ai/opencode](https://github.com/opencode-ai/opencode)).

---

## 4. Team Composition & Collaboration

| Feature | swe-swe | OpenHands | Replit | OpenCode |
|---------|---------|-----------|--------|----------|
| **Multi-user sessions** | Yes, real-time ([cmd/swe-swe-server/main.go:107-117](cmd/swe-swe-server/main.go:107)) | Cloud only | Yes | Shareable sessions |
| **Role-based access** | Password-based ([README.md:339-349](README.md:339)) | Cloud/Enterprise | Teams tier+ | No |
| **Async collaboration** | No | Via integrations | Yes | Session links |
| **Enterprise SSO** | No | Enterprise tier | Enterprise tier | No |

**Surprise Finding:** swe-swe's collaboration is surprisingly robust for a local tool - multiple developers can connect to the same terminal session simultaneously with accurate screen state synchronization via vt10x terminal emulation.

---

## 5. Software Development Constraints

### Language & Framework Support

| Platform | Languages | Frameworks | Constraints |
|----------|-----------|------------|-------------|
| **swe-swe** | Any (Docker-based) | Any | Limited by container resources |
| **OpenHands** | Primarily Python, bash | Any | CodeActAgent optimized for these |
| **Replit** | 50+ languages | Full-stack web | Browser-based limitations |
| **OpenCode** | Any (LSP-based) | Any | Terminal-only interface |

### Project Types Best Suited

**swe-swe:**
- Complex multi-service applications requiring browser automation
- Projects needing specific AI tool combinations
- Enterprise environments with SSL certificate requirements ([README.md:107-112](README.md:107))

**OpenHands:**
- Autonomous task execution (77.6 SWEBench score - [github.com/OpenHands/OpenHands](https://github.com/OpenHands/OpenHands))
- Data science and linear regression tasks (CodeActAgent demo)
- Projects requiring GitHub/GitLab/Jira integration (Cloud)

**Replit:**
- Rapid prototyping and MVPs
- Educational projects
- Simple full-stack web applications

**OpenCode:**
- Privacy-sensitive codebases
- Projects requiring specific LLM models
- Teams already using terminal workflows

### Deployment Targets

| Platform | Local | Docker | Cloud Deploy | Self-hosted |
|----------|-------|--------|--------------|-------------|
| **swe-swe** | Via Docker | Native | Manual | Yes |
| **OpenHands** | Yes | Yes | OpenHands Cloud | Enterprise (K8s) |
| **Replit** | No | No | One-click | No |
| **OpenCode** | Yes | Possible | No | Yes |

---

## 6. Pricing & Cost Model

| Platform | Free Tier | Paid Plans | Cost Model |
|----------|-----------|------------|------------|
| **swe-swe** | Fully free | N/A | BYOK (Bring Your Own Keys) |
| **OpenHands** | CLI + Local free | Cloud: $10 credits, Enterprise: custom | LLM costs + Cloud compute |
| **Replit** | Limited | Core $20/mo, Teams $35/user/mo ([superblocks.com/blog/replit-pricing](https://www.superblocks.com/blog/replit-pricing)) | Effort-based + compute |
| **OpenCode** | Fully free | Zen service (premium models) | BYOK + optional premium |

**Surprise Finding:** Replit's effort-based pricing makes cost prediction difficult. "Heavy compute, storage, and Agent usage generates overage charges" beyond included credits.

---

## 7. Where Each Platform Thrives

### swe-swe: Best For

**Team Profiles:**
- Solo developers with existing AI CLI tool preferences
- Small teams needing shared development environments
- Enterprises with corporate proxy/SSL requirements

**Environments:**
- Local development with Docker
- Air-gapped environments (all tools bundled in container)
- Corporate networks requiring certificate injection ([cmd/swe-swe/main.go:811-864](cmd/swe-swe/main.go:811))

**Use Cases:**
- Browser automation development (MCP Playwright integration)
- Multi-agent workflows (switch between Claude, Gemini, Codex in same session)
- Pair programming with real-time terminal sharing

**Why:** swe-swe doesn't try to replace your AI tools - it makes them better by providing consistent infrastructure. The container isolation means experiments can't break your host system, and the web-based terminal means you can work from any device.

### OpenHands: Best For

**Team Profiles:**
- AI researchers building custom agents
- Teams wanting autonomous task execution
- Organizations with Jira/Slack/GitHub workflow integrations

**Environments:**
- CI/CD pipelines (headless agent execution)
- Cloud-native deployments (Kubernetes Enterprise)
- Research environments needing reproducibility

**Use Cases:**
- Automated issue resolution (GitHub integration)
- Large-scale code migrations
- Building custom AI agents with SDK

**Why:** OpenHands' SWEBench performance (77.6 score) demonstrates it can autonomously solve real software engineering tasks. The SDK enables building specialized agents for specific domains.

### Replit: Best For

**Team Profiles:**
- Students and educators
- Non-technical founders building MVPs
- Hackathon participants

**Environments:**
- Chromebook/tablet development
- Shared computer labs
- Environments where local setup is impossible

**Use Cases:**
- Rapid prototyping from natural language
- Learning to code with AI assistance
- Quick demos and proof-of-concepts

**Why:** Zero setup friction is Replit's killer feature. Type a prompt, get a deployed app. The trade-off is less control and higher costs at scale.

### OpenCode: Best For

**Team Profiles:**
- Privacy-conscious developers
- Terminal power users
- Teams requiring specific LLM providers

**Environments:**
- Secure/classified environments
- Companies with strict data policies
- Local-only development requirements

**Use Cases:**
- Working with proprietary codebases
- Projects requiring local LLM inference
- Developers who live in the terminal

**Why:** OpenCode's privacy-first design ("does not store code or context data") makes it suitable for sensitive work. The 75+ model providers means maximum flexibility.

---

## 8. Architectural Comparison

### Execution Model

```
swe-swe:
┌──────────────────────────────────────────┐
│  Host Machine                            │
│  ┌────────────────────────────────────┐  │
│  │  Docker Compose                    │  │
│  │  ┌──────────┐ ┌────────┐ ┌──────┐  │  │
│  │  │swe-swe   │ │code-   │ │chrome│  │  │
│  │  │(Claude,  │ │server  │ │(VNC) │  │  │
│  │  │Gemini,..)│ │        │ │      │  │  │
│  │  └──────────┘ └────────┘ └──────┘  │  │
│  │       ↑           ↑          ↑     │  │
│  │       └───────────┴──────────┘     │  │
│  │              Traefik               │  │
│  └────────────────────────────────────┘  │
└──────────────────────────────────────────┘

OpenHands:
┌─────────────────────────────────────────┐
│  OpenHands Runtime (Docker/Remote/Local)│
│  ┌─────────────────────────────────────┐│
│  │         CodeActAgent                ││
│  │  ┌─────────┐   ┌────────────────┐   ││
│  │  │   LLM   │──▶│  Tool System   │   ││
│  │  │Interface│   │(Action→Observe)│   ││
│  │  └─────────┘   └────────────────┘   ││
│  │       ▲              │              ││
│  │       └──────────────┘              ││
│  │         Event Loop                  ││
│  └─────────────────────────────────────┘│
└─────────────────────────────────────────┘
```

### Key Architectural Differences

| Aspect | swe-swe | OpenHands |
|--------|---------|-----------|
| **Agent logic** | External (Claude, Gemini, etc.) | Internal (CodeActAgent) |
| **Tool integration** | Via MCP in each AI tool | Native Action-Observation pattern |
| **State management** | PTY session with vt10x emulation | Event history with condenser |
| **Sandboxing** | Docker containers | Docker/Remote/Local workspaces |

---

## 9. Summary Recommendations

### Choose swe-swe if:
- You already use Claude Code, Gemini CLI, or similar tools
- You need browser automation with AI (MCP Playwright)
- Your team wants to share a development environment
- You have corporate SSL/proxy requirements

### Choose OpenHands if:
- You want autonomous AI task execution
- You're building custom AI agents
- You need cloud-scale agent deployment
- Jira/Slack/GitHub integration is important

### Choose Replit if:
- Zero setup is more important than control
- You're prototyping or learning
- You need instant deployment
- Cost predictability isn't critical

### Choose OpenCode if:
- Privacy/security is paramount
- You need maximum LLM flexibility (75+ providers)
- You prefer terminal-based workflows
- You want to avoid vendor lock-in

---

## Sources

### Web Sources
- [OpenHands GitHub](https://github.com/OpenHands/OpenHands) - Primary repository, 66.1k stars
- [OpenHands Docs - Overview](https://docs.openhands.dev/) - Official documentation
- [OpenHands Docs - Architecture](https://docs.openhands.dev/sdk/arch/overview) - SDK architecture details
- [OpenHands Docs - Agent](https://docs.openhands.dev/sdk/arch/agent) - Agent design patterns
- [OpenHands Docs - Tool System](https://docs.openhands.dev/sdk/arch/tool-system) - Action-Observation framework
- [OpenHands Docs - Runtimes](https://docs.openhands.dev/usage/runtimes) - Execution environments
- [OpenHands Docs - CLI](https://docs.openhands.dev/usage/cli) - CLI installation and usage
- [OpenHands Docs - Local Setup](https://docs.openhands.dev/usage/run-openhands/local-setup) - Local GUI setup
- [OpenHands Docs - Agents](https://docs.openhands.dev/usage/agents) - CodeActAgent details
- [Replit Docs](https://docs.replit.com/) - Official documentation
- [Replit Pricing Analysis](https://www.superblocks.com/blog/replit-pricing) - Third-party pricing breakdown
- [OpenCode Website](https://opencode.ai/) - Official website
- [OpenCode GitHub](https://github.com/opencode-ai/opencode) - Source repository

### Local File Sources
- [README.md](README.md) - swe-swe project documentation
- [cmd/swe-swe/main.go](cmd/swe-swe/main.go) - CLI implementation
- [cmd/swe-swe-server/main.go](cmd/swe-swe-server/main.go) - WebSocket server implementation

---

*Research conducted by Claude Code on 2026-01-01*
