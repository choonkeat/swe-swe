# Configuration Reference

> For full command reference, examples, and build architecture, see [cli-commands-and-binary-management.md](cli-commands-and-binary-management.md).

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
  --ssl MODE                             SSL mode: 'no' (default), 'selfsign', 'selfsign@<host>',
                                         'letsencrypt@<domain>', or 'letsencrypt-staging@<domain>'
  --email EMAIL                          Email for Let's Encrypt certificate notifications (required with letsencrypt)
  --copy-home-paths PATHS                Comma-separated paths relative to $HOME to copy into container
                                         (e.g., .gitconfig,.ssh/config)
  --preview-ports RANGE                  App preview port range (default: 3000-3019)
  --public-ports RANGE                   Public (no-auth) port range (default: 5000-5019)
  --repos-dir DIR                        Host directory to mount at /repos for external repo clones
                                         (default: .swe-swe/repos in project)
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
| `SWE_SWE_AUTO_UPGRADE` | When set to any non-empty value, `swe-swe up` skips the "Upgrade? [y/N]" prompt if the CLI version is newer than the stored container config. It regenerates config with saved flags (`init --previous-init-flags=reuse`) and auto-injects `--build`. Used by the systemd unit in `deploy/digitalocean/` so remote hosts self-upgrade. | unset |
| `SWE_PORT` | External port | `1977` |
| `ANTHROPIC_API_KEY` | Claude API key (passed through automatically) | — |
| `OPENAI_API_KEY` | OpenAI API key for Codex (uncomment in docker-compose.yml) | — |
| `GEMINI_API_KEY` | Google Gemini API key (uncomment in docker-compose.yml) | — |
| `SWE_PUBLIC_PORTS` | Public port range override (default: `5000-5019`, must be within 5000-5999) | `5000-5019` |
| `SWE_PREVIEW_VHOST_SUFFIX` | Logical vhost suffix rewritten onto the upstream `Host` for App Preview host-demux (see [ADR-0045](adr/0045-preview-host-demux.md)). A preview label `app1-5000` proxies to `127.0.0.1:5000` with `Host: app1.<suffix>:5000`, so your compose traefik/nginx matches it as on a laptop. | `lvh.me` |
| `SWE_PREVIEW_REACH_DOMAIN` | Explicit browser-reachable wildcard domain for App Preview. When set it is the sole reach candidate the frontend probes (else it derives `lvh.me` plus the page's own hostname). Use a wildcard domain that resolves to the swe-swe machine *from the user's browser* (e.g. `<ip>.sslip.io`, or an admin-owned wildcard). Its first label is treated as the reach itself, not a vhost. | derived |
| `NODE_EXTRA_CA_CERTS` | Enterprise CA certificate path (auto-copied during init) | — |
| `SSL_CERT_FILE` | SSL certificate file path (auto-copied during init) | — |
| `NODE_EXTRA_CA_CERTS_BUNDLE` | Bundle of CA certificates (auto-copied during init) | — |

### Proxy command

These environment variables tune `swe-swe proxy` behavior:

| Variable | Description | Default |
|----------|-------------|---------|
| `PROXY_HEARTBEAT_STALE` | Duration before a client heartbeat is considered stale | `5s` |
| `PROXY_KILL_GRACE` | Grace period before force-killing a proxied process | `5s` |
| `PROXY_SHUTDOWN_GRACE` | Grace period for graceful shutdown on SIGINT/SIGTERM | `30s` |

## Config Files

After `swe-swe init`, editable files are generated in `$HOME/.swe-swe/projects/{sanitized-path}/`:

| File | Purpose |
|------|---------|
| `docker-compose.yml` | Service definitions — edit to uncomment API keys, add volumes, etc. |
| `Dockerfile` | Container image — edit to add custom build steps |

And in your project directory:

| File | Purpose |
|------|---------|
| `.swe-swe/` | Internal directory for certs, proxy scripts, and uploads |
| `.swe-swe/env` | Environment file sourced inside the container — see [.swe-swe/env](#sweswe-env) below |

Saved init flags are stored in `$HOME/.swe-swe/projects/{sanitized-path}/init.json` and can be reapplied with `--previous-init-flags=reuse`.

### Container-side (injected per session)

Each session gets these environment variables automatically:

| Variable | Description | Example |
|----------|-------------|---------|
| `PORT` | Preview app port (from `--preview-ports` range) | `3000` |
| `AGENT_CHAT_PORT` | Agent chat MCP port (`PORT` + 1000) | `4000` |
| `AGENT_CHAT_DISABLE` | Set to `1` for non-chat (terminal) sessions; unset for agent-chat sessions | `1` |
| `PUBLIC_PORT` | Public no-auth port (from `--public-ports` range) | `5000` |
| `SESSION_UUID` | Unique session identifier | `a1b2c3...` |

The `PUBLIC_PORT` is accessible externally without authentication, unlike `PORT` and `AGENT_CHAT_PORT` which are behind ForwardAuth. Use it for webhooks, public APIs, or shareable preview URLs that don't require login.

`AGENT_CHAT_DISABLE` controls the built-in `AskUserQuestion` guard hook that swe-swe installs into `~/.claude/settings.json`. That tool's multiple-choice menu renders only in the local terminal TUI, which is invisible to a user talking through the web chat UI -- calling it there hangs the agent forever. The hook therefore blocks `AskUserQuestion` (forcing the agent to ask via the agent-chat `send_message` tool) unless `AGENT_CHAT_DISABLE=1`:

| `AGENT_CHAT_DISABLE` | Behaviour |
|----------------------|-----------|
| `1` | Allow the built-in tool (the TUI is the real user surface) |
| unset | Block it, forcing `send_message` |
| `0`, `true`, anything else | Block it (only the literal `1` allows) |

swe-swe-server sets `AGENT_CHAT_DISABLE=1` for non-chat (terminal) sessions and leaves it unset for agent-chat sessions, so the fail-safe default in web chat is to block. Hooks are snapshotted at session start, so this env var (read at tool-call time) is the per-session knob.

<a id="sweswe-env"></a>
### .swe-swe/env

Drop a `.swe-swe/env` file at the root of your workspace to set per-project environment variables. The file is read in two complementary places:

1. **Agent processes** (Codex, Claude, Gemini, Goose, etc.): swe-swe-server's `loadEnvFile` parses the file as `KEY=VALUE` lines and merges entries into the agent process's environment via `execve`. `$VAR` and `${VAR}` references are expanded against the parent server's env.
2. **Login shells** (the right-side "Terminal" tab Shell session, plus any `bash -l` you spawn manually): `/etc/profile.d/zz-swe-swe-env.sh` sources the same file with `set -a`. This is required because `bash -l` sources `/etc/profile`, which on Debian unconditionally resets `PATH` — without this re-application, any `PATH=...` line in `.swe-swe/env` would be silently clobbered for login shells.

**Format rules** (kept narrow so both parsers agree):

- One `KEY=value` per line.
- Lines starting with `#` and blank lines are ignored.
- `$VAR` / `${VAR}` references are expanded.
- Values containing spaces or shell metacharacters **must be quoted** (`MSG="hello world"`) — the bash sourcing path requires it.
- No `export` prefix needed; both paths handle export automatically.

**Example:**

```
# Add Go to PATH for both the agent and the Terminal tab
PATH=/usr/local/go/bin:$PATH

# API keys for whatever the agent calls out to
OPENAI_API_KEY=sk-...
DATABASE_URL=postgres://localhost/myapp
```

The file is read at session start. To pick up changes, end and restart the session.

**Migration note:** Earlier swe-swe versions used `swe-swe/env`. swe-swe-server now auto-renames `swe-swe/env` to `.swe-swe/env` on the next session prepare, so existing workspaces self-heal without user action.

### Chat-log archiving (AGENT_CHAT_EXPORT_DIR)

Chat sessions default `AGENT_CHAT_EXPORT_DIR` to `{workDir}/agent-chats`, which makes agent-chat (>= 0.8.14) stream a markdown archive of the conversation — with screenshots and other attachments copied into `agent-chats/assets/` — into the repo as the chat progresses. Nothing is ever committed automatically; the export sits in the working tree.

The default is presence-checked, so any user-set value wins:

- **Opt out per workspace**: an `AGENT_CHAT_EXPORT_DIR=` line (empty value) in `.swe-swe/env`. Check it in to make it team policy.
- **Opt out per session**: untick "Archive chat log into repo" in the new-session dialog, or add the empty line to the Settings panel env textarea.
- **Opt out mid-session**: ask the agent to call agent-chat's `chatlog_optout` tool (stops the export and deletes this session's file).
- **Relocate**: set the var to a different path. It must stay inside the session's working directory — a path that escapes it disables the export **silently** (agent-chat logs a warning to its own stderr; nothing appears in the chat). If "nothing is being archived", check this first.

Merge conflicts in `agent-chats/index.html` need no manual resolution: accept either side; the next export regenerates it from the `*.md` files.

## Runtime

### YOLO Mode

YOLO mode allows agents to auto-approve actions without user confirmation. Toggle it via:

- **Status bar**: Click the "Connected" text (changes to "YOLO" when active)
- **Settings panel**: Use the YOLO toggle switch

When toggled, the agent restarts with the appropriate YOLO flag. The status bar shows "YOLO" with a border indicator when active.
