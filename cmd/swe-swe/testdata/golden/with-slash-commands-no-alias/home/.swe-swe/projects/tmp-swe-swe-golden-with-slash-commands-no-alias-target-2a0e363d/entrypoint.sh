#!/bin/bash
set -e
trap 'echo -e "\n\033[0;31m✗ Entrypoint failed at line $LINENO (exit code $?)\033[0m" >&2' ERR

# Container Entrypoint
# Configures MCP servers and agent tools, then starts swe-swe-server.
# In DOCKER mode, runs as root for socket permissions, then drops to app user.
# In non-DOCKER mode, runs directly as app user.

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# MCP-less mode: swe-swe-server hosts the MCP servers via mcp-cli-proxy per
# session; skip writing every agent's native MCP config below.
export SWE_MCP_LESS=1


# Copy slash commands to agent directories
if [ -d "/home/app/.swe-swe/commands/md/choonkeat/slash-commands/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.swe-swe/commands/md/choonkeat/slash-commands 2>/dev/null || true
    (cd /home/app/.swe-swe/commands/md/choonkeat/slash-commands && git pull) 2>/dev/null && \
        echo -e "${GREEN}[ok] Updated slash commands: choonkeat/slash-commands (swe-swe store)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: choonkeat/slash-commands (swe-swe store)${NC}"
elif [ -d "/tmp/slash-commands/choonkeat/slash-commands" ]; then
    mkdir -p "$(dirname "/home/app/.swe-swe/commands/md/choonkeat/slash-commands")"
    cp -r /tmp/slash-commands/choonkeat/slash-commands /home/app/.swe-swe/commands/md/choonkeat/slash-commands
    echo -e "${GREEN}[ok] Installed slash commands: choonkeat/slash-commands (swe-swe store)${NC}"
fi
if [ -e "/home/app/.claude/commands/choonkeat/slash-commands" ] && [ ! -L "/home/app/.claude/commands/choonkeat/slash-commands" ]; then
    echo -e "${YELLOW}⚠ Slash command target exists and is not a symlink, leaving unchanged: /home/app/.claude/commands/choonkeat/slash-commands (claude)${NC}"
elif [ -d "/home/app/.swe-swe/commands/md/choonkeat/slash-commands" ]; then
    mkdir -p "$(dirname "/home/app/.claude/commands/choonkeat/slash-commands")"
    ln -sfn /home/app/.swe-swe/commands/md/choonkeat/slash-commands /home/app/.claude/commands/choonkeat/slash-commands
    echo -e "${GREEN}[ok] Linked slash commands: choonkeat/slash-commands (claude)${NC}"
fi
if [ -e "/home/app/.codex/prompts/choonkeat/slash-commands" ] && [ ! -L "/home/app/.codex/prompts/choonkeat/slash-commands" ]; then
    echo -e "${YELLOW}⚠ Slash command target exists and is not a symlink, leaving unchanged: /home/app/.codex/prompts/choonkeat/slash-commands (codex)${NC}"
elif [ -d "/home/app/.swe-swe/commands/md/choonkeat/slash-commands" ]; then
    mkdir -p "$(dirname "/home/app/.codex/prompts/choonkeat/slash-commands")"
    ln -sfn /home/app/.swe-swe/commands/md/choonkeat/slash-commands /home/app/.codex/prompts/choonkeat/slash-commands
    echo -e "${GREEN}[ok] Linked slash commands: choonkeat/slash-commands (codex)${NC}"
fi
if [ -e "/home/app/.config/opencode/command/choonkeat/slash-commands" ] && [ ! -L "/home/app/.config/opencode/command/choonkeat/slash-commands" ]; then
    echo -e "${YELLOW}⚠ Slash command target exists and is not a symlink, leaving unchanged: /home/app/.config/opencode/command/choonkeat/slash-commands (opencode)${NC}"
elif [ -d "/home/app/.swe-swe/commands/md/choonkeat/slash-commands" ]; then
    mkdir -p "$(dirname "/home/app/.config/opencode/command/choonkeat/slash-commands")"
    ln -sfn /home/app/.swe-swe/commands/md/choonkeat/slash-commands /home/app/.config/opencode/command/choonkeat/slash-commands
    echo -e "${GREEN}[ok] Linked slash commands: choonkeat/slash-commands (opencode)${NC}"
fi
if [ -e "/home/app/.pi/agent/prompts/choonkeat/slash-commands" ] && [ ! -L "/home/app/.pi/agent/prompts/choonkeat/slash-commands" ]; then
    echo -e "${YELLOW}⚠ Slash command target exists and is not a symlink, leaving unchanged: /home/app/.pi/agent/prompts/choonkeat/slash-commands (pi)${NC}"
elif [ -d "/home/app/.swe-swe/commands/md/choonkeat/slash-commands" ]; then
    mkdir -p "$(dirname "/home/app/.pi/agent/prompts/choonkeat/slash-commands")"
    ln -sfn /home/app/.swe-swe/commands/md/choonkeat/slash-commands /home/app/.pi/agent/prompts/choonkeat/slash-commands
    echo -e "${GREEN}[ok] Linked slash commands: choonkeat/slash-commands (pi)${NC}"
fi


# Create OpenCode MCP configuration
# OpenCode uses a different schema: type="local" and command as array
# mcp-less mode skips native MCP config (swe-swe-server runs the proxy fleet).
if [ -z "$SWE_MCP_LESS" ]; then
mkdir -p /home/app/.config/opencode
cat > /home/app/.config/opencode/opencode.json << 'EOF'
{
  "mcp": {
    "swe-swe-agent-chat": {
      "type": "local",
      "command": ["sh", "-c", "exec npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY"]
    },
    "swe-swe-playwright": {
      "type": "local",
      "command": ["sh", "-c", "exec mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT"]
    },
    "swe-swe-preview": {
      "type": "local",
      "command": ["sh", "-c", "exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp?key=$MCP_AUTH_KEY"]
    },
    "swe-swe-whiteboard": {
      "type": "local",
      "command": ["npx", "-y", "@choonkeat/agent-whiteboard"]
    },
    "swe-swe": {
      "type": "local",
      "command": ["sh", "-c", "exec npx -y @choonkeat/agent-reverse-proxy --bridge 'http://localhost:$SWE_SERVER_PORT/mcp?key='$MCP_AUTH_KEY"]
    }
  }
}
EOF

echo -e "${GREEN}[ok] Created OpenCode MCP configuration${NC}"
fi

# Create Codex MCP configuration (TOML format)
# Codex sandboxes MCP child processes and only forwards env vars listed in
# `env_vars` -- so we cannot use the `sh -c "exec npx ... $VAR"` wrapper that
# the other agents use, since $VAR would expand to empty inside the sandbox.
# Instead we run npx (or mcp-lazy-init) directly and let Codex substitute
# $VAR references in args from the declared env_vars whitelist.
# mcp-less mode skips native MCP config (swe-swe-server runs the proxy fleet).
if [ -z "$SWE_MCP_LESS" ]; then
mkdir -p /home/app/.codex
cat > /home/app/.codex/config.toml << 'EOF'
[mcp_servers.swe-swe-agent-chat]
command = "npx"
args = ["-y", "@choonkeat/agent-chat", "--theme-cookie", "swe-swe-theme", "--autocomplete-triggers", "/=slash-command", "--autocomplete-url", "http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY"]
env_vars = ["AGENT_CHAT_PORT", "SWE_SERVER_PORT", "SESSION_UUID", "MCP_AUTH_KEY"]

[mcp_servers.swe-swe-playwright]
command = "mcp-lazy-init"
args = ["--init-method", "POST", "--init-url", "http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY", "--", "npx", "-y", "@playwright/mcp@latest", "--cdp-endpoint", "http://localhost:$BROWSER_CDP_PORT"]
env_vars = ["SWE_SERVER_PORT", "SESSION_UUID", "MCP_AUTH_KEY", "BROWSER_CDP_PORT"]

[mcp_servers.swe-swe-preview]
command = "npx"
args = ["-y", "@choonkeat/agent-reverse-proxy", "--bridge", "http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp?key=$MCP_AUTH_KEY"]
env_vars = ["SWE_SERVER_PORT", "SESSION_UUID", "MCP_AUTH_KEY"]

[mcp_servers.swe-swe-whiteboard]
command = "npx"
args = ["-y", "@choonkeat/agent-whiteboard"]

[mcp_servers.swe-swe]
command = "npx"
args = ["-y", "@choonkeat/agent-reverse-proxy", "--bridge", "http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY"]
env_vars = ["SWE_SERVER_PORT", "MCP_AUTH_KEY"]
EOF

echo -e "${GREEN}[ok] Created Codex MCP configuration${NC}"
fi

# Create Gemini MCP configuration
# mcp-less mode skips native MCP config (swe-swe-server runs the proxy fleet).
if [ -z "$SWE_MCP_LESS" ]; then
mkdir -p /home/app/.gemini
cat > /home/app/.gemini/settings.json << 'EOF'
{
  "mcpServers": {
    "swe-swe-agent-chat": {
      "command": "sh",
      "args": ["-c", "exec npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY"]
    },
    "swe-swe-playwright": {
      "command": "sh",
      "args": ["-c", "exec mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT"]
    },
    "swe-swe-preview": {
      "command": "sh",
      "args": ["-c", "exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp?key=$MCP_AUTH_KEY"]
    },
    "swe-swe-whiteboard": {
      "command": "npx",
      "args": ["-y", "@choonkeat/agent-whiteboard"]
    },
    "swe-swe": {
      "command": "sh",
      "args": ["-c", "exec npx -y @choonkeat/agent-reverse-proxy --bridge 'http://localhost:$SWE_SERVER_PORT/mcp?key='$MCP_AUTH_KEY"]
    }
  }
}
EOF

echo -e "${GREEN}[ok] Created Gemini MCP configuration${NC}"
fi

# Create Goose MCP configuration (YAML format)
# mcp-less mode skips native MCP config (swe-swe-server runs the proxy fleet).
if [ -z "$SWE_MCP_LESS" ]; then
mkdir -p /home/app/.config/goose
cat > /home/app/.config/goose/config.yaml << 'EOF'
extensions:
  swe-swe-agent-chat:
    type: stdio
    cmd: sh
    args:
      - "-c"
      - "exec npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY"
  swe-swe-playwright:
    type: stdio
    cmd: sh
    args:
      - "-c"
      - "exec mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT"
  swe-swe-preview:
    type: stdio
    cmd: sh
    args:
      - "-c"
      - "exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp?key=$MCP_AUTH_KEY"
  swe-swe-whiteboard:
    type: stdio
    cmd: npx
    args:
      - "-y"
      - "@choonkeat/agent-whiteboard"
  swe-swe:
    type: stdio
    cmd: sh
    args:
      - "-c"
      - "exec npx -y @choonkeat/agent-reverse-proxy --bridge 'http://localhost:$SWE_SERVER_PORT/mcp?key='$MCP_AUTH_KEY"
EOF

echo -e "${GREEN}[ok] Created Goose MCP configuration${NC}"
fi
# Wrapper: auto-run 'goose configure' if no provider is configured
mkdir -p /home/app/.swe-swe/bin
cat > /home/app/.swe-swe/bin/goose << 'GOOSE_WRAPPER'
#!/bin/bash
GOOSE=/usr/local/bin/goose
$GOOSE "$@" || ($GOOSE configure && $GOOSE "$@")
GOOSE_WRAPPER
chmod +x /home/app/.swe-swe/bin/goose
echo -e "${GREEN}[ok] Created Goose wrapper script${NC}"

# Create Claude MCP configuration (user scope = cross-project)
# Uses claude mcp add which writes to ~/.claude.json
# Always re-create to pick up any flag changes (e.g. --autocomplete-triggers)
# mcp-less mode skips native MCP config (swe-swe-server runs the proxy fleet).
if [ -z "$SWE_MCP_LESS" ]; then
claude_mcp_setup() {
  unset CLAUDECODE
  claude mcp remove --scope user swe-swe-agent-chat 2>/dev/null || true
  claude mcp remove --scope user swe-swe-playwright 2>/dev/null || true
  claude mcp remove --scope user swe-swe-preview 2>/dev/null || true
  claude mcp remove --scope user swe-swe-whiteboard 2>/dev/null || true
  claude mcp remove --scope user swe-swe 2>/dev/null || true
  claude mcp add --scope user --transport stdio swe-swe-agent-chat -- sh -c 'exec npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY'
  claude mcp add --scope user --transport stdio swe-swe-playwright -- sh -c 'exec mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT'
  claude mcp add --scope user --transport stdio swe-swe-preview -- sh -c 'exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp?key=$MCP_AUTH_KEY'
  claude mcp add --scope user --transport stdio swe-swe-whiteboard -- npx -y @choonkeat/agent-whiteboard
  claude mcp add --scope user --transport stdio swe-swe -- sh -c 'exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY'
}
claude_mcp_setup
echo -e "${GREEN}[ok] Created Claude MCP configuration${NC}"
fi

# Guard the built-in AskUserQuestion tool. Its multiple-choice menu renders
# only in the local terminal TUI, which is invisible to a user talking through
# the web chat UI (agent-chat) -- calling it there hangs the agent forever on
# input the user can never give. This PreToolUse hook blocks the tool (exit 2,
# which feeds stderr back to the agent so it switches to the send_message MCP
# tool) UNLESS the session opted out with AGENT_CHAT_DISABLE=1. swe-swe-server
# sets AGENT_CHAT_DISABLE=1 for non-chat (terminal) sessions where the TUI IS
# the user surface, and leaves it unset for agent-chat sessions. Fail-safe is
# block: a wrongly shown menu hard-hangs the agent, a wrongly blocked one just
# nudges it to send_message. Hooks are snapshotted at session start, so the
# env var (read at tool-call time) is the per-session knob; this file is static.
mkdir -p /home/app/.claude
CLAUDE_SETTINGS=/home/app/.claude/settings.json
cat > /tmp/swe-claude-settings.json << 'SETTINGSEOF'
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "AskUserQuestion",
        "hooks": [
          {
            "type": "command",
            "command": "[ \"$AGENT_CHAT_DISABLE\" = \"1\" ] && exit 0; echo 'BLOCKED: do not use the built-in AskUserQuestion tool -- its menu renders only in the local TUI, which the user may not see (e.g. an agent-chat session). Ask via the agent-chat send_message tool instead (question -> text, primary option -> first_quick_reply, rest -> more_quick_replies). To allow the built-in tool, set AGENT_CHAT_DISABLE=1.' >&2; exit 2"
          }
        ]
      }
    ]
  }
}
SETTINGSEOF
if [ -s "$CLAUDE_SETTINGS" ] && command -v jq >/dev/null 2>&1; then
  # Merge idempotently into existing settings: drop any prior AskUserQuestion
  # matcher, append ours, preserve every other key and PreToolUse entry.
  TMP_SETTINGS=$(mktemp)
  if jq --slurpfile add /tmp/swe-claude-settings.json \
       '.hooks.PreToolUse = (((.hooks.PreToolUse // []) | map(select(.matcher != "AskUserQuestion"))) + ($add[0].hooks.PreToolUse))' \
       "$CLAUDE_SETTINGS" > "$TMP_SETTINGS" 2>/dev/null; then
    mv "$TMP_SETTINGS" "$CLAUDE_SETTINGS"
  else
    # Existing file was not valid JSON; overwrite with our fragment.
    rm -f "$TMP_SETTINGS"
    cp /tmp/swe-claude-settings.json "$CLAUDE_SETTINGS"
  fi
elif [ -s "$CLAUDE_SETTINGS" ]; then
  # File exists but jq is unavailable: do not risk clobbering it.
  echo -e "${YELLOW}[warn] jq unavailable; left existing ~/.claude/settings.json untouched (AskUserQuestion guard not installed)${NC}"
else
  cp /tmp/swe-claude-settings.json "$CLAUDE_SETTINGS"
fi
rm -f /tmp/swe-claude-settings.json

echo -e "${GREEN}[ok] Installed AskUserQuestion guard hook${NC}"

# MCP-less steering: with no native MCP client, the agent must reach every tool
# through the `mcp` CLI (sockets in $SWE_MCP_DIR, one per server). The blocking
# send_message contract is the load-bearing rule -- run it, wait, and treat its
# stdout as the user's reply. Written to ~/.claude/CLAUDE.md (user memory).
if [ -n "$SWE_MCP_LESS" ]; then
mkdir -p /home/app/.claude
cat > /home/app/.claude/CLAUDE.md << 'MCPLESSEOF'
# MCP-less mode

This environment has NO MCP client. Reach every tool through the `mcp` CLI,
which mirrors the tool id `mcp__<server>__<tool>`:

    mcp                          # list servers (the socket dir is the registry)
    mcp <server>                 # full docs for every tool (what native MCP injects)
    mcp <server> <tool> -h       # full docs for one tool
    mcp <server> <tool> [--flags] # call the tool; its result prints to stdout

The -h output IS the tool's documentation -- a native MCP client would inject
it into your context automatically; here you must pull it. Never guess flags.

Talk to the user through agent-chat -- it is the ONLY channel the user sees:

- Start each turn with `mcp swe-swe-agent-chat check_messages`.
- EVERY user-visible message MUST go through send_message. Before your first
  send_message -- and again after any context compaction -- run
  `mcp swe-swe-agent-chat send_message -h` and follow it exactly.
- `send_message` BLOCKS until the user replies; the reply is RETURNED as the
  command's stdout. Never background it; end every turn on it.
- Non-blocking status: `mcp swe-swe-agent-chat send_progress --text "..."`.

Once the task at hand is clear (and when it changes), name this session so the
user can tell sessions apart: see `mcp swe-swe set_session_name -h`.
MCPLESSEOF

echo -e "${GREEN}[ok] Installed MCP-less agent steering (~/.claude/CLAUDE.md)${NC}"
fi

# Install Pi mcp-bridge extension into the global Pi config dir so every
# session in every workspace gets the swe-swe / agent-chat / playwright /
# preview / whiteboard MCPs without per-workspace setup. Pi prefers a
# project-local .pi/extensions/ override, so /workspace can still drop a
# custom mcp-bridge.ts to hack on it.
# mcp-less mode skips native MCP config (swe-swe-server runs the proxy fleet).
if [ -z "$SWE_MCP_LESS" ]; then
mkdir -p /home/app/.pi/agent/extensions
cp /tmp/pi-mcp-bridge.ts /home/app/.pi/agent/extensions/mcp-bridge.ts

echo -e "${GREEN}[ok] Installed Pi mcp-bridge extension${NC}"
fi

# Resolve internal server port. SWE_PORT is set by both compose (via the
# swe-swe service environment block) and dockerfile-only mode (via ENV in
# the generated Dockerfile), so the default is the same in either mode.
SWE_SERVER_PORT="${SWE_PORT:-1977}"
export SWE_SERVER_PORT

# Create open/xdg-open shims that route URLs to the Preview pane
mkdir -p /home/app/.swe-swe/bin
cat > /home/app/.swe-swe/bin/swe-swe-open << 'SHIM'
#!/bin/sh
URL="${1:-}"
[ -z "$URL" ] && exit 0
curl -sf "http://localhost:$SWE_SERVER_PORT/proxy/${SESSION_UUID}/preview/__agent-reverse-proxy-debug__/open?url=$(printf '%s' "$URL" | jq -sRr @uri)&key=$MCP_AUTH_KEY" >/dev/null 2>&1 &
echo "-> Preview: $URL" >&2
SHIM
chmod +x /home/app/.swe-swe/bin/swe-swe-open
for name in xdg-open open x-www-browser www-browser sensible-browser; do
    ln -sf swe-swe-open /home/app/.swe-swe/bin/$name
done
echo -e "${GREEN}[ok] Created open/xdg-open shims in .swe-swe/bin${NC}"

# Execute the original command directly (already running as app user)
# Use sh -c to expand shell variables in CMD arguments (e.g. ${SWE_PORT:-1977})
cd /workspace
exec sh -c "$*"
