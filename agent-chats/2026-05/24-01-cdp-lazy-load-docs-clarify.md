<!-- agent-chat export
title: Cdp Lazy Load Docs Clarify
date: 2026-05-24
index: 01
slug: cdp-lazy-load-docs-clarify
agent: claude
version: 0.7.1 (ca4147e)
-->

# Cdp Lazy Load Docs Clarify

_2026-05-24 · 01 · claude · agent-chat 0.7.1 (ca4147e)_

**USER**

> Sanity check that new swe swe installs would’ve clarified this in CLAUDE/AGENTS md?
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-05-24-01-1.jpeg" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-05-24-01-1.jpeg" alt="IMG_1713.jpeg" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

<small>took 1m 56s</small><br>
**AGENT**

> Checked the install templates — **partial: the root cause is documented, the e2e implication isn't.**
>
> A fresh `swe-swe init` ships two docs that both state the core fact:
>
> - **`.swe-swe/docs/AGENTS.md`** (L18): "Chrome CDP is lazy-loaded on demand: it starts the first time an MCP playwright tool is invoked. No browser process is running before that."
> - **`.swe-swe/docs/browser-automation.md`** (L16 + Troubleshooting): "launched on the first Playwright MCP tool call, not at session creation" → "If browser tools fail, try using a Playwright tool first (e.g. `browser_navigate`)."
>
> So the **cause** (CDP lazy, warms on first MCP call) is there — a careful agent reading AGENTS.md could've deduced your fix.
>
> **Missing:** the bridge to your exact case. The docs only frame the symptom as "browser *tools* fail." They don't anticipate that an **e2e suite connecting to `$BROWSER_CDP_PORT` directly** won't trigger the lazy launch itself, so it fails until a prior MCP playwright call warms CDP. That leap you had to spell out by hand.
>
> Want me to add a one-liner to the templates so future installs cover it? e.g.: "Tests/e2e that connect to `$BROWSER_CDP_PORT` directly must run after a Playwright MCP call (e.g. `browser_navigate`) — the suite won't trigger the lazy launch itself."

[Quick replies]
- Yes, add the one-liner
- No, leave docs as-is
- Reword it first

**USER**

> Yes, add the one-liner

<small>took 2m 38s</small><br>
**AGENT**

> Added. Edited the source-of-truth templates (the host `container-templates/` copies are untracked and regenerate from these at `swe-swe init`):
>
> **`templates/container/.swe-swe/docs/AGENTS.md`** — appended under the CDP lazy-load line:
> > Tests/e2e that connect to `$BROWSER_CDP_PORT` directly must run after a Playwright MCP call (e.g. `browser_navigate`) to warm CDP. The suite won't trigger the lazy launch itself, so it will fail until then.
>
> **`templates/container/.swe-swe/docs/browser-automation.md`** — new Troubleshooting bullet:
> > **e2e / tests fail connecting to CDP**: A test suite that connects to `$BROWSER_CDP_PORT` directly does not trigger the lazy browser launch. Warm CDP first with a Playwright MCP call (e.g., `browser_navigate` to any page), then run the suite.
>
> Verified:
> - `make build golden-update` → golden diff is exactly these 2 lines added across all variants (baseline + docs + container-templates), **nothing removed, no other files touched**
> - `make ascii-check` passed
> - `make test-cli` (golden comparison) passed — 47s, green
>
> Not committed yet. Want me to commit it?

[Quick replies]
- Yes, commit it
- No, I'll review first
- Commit + push

**USER**

> Export chat logs and commit together

