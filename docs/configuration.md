# Configuration Reference

## CLI Flags (`swe-swe init`)

```
Init Options:
  --project-directory PATH               Project directory (defaults to current directory)
  --previous-init-flags=reuse            Reapply saved configuration from previous init
  --previous-init-flags=ignore           Ignore saved configuration, use provided flags
  --agents AGENTS                        Comma-separated agents: claude,gemini,codex,aider,goose,opencode (default: all)
  --exclude-agents AGENTS                Comma-separated agents to exclude
  --apt-get-install PACKAGES             Additional apt packages to install (comma or space separated)
  --npm-install PACKAGES                 Additional npm packages to install globally (comma or space separated)
  --with-docker                          Mount Docker socket to allow container to run Docker commands
  --with-slash-commands REPOS            Git repos to clone as slash commands for Claude/Codex/OpenCode
                                         Format: [alias@]<git-url> (space-separated)
  --ssl MODE                             SSL mode: 'no' (default), 'selfsign', or 'selfsign@<host>'
                                         Use selfsign@<ip-or-hostname> for remote access
  --copy-home-paths PATHS                Comma-separated paths relative to $HOME to copy into container
                                         (e.g., .gitconfig,.ssh/config)
  --preview-ports RANGE                  App preview port range (default: 3000-3019)
  --terminal-font-size SIZE              Terminal font size in pixels (default: 14)
  --terminal-font-family FONT            Terminal font family (default: Menlo, Monaco, "Courier New", monospace)
  --status-bar-font-size SIZE            Status bar font size in pixels (default: 12)
  --status-bar-font-family FONT          Status bar font family (default: system sans-serif)
```

## Environment Variables

### Host-side (set before `swe-swe up`)

| Variable | Description | Default |
|----------|-------------|---------|
| `SWE_SWE_PASSWORD` | Authentication password for all services | `changeme` |
| `SWE_PORT` | External port | `1977` |
| `ANTHROPIC_API_KEY` | Claude API key (passed through automatically) | — |
| `OPENAI_API_KEY` | OpenAI API key for Codex (uncomment in docker-compose.yml) | — |
| `GEMINI_API_KEY` | Google Gemini API key (uncomment in docker-compose.yml) | — |
| `NODE_EXTRA_CA_CERTS` | Enterprise CA certificate path (auto-copied during init) | — |
| `SSL_CERT_FILE` | SSL certificate file path (auto-copied during init) | — |

### Proxy command

These environment variables tune `swe-swe proxy` behavior:

| Variable | Description | Default |
|----------|-------------|---------|
| `PROXY_HEARTBEAT_STALE` | Duration before a client heartbeat is considered stale | `5s` |
| `PROXY_KILL_GRACE` | Grace period before force-killing a proxied process | `5s` |
| `PROXY_SHUTDOWN_GRACE` | Grace period for graceful shutdown on SIGINT/SIGTERM | `30s` |

## Config Files

After `swe-swe init`, the following files are generated in your project directory:

| File | Purpose |
|------|---------|
| `docker-compose.yml` | Service definitions — edit to uncomment API keys, add volumes, etc. |
| `Dockerfile` | Container image — edit to add custom build steps |
| `.swe-swe/` | Internal directory for certs, proxy scripts, and uploads |
| `swe-swe/env` | Environment file sourced inside the container |

Saved init flags are stored in `$HOME/.swe-swe/projects/{sanitized-path}/init.json` and can be reapplied with `--previous-init-flags=reuse`.

## Runtime

### YOLO Mode

YOLO mode allows agents to auto-approve actions without user confirmation. Toggle it via:

- **Status bar**: Click the "Connected" text (changes to "YOLO" when active)
- **Settings panel**: Use the YOLO toggle switch

When toggled, the agent restarts with the appropriate YOLO flag. The status bar shows "YOLO" with a border indicator when active.
