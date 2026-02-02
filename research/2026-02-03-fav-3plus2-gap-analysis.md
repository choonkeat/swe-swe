# FAV 3+2 Gap Analysis: swe-swe vs. the Agentic Workspace Vision

**Research Date:** 2026-02-03
**Context:** Gap assessment against the "Files Agent View" (FAV) presentation vision — a 5-component agentic workspace (Core Triplet + two multipliers)

---

## Executive Summary

The FAV presentation defines a **3+2 system**: a Core Triplet (Conversational Interface, Dynamic Visualization, Agentic AI) plus two multipliers (+1 Building Block Library, +2 Recursive Improver). No platform fully implements this today. swe-swe's agentic execution layer is its strongest asset; the gaps are in the interaction and accumulation layers above it.

---

## Part 1: swe-swe vs. the FAV Vision

### The Core Triplet (3)

| Component | Presentation Vision | swe-swe Today | Gap |
|-----------|-------------------|---------------|-----|
| **1. Conversational Interface** | Human-in-the-loop chat — steer, decide, collaborate as a team | Terminal I/O to individual AI agents (Claude, Gemini, etc.). Multi-viewer PTY sharing exists but it's a dev terminal, not a team conversation space. | **Medium.** The raw conversational channel exists (agent ↔ human via terminal), but there's no team-oriented chat layer — no threaded discussions, no multi-user conversation around data/decisions, no persistent conversation history that the system reasons over. |
| **2. Dynamic Visualization** | Real-time canvas — see the data, not a report; live preview | App Preview panel exists. Browser VNC exists. Terminal recordings with playback exist. | **Medium-Large.** Preview is scoped to "whatever the agent's app serves on a port." There's no general-purpose dynamic canvas — no charts-on-demand, no visual evidence clusters, no "non-technical user asks for a chart via chat" capability. The visualization is code-output, not data-visualization. |
| **3. Agentic AI** | Tool-using executor — pulls data, calls APIs, reads files, acts on your behalf | Multi-agent support (Claude, Gemini, Codex, Aider, Goose, OpenCode), MCP Playwright browser automation, app preview debug channel, YOLO mode, proxy for host tools. | **Small.** This is swe-swe's strongest pillar. The agent can execute code, use tools, automate browsers, and act autonomously. The gap is that it's scoped to software engineering — it doesn't pull business data, call SaaS APIs, or act on general knowledge-work tasks. |

### The +2 Multipliers

| Component | Presentation Vision | swe-swe Today | Gap |
|-----------|-------------------|---------------|-----|
| **+1 Building Block Library** | Reusable blocks (SAP connector, compliance checker, budget template) shared across teams — an "app store" built by your colleagues | Slash commands (cloneable git repos of markdown/TOML commands). That's it. | **Large.** Slash commands are developer-facing CLI snippets, not composable building blocks. There's no registry, no sharing across teams, no discoverability UI, no "install this block into my workspace" flow. The concept of a block that encapsulates a connector, workflow, or template doesn't exist yet. |
| **+2 Recursive Improver** | Analyzes past conversations/workflows, finds repetitive patterns, proposes new tools/templates/process changes — Toyota-style continuous improvement | Nothing. Sessions are stateless. Terminal recordings exist but are pure audit playback — no analysis, no pattern detection. | **Very Large.** This is the biggest gap. There is zero cross-session learning, zero workflow analysis, zero automated suggestion of improvements. No conversation history is retained for analysis. No feedback loop exists. |

### Visual Summary

```
                    Presentation FAV          swe-swe today
                    ───────────────           ─────────────
Agentic AI          ████████████  (core)      ████████░░  (strong, but SW-eng only)
Conversation        ████████████  (core)      ████░░░░░░  (terminal, not team chat)
Visualization       ████████████  (core)      ███░░░░░░░  (app preview, not canvas)
Block Library       ████████████  (+1)        ██░░░░░░░░  (slash commands only)
Recursive Improver  ████████████  (+2)        ░░░░░░░░░░  (nothing)
```

### Three Highest-Impact Gaps to Close

1. **Conversation layer** — swe-swe needs a persistent, multi-user conversational interface where teams discuss, decide, and the agent participates as a peer. Today's terminal sessions are ephemeral and single-user-focused. This is the connective tissue that makes the other two pillars useful.

2. **Dynamic visualization / canvas** — The app preview is a start, but the vision calls for an agent that can render charts, dashboards, and visual artifacts on demand during a conversation. This means a general-purpose rendering surface (not just "whatever localhost:3000 serves").

3. **Recursive improver** — The hardest and most differentiated piece. Requires: (a) persisting conversation/workflow logs in a queryable form, (b) an analysis layer that detects repetitive patterns, and (c) a suggestion engine that proposes new blocks/templates/process changes. Terminal recordings are a potential data source, but there's no analysis pipeline.

The block library (+1) is the natural bridge — once conversations and visualizations exist, the things teams build inside them become the blocks. Slash commands are the embryo of this, but need to evolve from "CLI snippets for developers" to "composable workspace capabilities for anyone."

---

## Part 2: Competitive Landscape

### Scorecard

| Platform | Conversation | Visualization / Canvas | Agentic AI | +1 Block Library | +2 Recursive Improver |
|----------|-------------|----------------------|------------|-----------------|---------------------|
| **swe-swe** | Terminal PTY | App preview panel | Multi-agent, MCP, browser | Slash commands | None |
| **OpenHands** | Chat UI + CLI | VS Code + VNC + browser | Multi-agent SDK, scales to 1000s | Composable SDK | None |
| **Cursor / Windsurf** | Editor-inline chat | Code diff / Codemaps | Agent mode (Composer/Cascade) | Extensions ecosystem | None |
| **Devin** | Chat + autonomous | Browser + shell viewer | Full autonomy, planning | None | None |
| **FlowithOS** | Chat + canvas nodes | 2D canvas, branching, visual | Agent Neo, multi-agent | Knowledge Garden + templates | Partial (Oracle learns patterns) |
| **Duco** | Chat-based workspace | Auditable dashboards | Agentic Rule Builder | Rule/process templates | Partial (learns approval criteria) |

### Candidate Analysis

**OpenHands** ([openhands.dev](https://openhands.dev/), [GitHub](https://github.com/OpenHands/OpenHands)) is the closest open-source competitor architecturally. It has a proper chat UI, a REST/WebSocket server, VS Code + VNC + Chromium (very similar to swe-swe's stack), and a composable Python SDK. But it's still a coding agent platform — it doesn't have a general-purpose canvas, no block library for non-dev use cases, and no recursive improver. It's swe-swe's peer, not swe-swe's FAV target.

**Cursor / Windsurf / Devin** are IDE-first tools. They nail the developer conversation + agentic execution loop, but they're firmly in the "Vibe-Code Software" column of the presentation's comparison table — not the "Agentic Workspace" column. No canvas, no block library, no improvement loop. Windsurf's Cognition acquisition could change this, but today it's an editor.

**FlowithOS** ([flowith.io](https://flowith.io/)) is the closest thing to the FAV vision that exists today:
- 2D canvas with branching conversations (not just chat)
- Multi-agent orchestration with handoffs
- Knowledge Garden as a proto-block-library
- Oracle system that reads intent and reduces re-prompting (proto-improver)

But it's a hosted SaaS — closed source, no self-hosting, no container isolation, no code execution environment. It's a knowledge/content workspace, not a software workspace.

**Duco** ([du.co](https://du.co/agentic-workspace-for-intelligent-operations/)) is domain-specific (financial ops) and explicitly uses the phrase "agentic workspace." It has the chat + agents + auditable workspace model, agents that learn approval criteria over time (proto-improver), and rule templates (proto-blocks). But it's vertical SaaS, not a general platform. GA planned for Q1 2026.

### Where Competitors Are That swe-swe Isn't

| Competitor | Capability swe-swe lacks |
|-----------|-------------------------|
| OpenHands | Proper chat UI, composable SDK, cloud scaling |
| FlowithOS | 2D canvas, branching conversations, knowledge garden |
| Duco | Team collaboration, auditable workflows, learning criteria |
| Cursor | Inline code visualization (diffs, codemaps) |

### Where Nobody Is Yet (the Real Opportunity)

| Component | State of the market |
|-----------|-------------------|
| **+1 Block Library** | FlowithOS has Knowledge Garden but no "install a connector" model. Nobody has the "app store built by your colleagues" vision. |
| **+2 Recursive Improver** | Nobody has this. FlowithOS Oracle reduces re-prompting but doesn't analyze past workflows to propose new tools. Duco learns approval criteria but doesn't generalize. This is wide open. |

---

## Bottom Line

**No one is the FAV 3+2 today.** The market has:
- Plenty of "3 minus 1" (agentic + conversation, no canvas) — OpenHands, Devin
- A few "3 minus 0" attempts (all three pillars) — FlowithOS is closest
- Zero "+1" implementations (real block library with cross-team sharing)
- Zero "+2" implementations (recursive improvement from workflow analysis)

swe-swe's strongest asset is its **agentic execution layer** — container isolation, multi-agent support, browser automation, proxy, recordings. That's the hardest part to build and it already works. The gaps to FAV are in the **interaction and accumulation layers** above it: team conversation, dynamic canvas, block sharing, and the improvement loop.

The +2 Recursive Improver is the most differentiated piece of the vision and is completely greenfield across the entire market.

---

## Sources

- [OpenHands GitHub](https://github.com/OpenHands/OpenHands)
- [OpenHands Platform](https://openhands.dev/)
- [OpenHands SDK Paper](https://arxiv.org/html/2511.03690v1)
- [FlowithOS Overview](https://skywork.ai/blog/ai-agent/agentic-workspaces-2025-flowithos-ai-productivity/)
- [FlowithOS Canvas Mode](https://skywork.ai/blog/ai-agent/function/flowith-os-canvas-mode-explained/)
- [FlowithOS Deep Dive](https://skywork.ai/skypage/en/flowithos-agentic-os-ai-workflows/1983347044656451584)
- [Duco Agentic Workspace](https://du.co/duco-introduces-agentic-workspace-for-intelligent-operations/)
- [Windsurf vs Cursor](https://windsurf.com/compare/windsurf-vs-cursor)
- [AI Code Editor Comparison 2026](https://research.aimultiple.com/ai-code-editor/)
- [Canvas Chat](https://ericmjl.github.io/blog/2025/12/31/canvas-chat-a-visual-interface-for-thinking-with-llms/)
- [LangChain Open Canvas](https://github.com/langchain-ai/open-canvas)
