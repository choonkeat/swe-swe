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
if [ -e "/home/app/.codex/prompts/ck" ] && [ ! -L "/home/app/.codex/prompts/ck" ]; then
    echo -e "${YELLOW}⚠ Slash command target exists and is not a symlink, leaving unchanged: /home/app/.codex/prompts/ck (codex)${NC}"
elif [ -d "/home/app/.swe-swe/commands/md/ck" ]; then
    mkdir -p "$(dirname "/home/app/.codex/prompts/ck")"
    ln -sfn /home/app/.swe-swe/commands/md/ck /home/app/.codex/prompts/ck
    echo -e "${GREEN}[ok] Linked slash commands: ck (codex)${NC}"
fi



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
