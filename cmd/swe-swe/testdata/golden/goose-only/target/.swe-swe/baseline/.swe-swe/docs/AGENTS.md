# swe-swe

## Commands

Slash-command agents (Claude, Codex, Gemini, OpenCode) get these as `/swe-swe:<name>`:

| Command | Description |
|---------|-------------|
| `setup` | Configure git, SSH, testing, credentials |
| `debug-preview-page` | Debug web apps using the App Preview debug channel |
| `update-swe-swe` | Update workspace swe-swe files after a version upgrade |

Agents without slash-command support (Goose, Aider) do not see these commands.

## Current Setup

<!-- Agent: Update this section when setup changes -->
- Git: (not configured)
- SSH: (not configured)
- Testing: (not configured)

## Environment Conventions

- `PORT` - a webpage served on this port renders automatically in the user's Preview tab.
- `PUBLIC_PORT` - a webpage served on this port is accessible to anyone (not protected behind auth).
- Chrome CDP is lazy-loaded on demand: it starts the first time an MCP playwright tool is invoked. No browser process is running before that.

## Documentation

- `app-preview.md` - App preview panel, navigation, and debug channel
- `browser-automation.md` - MCP browser at /chrome/
- `docker.md` - Docker access from container
