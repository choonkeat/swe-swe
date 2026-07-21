# swe-swe

## Commands

Slash-command agents (Claude, Codex, Gemini, OpenCode, Pi) get these as `/swe-swe:<name>`:

| Command | Description |
|---------|-------------|
| `debug-preview-page` | Debug web apps using the App Preview debug channel |
| `pr-discuss` | Discuss and resolve a GitHub PR / GitLab MR over chat |
| `update` | Update workspace swe-swe files after a version upgrade |

Agents without slash-command support (Goose, Aider) do not see these commands.

## Tools on PATH

- `prctx` - read a GitHub PR / GitLab MR's review comments, stage replies and
  new comments locally, then post them upstream. Reach for it whenever the user
  points at a PR/MR url or asks to answer review feedback. Nothing is sent until
  `prctx flush`, so drafting is safe. Run `prctx -h` for the full workflow; the
  token comes from Settings > Credentials > Git HTTPS.

## Environment Conventions

- `PORT` - a webpage served on this port renders automatically in the user's Preview tab.
- `PUBLIC_PORT` - a webpage served on this port is accessible to anyone (not protected behind auth).
- Chrome CDP is lazy-loaded on demand: it starts the first time an MCP playwright tool is invoked. No browser process is running before that.
- Tests/e2e that connect to `$BROWSER_CDP_PORT` directly must run after a Playwright MCP call (e.g. `browser_navigate`) to warm CDP. The suite won't trigger the lazy launch itself, so it will fail until then.
- Chat sessions auto-archive their conversation into `agent-chats/` (markdown + assets, updated as the chat progresses). Once the task at hand is clear, name the log via `set_chat_title` so it is not left `-untitled`. Never delete or rewrite other sessions' entries in `agent-chats/`.
