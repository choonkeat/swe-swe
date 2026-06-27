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


# Copy slash commands to agent directories
if [ -d "/home/app/.swe-swe/commands/md/ck/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.swe-swe/commands/md/ck 2>/dev/null || true
    (cd /home/app/.swe-swe/commands/md/ck && git pull) 2>/dev/null && \
        echo -e "${GREEN}[ok] Updated slash commands: ck (swe-swe store)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: ck (swe-swe store)${NC}"
elif [ -d "/tmp/slash-commands/ck" ]; then
    mkdir -p "$(dirname "/home/app/.swe-swe/commands/md/ck")"
    cp -r /tmp/slash-commands/ck /home/app/.swe-swe/commands/md/ck
    echo -e "${GREEN}[ok] Installed slash commands: ck (swe-swe store)${NC}"
fi
if [ -e "/home/app/.claude/commands/ck" ] && [ ! -L "/home/app/.claude/commands/ck" ]; then
    echo -e "${YELLOW}⚠ Slash command target exists and is not a symlink, leaving unchanged: /home/app/.claude/commands/ck (claude)${NC}"
elif [ -d "/home/app/.swe-swe/commands/md/ck" ]; then
    mkdir -p "$(dirname "/home/app/.claude/commands/ck")"
    ln -sfn /home/app/.swe-swe/commands/md/ck /home/app/.claude/commands/ck
    echo -e "${GREEN}[ok] Linked slash commands: ck (claude)${NC}"
fi
if [ -e "/home/app/.codex/prompts/ck" ] && [ ! -L "/home/app/.codex/prompts/ck" ]; then
    echo -e "${YELLOW}⚠ Slash command target exists and is not a symlink, leaving unchanged: /home/app/.codex/prompts/ck (codex)${NC}"
elif [ -d "/home/app/.swe-swe/commands/md/ck" ]; then
    mkdir -p "$(dirname "/home/app/.codex/prompts/ck")"
    ln -sfn /home/app/.swe-swe/commands/md/ck /home/app/.codex/prompts/ck
    echo -e "${GREEN}[ok] Linked slash commands: ck (codex)${NC}"
fi
if [ -e "/home/app/.config/opencode/command/ck" ] && [ ! -L "/home/app/.config/opencode/command/ck" ]; then
    echo -e "${YELLOW}⚠ Slash command target exists and is not a symlink, leaving unchanged: /home/app/.config/opencode/command/ck (opencode)${NC}"
elif [ -d "/home/app/.swe-swe/commands/md/ck" ]; then
    mkdir -p "$(dirname "/home/app/.config/opencode/command/ck")"
    ln -sfn /home/app/.swe-swe/commands/md/ck /home/app/.config/opencode/command/ck
    echo -e "${GREEN}[ok] Linked slash commands: ck (opencode)${NC}"
fi
if [ -e "/home/app/.pi/agent/prompts/ck" ] && [ ! -L "/home/app/.pi/agent/prompts/ck" ]; then
    echo -e "${YELLOW}⚠ Slash command target exists and is not a symlink, leaving unchanged: /home/app/.pi/agent/prompts/ck (pi)${NC}"
elif [ -d "/home/app/.swe-swe/commands/md/ck" ]; then
    mkdir -p "$(dirname "/home/app/.pi/agent/prompts/ck")"
    ln -sfn /home/app/.swe-swe/commands/md/ck /home/app/.pi/agent/prompts/ck
    echo -e "${GREEN}[ok] Linked slash commands: ck (pi)${NC}"
fi
if [ -d "/home/app/.swe-swe/commands/md/org/team-cmds/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.swe-swe/commands/md/org/team-cmds 2>/dev/null || true
    (cd /home/app/.swe-swe/commands/md/org/team-cmds && git pull) 2>/dev/null && \
        echo -e "${GREEN}[ok] Updated slash commands: org/team-cmds (swe-swe store)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: org/team-cmds (swe-swe store)${NC}"
elif [ -d "/tmp/slash-commands/org/team-cmds" ]; then
    mkdir -p "$(dirname "/home/app/.swe-swe/commands/md/org/team-cmds")"
    cp -r /tmp/slash-commands/org/team-cmds /home/app/.swe-swe/commands/md/org/team-cmds
    echo -e "${GREEN}[ok] Installed slash commands: org/team-cmds (swe-swe store)${NC}"
fi
if [ -e "/home/app/.claude/commands/org/team-cmds" ] && [ ! -L "/home/app/.claude/commands/org/team-cmds" ]; then
    echo -e "${YELLOW}⚠ Slash command target exists and is not a symlink, leaving unchanged: /home/app/.claude/commands/org/team-cmds (claude)${NC}"
elif [ -d "/home/app/.swe-swe/commands/md/org/team-cmds" ]; then
    mkdir -p "$(dirname "/home/app/.claude/commands/org/team-cmds")"
    ln -sfn /home/app/.swe-swe/commands/md/org/team-cmds /home/app/.claude/commands/org/team-cmds
    echo -e "${GREEN}[ok] Linked slash commands: org/team-cmds (claude)${NC}"
fi
if [ -e "/home/app/.codex/prompts/org/team-cmds" ] && [ ! -L "/home/app/.codex/prompts/org/team-cmds" ]; then
    echo -e "${YELLOW}⚠ Slash command target exists and is not a symlink, leaving unchanged: /home/app/.codex/prompts/org/team-cmds (codex)${NC}"
elif [ -d "/home/app/.swe-swe/commands/md/org/team-cmds" ]; then
    mkdir -p "$(dirname "/home/app/.codex/prompts/org/team-cmds")"
    ln -sfn /home/app/.swe-swe/commands/md/org/team-cmds /home/app/.codex/prompts/org/team-cmds
    echo -e "${GREEN}[ok] Linked slash commands: org/team-cmds (codex)${NC}"
fi
if [ -e "/home/app/.config/opencode/command/org/team-cmds" ] && [ ! -L "/home/app/.config/opencode/command/org/team-cmds" ]; then
    echo -e "${YELLOW}⚠ Slash command target exists and is not a symlink, leaving unchanged: /home/app/.config/opencode/command/org/team-cmds (opencode)${NC}"
elif [ -d "/home/app/.swe-swe/commands/md/org/team-cmds" ]; then
    mkdir -p "$(dirname "/home/app/.config/opencode/command/org/team-cmds")"
    ln -sfn /home/app/.swe-swe/commands/md/org/team-cmds /home/app/.config/opencode/command/org/team-cmds
    echo -e "${GREEN}[ok] Linked slash commands: org/team-cmds (opencode)${NC}"
fi
if [ -e "/home/app/.pi/agent/prompts/org/team-cmds" ] && [ ! -L "/home/app/.pi/agent/prompts/org/team-cmds" ]; then
    echo -e "${YELLOW}⚠ Slash command target exists and is not a symlink, leaving unchanged: /home/app/.pi/agent/prompts/org/team-cmds (pi)${NC}"
elif [ -d "/home/app/.swe-swe/commands/md/org/team-cmds" ]; then
    mkdir -p "$(dirname "/home/app/.pi/agent/prompts/org/team-cmds")"
    ln -sfn /home/app/.swe-swe/commands/md/org/team-cmds /home/app/.pi/agent/prompts/org/team-cmds
    echo -e "${GREEN}[ok] Linked slash commands: org/team-cmds (pi)${NC}"
fi


# Create OpenCode MCP configuration
# OpenCode uses a different schema: type="local" and command as array
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
      "command": ["sh", "-c", "exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp"]
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

# Create Codex MCP configuration (TOML format)
# Codex sandboxes MCP child processes and only forwards env vars listed in
# `env_vars` -- so we cannot use the `sh -c "exec npx ... $VAR"` wrapper that
# the other agents use, since $VAR would expand to empty inside the sandbox.
# Instead we run npx (or mcp-lazy-init) directly and let Codex substitute
# $VAR references in args from the declared env_vars whitelist.
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
args = ["-y", "@choonkeat/agent-reverse-proxy", "--bridge", "http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp"]
env_vars = ["SWE_SERVER_PORT", "SESSION_UUID"]

[mcp_servers.swe-swe-whiteboard]
command = "npx"
args = ["-y", "@choonkeat/agent-whiteboard"]

[mcp_servers.swe-swe]
command = "npx"
args = ["-y", "@choonkeat/agent-reverse-proxy", "--bridge", "http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY"]
env_vars = ["SWE_SERVER_PORT", "MCP_AUTH_KEY"]
EOF

echo -e "${GREEN}[ok] Created Codex MCP configuration${NC}"

# Create Gemini MCP configuration
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
      "args": ["-c", "exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp"]
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

# Create Goose MCP configuration (YAML format)
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
      - "exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp"
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
claude_mcp_setup() {
  unset CLAUDECODE
  claude mcp remove --scope user swe-swe-agent-chat 2>/dev/null || true
  claude mcp remove --scope user swe-swe-playwright 2>/dev/null || true
  claude mcp remove --scope user swe-swe-preview 2>/dev/null || true
  claude mcp remove --scope user swe-swe-whiteboard 2>/dev/null || true
  claude mcp remove --scope user swe-swe 2>/dev/null || true
  claude mcp add --scope user --transport stdio swe-swe-agent-chat -- sh -c 'exec npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY'
  claude mcp add --scope user --transport stdio swe-swe-playwright -- sh -c 'exec mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT'
  claude mcp add --scope user --transport stdio swe-swe-preview -- sh -c 'exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp'
  claude mcp add --scope user --transport stdio swe-swe-whiteboard -- npx -y @choonkeat/agent-whiteboard
  claude mcp add --scope user --transport stdio swe-swe -- sh -c 'exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY'
}
claude_mcp_setup
echo -e "${GREEN}[ok] Created Claude MCP configuration${NC}"

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

# Install Pi mcp-bridge extension into the global Pi config dir so every
# session in every workspace gets the swe-swe / agent-chat / playwright /
# preview / whiteboard MCPs without per-workspace setup. Pi prefers a
# project-local .pi/extensions/ override, so /workspace can still drop a
# custom mcp-bridge.ts to hack on it.
mkdir -p /home/app/.pi/agent/extensions
cp /tmp/pi-mcp-bridge.ts /home/app/.pi/agent/extensions/mcp-bridge.ts

echo -e "${GREEN}[ok] Installed Pi mcp-bridge extension${NC}"

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
