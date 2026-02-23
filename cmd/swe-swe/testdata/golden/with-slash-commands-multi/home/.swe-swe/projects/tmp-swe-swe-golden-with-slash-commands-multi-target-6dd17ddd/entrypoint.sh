#!/bin/bash
set -e
trap 'echo -e "\n\033[0;31m✗ Entrypoint failed at line $LINENO (exit code $?)\033[0m" >&2' ERR

# Enterprise Certificate Installation Entrypoint
# This script installs enterprise certificates into the system trust store
# (as root) before starting the main swe-swe-server process (as app user).
#
# Usage: This is automatically called when the container starts.
# The original CMD follows after certificate installation completes.

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Install certificates if mounted (must be root for this)
if [ -d /swe-swe/certs ] && [ "$(find /swe-swe/certs -type f -name '*.pem' 2>/dev/null | wc -l)" -gt 0 ]; then
    echo -e "${YELLOW}→ Installing enterprise certificates...${NC}"

    # Copy PEM files to system CA certificate directory
    if cp /swe-swe/certs/*.pem /usr/local/share/ca-certificates/ 2>/dev/null; then
        # Update CA certificate bundle
        if update-ca-certificates; then
            echo -e "${GREEN}✓ Enterprise certificates installed and trusted${NC}"
        else
            echo -e "${YELLOW}⚠ Warning: update-ca-certificates failed, continuing anyway${NC}"
        fi
    else
        echo -e "${YELLOW}⚠ Warning: No certificates to install${NC}"
    fi
fi


# Copy slash commands to agent directories
if [ -d "/home/app/.claude/commands/ck/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.claude/commands/ck 2>/dev/null || true
    su -s /bin/bash app -c "cd /home/app/.claude/commands/ck && git pull" 2>/dev/null && \
        echo -e "${GREEN}✓ Updated slash commands: ck (claude)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: ck (claude)${NC}"
elif [ -d "/tmp/slash-commands/ck" ]; then
    mkdir -p /home/app/.claude/commands
    cp -r /tmp/slash-commands/ck /home/app/.claude/commands/ck
    chown -R app:app /home/app/.claude/commands/ck
    echo -e "${GREEN}✓ Installed slash commands: ck (claude)${NC}"
fi
if [ -d "/home/app/.codex/prompts/ck/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.codex/prompts/ck 2>/dev/null || true
    su -s /bin/bash app -c "cd /home/app/.codex/prompts/ck && git pull" 2>/dev/null && \
        echo -e "${GREEN}✓ Updated slash commands: ck (codex)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: ck (codex)${NC}"
elif [ -d "/tmp/slash-commands/ck" ]; then
    mkdir -p /home/app/.codex/prompts
    cp -r /tmp/slash-commands/ck /home/app/.codex/prompts/ck
    chown -R app:app /home/app/.codex/prompts/ck
    echo -e "${GREEN}✓ Installed slash commands: ck (codex)${NC}"
fi
if [ -d "/home/app/.config/opencode/command/ck/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.config/opencode/command/ck 2>/dev/null || true
    su -s /bin/bash app -c "cd /home/app/.config/opencode/command/ck && git pull" 2>/dev/null && \
        echo -e "${GREEN}✓ Updated slash commands: ck (opencode)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: ck (opencode)${NC}"
elif [ -d "/tmp/slash-commands/ck" ]; then
    mkdir -p /home/app/.config/opencode/command
    cp -r /tmp/slash-commands/ck /home/app/.config/opencode/command/ck
    chown -R app:app /home/app/.config/opencode/command/ck
    echo -e "${GREEN}✓ Installed slash commands: ck (opencode)${NC}"
fi
if [ -d "/home/app/.claude/commands/org/team-cmds/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.claude/commands/org/team-cmds 2>/dev/null || true
    su -s /bin/bash app -c "cd /home/app/.claude/commands/org/team-cmds && git pull" 2>/dev/null && \
        echo -e "${GREEN}✓ Updated slash commands: org/team-cmds (claude)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: org/team-cmds (claude)${NC}"
elif [ -d "/tmp/slash-commands/org/team-cmds" ]; then
    mkdir -p /home/app/.claude/commands
    cp -r /tmp/slash-commands/org/team-cmds /home/app/.claude/commands/org/team-cmds
    chown -R app:app /home/app/.claude/commands/org/team-cmds
    echo -e "${GREEN}✓ Installed slash commands: org/team-cmds (claude)${NC}"
fi
if [ -d "/home/app/.codex/prompts/org/team-cmds/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.codex/prompts/org/team-cmds 2>/dev/null || true
    su -s /bin/bash app -c "cd /home/app/.codex/prompts/org/team-cmds && git pull" 2>/dev/null && \
        echo -e "${GREEN}✓ Updated slash commands: org/team-cmds (codex)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: org/team-cmds (codex)${NC}"
elif [ -d "/tmp/slash-commands/org/team-cmds" ]; then
    mkdir -p /home/app/.codex/prompts
    cp -r /tmp/slash-commands/org/team-cmds /home/app/.codex/prompts/org/team-cmds
    chown -R app:app /home/app/.codex/prompts/org/team-cmds
    echo -e "${GREEN}✓ Installed slash commands: org/team-cmds (codex)${NC}"
fi
if [ -d "/home/app/.config/opencode/command/org/team-cmds/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.config/opencode/command/org/team-cmds 2>/dev/null || true
    su -s /bin/bash app -c "cd /home/app/.config/opencode/command/org/team-cmds && git pull" 2>/dev/null && \
        echo -e "${GREEN}✓ Updated slash commands: org/team-cmds (opencode)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: org/team-cmds (opencode)${NC}"
elif [ -d "/tmp/slash-commands/org/team-cmds" ]; then
    mkdir -p /home/app/.config/opencode/command
    cp -r /tmp/slash-commands/org/team-cmds /home/app/.config/opencode/command/org/team-cmds
    chown -R app:app /home/app/.config/opencode/command/org/team-cmds
    echo -e "${GREEN}✓ Installed slash commands: org/team-cmds (opencode)${NC}"
fi

# Create OpenCode MCP configuration
# OpenCode uses a different schema: type="local" and command as array
mkdir -p /home/app/.config/opencode
cat > /home/app/.config/opencode/opencode.json << 'EOF'
{
  "mcp": {
    "swe-swe-agent-chat": {
      "type": "local",
      "command": ["npx", "-y", "@choonkeat/agent-chat"]
    },
    "swe-swe-playwright": {
      "type": "local",
      "command": ["npx", "-y", "@playwright/mcp@latest", "--cdp-endpoint", "http://chrome:9223"]
    },
    "swe-swe-preview": {
      "type": "local",
      "command": ["sh", "-c", "exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:9898/proxy/$SESSION_UUID/preview/mcp"]
    },
    "swe-swe-whiteboard": {
      "type": "local",
      "command": ["npx", "-y", "@choonkeat/agent-whiteboard"]
    }
  }
}
EOF
chown -R app: /home/app/.config/opencode
echo -e "${GREEN}✓ Created OpenCode MCP configuration${NC}"

# Create Codex MCP configuration (TOML format)
mkdir -p /home/app/.codex
cat > /home/app/.codex/config.toml << 'EOF'
[mcp_servers.swe-swe-agent-chat]
command = "npx"
args = ["-y", "@choonkeat/agent-chat"]

[mcp_servers.swe-swe-playwright]
command = "npx"
args = ["-y", "@playwright/mcp@latest", "--cdp-endpoint", "http://chrome:9223"]

[mcp_servers.swe-swe-preview]
command = "sh"
args = ["-c", "exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:9898/proxy/$SESSION_UUID/preview/mcp"]

[mcp_servers.swe-swe-whiteboard]
command = "npx"
args = ["-y", "@choonkeat/agent-whiteboard"]
EOF
chown -R app: /home/app/.codex
echo -e "${GREEN}✓ Created Codex MCP configuration${NC}"

# Create Gemini MCP configuration
mkdir -p /home/app/.gemini
cat > /home/app/.gemini/settings.json << 'EOF'
{
  "mcpServers": {
    "swe-swe-agent-chat": {
      "command": "npx",
      "args": ["-y", "@choonkeat/agent-chat"]
    },
    "swe-swe-playwright": {
      "command": "npx",
      "args": ["-y", "@playwright/mcp@latest", "--cdp-endpoint", "http://chrome:9223"]
    },
    "swe-swe-preview": {
      "command": "sh",
      "args": ["-c", "exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:9898/proxy/$SESSION_UUID/preview/mcp"]
    },
    "swe-swe-whiteboard": {
      "command": "npx",
      "args": ["-y", "@choonkeat/agent-whiteboard"]
    }
  }
}
EOF
chown -R app: /home/app/.gemini
echo -e "${GREEN}✓ Created Gemini MCP configuration${NC}"

# Create Goose MCP configuration (YAML format)
mkdir -p /home/app/.config/goose
cat > /home/app/.config/goose/config.yaml << 'EOF'
extensions:
  swe-swe-agent-chat:
    type: stdio
    cmd: npx
    args:
      - "-y"
      - "@choonkeat/agent-chat"
  swe-swe-playwright:
    type: stdio
    cmd: npx
    args:
      - "-y"
      - "@playwright/mcp@latest"
      - "--cdp-endpoint"
      - "http://chrome:9223"
  swe-swe-preview:
    type: stdio
    cmd: sh
    args:
      - "-c"
      - "exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:9898/proxy/$SESSION_UUID/preview/mcp"
  swe-swe-whiteboard:
    type: stdio
    cmd: npx
    args:
      - "-y"
      - "@choonkeat/agent-whiteboard"
EOF
chown -R app: /home/app/.config/goose
echo -e "${GREEN}✓ Created Goose MCP configuration${NC}"
# Wrapper: auto-run 'goose configure' if no provider is configured
cat > /home/app/.swe-swe/bin/goose << 'GOOSE_WRAPPER'
#!/bin/bash
GOOSE=/usr/local/bin/goose
$GOOSE "$@" || ($GOOSE configure && $GOOSE "$@")
GOOSE_WRAPPER
chmod +x /home/app/.swe-swe/bin/goose
echo -e "${GREEN}✓ Created Goose wrapper script${NC}"

# Create Claude MCP configuration (user scope = cross-project)
# Uses claude mcp add which writes to ~/.claude.json
# Must run as app user so config goes to /home/app/.claude.json (not /root/)
# Re-run if config is missing or stale (remove-then-add ensures fresh config)
if ! grep -q '"swe-swe-agent-chat"' /home/app/.claude.json 2>/dev/null || ! grep -q '\-\-bridge' /home/app/.claude.json 2>/dev/null; then
  su -s /bin/bash app -c '
    unset CLAUDECODE
    claude mcp remove --scope user swe-swe-agent-chat 2>/dev/null || true
    claude mcp remove --scope user swe-swe-playwright 2>/dev/null || true
    claude mcp remove --scope user swe-swe-preview 2>/dev/null || true
    claude mcp remove --scope user swe-swe-whiteboard 2>/dev/null || true
    claude mcp add --scope user --transport stdio swe-swe-agent-chat -- npx -y @choonkeat/agent-chat
    claude mcp add --scope user --transport stdio swe-swe-playwright -- npx -y @playwright/mcp@latest --cdp-endpoint http://chrome:9223
    claude mcp add --scope user --transport stdio swe-swe-preview -- sh -c '"'"'exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:9898/proxy/$SESSION_UUID/preview/mcp'"'"'
    claude mcp add --scope user --transport stdio swe-swe-whiteboard -- npx -y @choonkeat/agent-whiteboard
  '
  echo -e "${GREEN}✓ Created Claude MCP configuration${NC}"
fi

# Create open/xdg-open shims that route URLs to the Preview pane
mkdir -p /home/app/.swe-swe/bin
cat > /home/app/.swe-swe/bin/swe-swe-open << 'SHIM'
#!/bin/sh
URL="${1:-}"
[ -z "$URL" ] && exit 0
curl -sf "http://localhost:9898/proxy/${SESSION_UUID}/preview/__agent-reverse-proxy-debug__/open?url=$(printf '%s' "$URL" | jq -sRr @uri)" >/dev/null 2>&1 &
echo "→ Preview: $URL" >&2
SHIM
chmod +x /home/app/.swe-swe/bin/swe-swe-open
for name in xdg-open open x-www-browser www-browser sensible-browser; do
    ln -sf swe-swe-open /home/app/.swe-swe/bin/$name
done
chown -R app: /home/app/.swe-swe/bin
# Prepend .swe-swe/bin to PATH so shims override system commands.
# Uses /etc/profile.d/ so login shells (terminal, codex) pick it up
# after /etc/profile resets PATH.
echo 'export PATH="/home/app/.swe-swe/bin:$PATH"' > /etc/profile.d/swe-swe-path.sh
echo -e "${GREEN}✓ Created open/xdg-open shims in .swe-swe/bin${NC}"

# Switch to app user and execute the original command
# Use exec to replace this process, preserving signal handling
exec su -s /bin/bash app -c "cd /workspace && exec $*"
