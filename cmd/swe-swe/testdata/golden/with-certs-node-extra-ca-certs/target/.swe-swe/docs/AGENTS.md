# swe-swe

## Commands

Slash-command agents (Claude, Codex, Gemini, OpenCode, Pi) get these as `/swe-swe:<name>`:

| Command | Description |
|---------|-------------|
| `debug-preview-page` | Debug web apps using the App Preview debug channel |
| `update-swe-swe` | Update workspace swe-swe files after a version upgrade |

Agents without slash-command support (Goose, Aider) do not see these commands.

## Environment Conventions

- `PORT` - a webpage served on this port renders automatically in the user's Preview tab.
- `PUBLIC_PORT` - a webpage served on this port is accessible to anyone (not protected behind auth).
- Chrome CDP is lazy-loaded on demand: it starts the first time an MCP playwright tool is invoked. No browser process is running before that.
- Tests/e2e that connect to `$BROWSER_CDP_PORT` directly must run after a Playwright MCP call (e.g. `browser_navigate`) to warm CDP. The suite won't trigger the lazy launch itself, so it will fail until then.
