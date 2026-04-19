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
if [ -d "/home/app/.claude/commands/choonkeat/slash-commands/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.claude/commands/choonkeat/slash-commands 2>/dev/null || true
    (cd /home/app/.claude/commands/choonkeat/slash-commands && git pull) 2>/dev/null && \
        echo -e "${GREEN}[ok] Updated slash commands: choonkeat/slash-commands (claude)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: choonkeat/slash-commands (claude)${NC}"
elif [ -d "/tmp/slash-commands/choonkeat/slash-commands" ]; then
    mkdir -p /home/app/.claude/commands
    cp -r /tmp/slash-commands/choonkeat/slash-commands /home/app/.claude/commands/choonkeat/slash-commands
    echo -e "${GREEN}[ok] Installed slash commands: choonkeat/slash-commands (claude)${NC}"
fi
if [ -d "/home/app/.codex/prompts/choonkeat/slash-commands/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.codex/prompts/choonkeat/slash-commands 2>/dev/null || true
    (cd /home/app/.codex/prompts/choonkeat/slash-commands && git pull) 2>/dev/null && \
        echo -e "${GREEN}[ok] Updated slash commands: choonkeat/slash-commands (codex)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: choonkeat/slash-commands (codex)${NC}"
elif [ -d "/tmp/slash-commands/choonkeat/slash-commands" ]; then
    mkdir -p /home/app/.codex/prompts
    cp -r /tmp/slash-commands/choonkeat/slash-commands /home/app/.codex/prompts/choonkeat/slash-commands
    echo -e "${GREEN}[ok] Installed slash commands: choonkeat/slash-commands (codex)${NC}"
fi
if [ -d "/home/app/.config/opencode/command/choonkeat/slash-commands/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.config/opencode/command/choonkeat/slash-commands 2>/dev/null || true
    (cd /home/app/.config/opencode/command/choonkeat/slash-commands && git pull) 2>/dev/null && \
        echo -e "${GREEN}[ok] Updated slash commands: choonkeat/slash-commands (opencode)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: choonkeat/slash-commands (opencode)${NC}"
elif [ -d "/tmp/slash-commands/choonkeat/slash-commands" ]; then
    mkdir -p /home/app/.config/opencode/command
    cp -r /tmp/slash-commands/choonkeat/slash-commands /home/app/.config/opencode/command/choonkeat/slash-commands
    echo -e "${GREEN}[ok] Installed slash commands: choonkeat/slash-commands (opencode)${NC}"
fi
if [ -d "/home/app/.pi/agent/prompts/choonkeat/slash-commands/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.pi/agent/prompts/choonkeat/slash-commands 2>/dev/null || true
    (cd /home/app/.pi/agent/prompts/choonkeat/slash-commands && git pull) 2>/dev/null && \
        echo -e "${GREEN}[ok] Updated slash commands: choonkeat/slash-commands (pi)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: choonkeat/slash-commands (pi)${NC}"
elif [ -d "/tmp/slash-commands/choonkeat/slash-commands" ]; then
    mkdir -p /home/app/.pi/agent/prompts
    cp -r /tmp/slash-commands/choonkeat/slash-commands /home/app/.pi/agent/prompts/choonkeat/slash-commands
    echo -e "${GREEN}[ok] Installed slash commands: choonkeat/slash-commands (pi)${NC}"
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
mkdir -p /home/app/.codex
cat > /home/app/.codex/config.toml << 'EOF'
[mcp_servers.swe-swe-agent-chat]
command = "sh"
args = ["-c", "exec npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY"]

[mcp_servers.swe-swe-playwright]
command = "sh"
args = ["-c", "exec mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT"]

[mcp_servers.swe-swe-preview]
command = "sh"
args = ["-c", "exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp"]

[mcp_servers.swe-swe-whiteboard]
command = "npx"
args = ["-y", "@choonkeat/agent-whiteboard"]

[mcp_servers.swe-swe]
command = "sh"
args = ["-c", "exec npx -y @choonkeat/agent-reverse-proxy --bridge 'http://localhost:$SWE_SERVER_PORT/mcp?key='$MCP_AUTH_KEY"]
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

# Resolve internal server port (SWE_PORT for dockerfile-only mode, 9898 for compose mode)
SWE_SERVER_PORT="${SWE_PORT:-9898}"
export SWE_SERVER_PORT

# Create open/xdg-open shims that route URLs to the Preview pane
mkdir -p /home/app/.swe-swe/bin
cat > /home/app/.swe-swe/bin/swe-swe-open << 'SHIM'
#!/bin/sh
URL="${1:-}"
[ -z "$URL" ] && exit 0
curl -sf "http://localhost:$SWE_SERVER_PORT/proxy/${SESSION_UUID}/preview/__agent-reverse-proxy-debug__/open?url=$(printf '%s' "$URL" | jq -sRr @uri)" >/dev/null 2>&1 &
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
