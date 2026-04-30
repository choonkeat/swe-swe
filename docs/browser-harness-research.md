# Browser Harness: Research and Applicability to swe-swe

Date: 2026-04-19

Context: Gregor Zunic (founder, browser_use) and Magnus Muller announced
"Browser Harness" on 2026-04-18. This note summarizes the pitch and evaluates
what, if anything, swe-swe should change in response.

Source tweets:

- https://x.com/gregpr07/status/2045566281991311483 (original announcement)
- https://x.com/mamagnus00/status/2045575219042271570 (founder amplification)

## What "Browser Harness" claims to be

From the announcement text:

- "A self-healing harness that can complete virtually any browser task."
- "We got tired of browser frameworks restricting the LLM. So we removed the
  framework."
- Self-healing: the LLM "edits helpers.py on the fly" as it hits edge cases.
- Direct CDP: "one websocket to Chrome" rather than going through a
  Playwright-style wrapper.
- "No framework, no rails, complete freedom."
- "Drop-in for Claude Code and Codex."
- 100 percent open source.

Magnus (browser_use founder) adds that the agent "can modify its own harness,
therefore no task is impossible," and that agents share domain-specific skills
so later runs are faster.

In short: instead of exposing a fixed toolbelt (click, fill, navigate, ...) to
the LLM, give it a small editable Python helper module and a raw CDP socket;
let the LLM write and rewrite its own helpers as it goes.

## How swe-swe currently handles browser automation

(See `docs/browser-automation.md` for the canonical description.)

- Per-session Chromium process, launched on first MCP call.
- Per-session CDP endpoint exposed as `BROWSER_CDP_PORT` (6000..6019).
- Per-session VNC via noVNC for visual observation.
- `swe-swe-playwright` MCP connects to `http://localhost:$BROWSER_CDP_PORT` and
  exposes a Playwright-style toolbelt (`browser_navigate`, `browser_click`,
  `browser_snapshot`, ...).
- Internal e2e tests in `e2e/` also use Playwright directly.

The important architectural observation: swe-swe already exposes a raw CDP
socket per session. The MCP Playwright server is just one client of that
socket, not a hard dependency of the browser stack.

## Applicability

Browser Harness's value proposition targets exactly the layer swe-swe currently
ships: the MCP Playwright wrapper. Everything below that layer (Xvfb, Chromium,
CDP port, VNC) is already set up the way Browser Harness wants.

Three practical paths:

### 1. Do nothing in swe-swe core

Users who want Browser Harness can install it inside their session container
and point it at `$BROWSER_CDP_PORT`. Nothing about swe-swe blocks this today.
Cost: zero. Discoverability: low.

### 2. Opt-in init flag: `--browser-harness`

Parallel to existing agent-selection flags. When set:

- Skip or supplement the `swe-swe-playwright` MCP registration.
- Preinstall browser_use's harness inside the container.
- Register it as an MCP for the chosen agent (or whatever mechanism
  browser_use uses for Claude Code and Codex).

Cost: a small template change plus golden test variants. Follows the two-commit
TDD pattern in `CLAUDE.md` (baseline flag + variants, then implementation).

### 3. Replace Playwright MCP entirely

Not recommended. Playwright MCP is battle-tested, well-understood by users, and
used internally for `e2e/`. Swapping it wholesale trades a known working setup
for novelty. The "self-healing helpers" promise is attractive but unproven in
our workflow.

## Recommendation

- Keep Playwright MCP as the default.
- If users ask for Browser Harness, add it as an opt-in init flag (option 2).
  It is a small, reversible change, and it aligns with swe-swe's pattern of
  letting the user pick their agent and tooling stack.
- Leave internal e2e on Playwright. Stability beats novelty for our own test
  suite.
- Do nothing pre-emptively. The announcement is one day old; wait for usage
  reports before committing to a default switch.

## Answers from browser_use/browser-use (GitHub)

The tweets use "Browser Harness" as framing. The upstream repository is
`browser-use/browser-use`. Findings from its README, `SKILL.md`, and
`init_cmd.py`:

- **How it plugs into Claude Code.** As a **Claude Code Skill**, not an MCP.
  Install is a one-liner:
  ```bash
  mkdir -p ~/.claude/skills/browser-use
  curl -o ~/.claude/skills/browser-use/SKILL.md \
    https://raw.githubusercontent.com/browser-use/browser-use/main/skills/browser-use/SKILL.md
  ```
  The skill invokes a local `browser-use` CLI (Python package, installed via
  `uv add browser-use` or `uvx browser-use`). Codex integration is the same
  shape; the skill just documents the CLI.
- **Chromium flags and CDP.** The CLI accepts a global `--cdp-url <url>`
  (accepts both `http://` and `ws://`), or attaches to an existing Chrome
  launched with `--remote-debugging-port=9222`. **swe-swe already exposes
  exactly this**: `BROWSER_CDP_PORT=6000` per session. Integration is
  effectively `export BROWSER_USE_CDP_URL=http://localhost:$BROWSER_CDP_PORT`
  (or passing `--cdp-url` at call time). No Chromium launch changes needed.
- **License.** MIT.
- **"helpers.py" from the tweet.** There is no canonical `helpers.py` in the
  repo. The "self-healing helpers" pattern refers to the user-editable
  template files scaffolded by `uvx browser-use init --template {default,
  advanced, tools}` (templates are fetched from GitHub at runtime; default
  entry file is `main.py`). The LLM edits those project-local scripts.

### Revised integration shape for swe-swe

Because Browser Harness is a **skill + CLI**, not an MCP, it does not conflict
with `swe-swe-playwright`. Both can coexist. An opt-in init flag would only
need to:

1. Ensure `uv` is available in the container image (or `pipx install
   browser-use`).
2. Drop `SKILL.md` into the agent's skills directory (`~/.claude/skills/
   browser-use/SKILL.md` for Claude Code; Codex has its own path).
3. Export `BROWSER_USE_CDP_URL=http://localhost:$BROWSER_CDP_PORT` alongside
   the existing per-session env vars, so the CLI attaches to the session's
   Chromium without a second browser instance.

This is a strictly additive change: no existing users lose Playwright MCP.
