#!/bin/bash
set -e

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





# Create Claude MCP configuration (user scope = cross-project)
# Uses claude mcp add which writes to ~/.claude.json
# Must run as app user so config goes to /home/app/.claude.json (not /root/)
# Skip if already configured (idempotent on container restart)
if ! grep -q '"swe-swe-agent-chat"' /home/app/.claude.json 2>/dev/null; then
  su -s /bin/bash app -c '
    unset CLAUDECODE
    claude mcp add --scope user --transport stdio swe-swe-agent-chat -- npx -y @choonkeat/agent-chat
    claude mcp add --scope user --transport stdio swe-swe-playwright -- npx -y @playwright/mcp@latest --cdp-endpoint http://chrome:9223
    claude mcp add --scope user --transport stdio swe-swe-preview -- npx -y @choonkeat/agent-reverse-proxy --tool-prefix preview --theme-cookie swe-swe-theme
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
PREVIEW_PORT=$(( 20000 + ${PORT:-3000} ))
curl -sf "http://localhost:${PREVIEW_PORT}/__agent-reverse-proxy-debug__/open?url=$(printf '%s' "$URL" | jq -sRr @uri)" >/dev/null 2>&1 &
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
