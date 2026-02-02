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


# Create Codex MCP configuration (TOML format)
mkdir -p /home/app/.codex
cat > /home/app/.codex/config.toml << 'EOF'
[mcp_servers.swe-swe-playwright]
command = "npx"
args = ["-y", "@playwright/mcp@latest", "--cdp-endpoint", "http://chrome:9223"]

[mcp_servers.swe-swe-preview]
command = "swe-swe-server"
args = ["--mcp"]
EOF
chown -R app: /home/app/.codex
echo -e "${GREEN}✓ Created Codex MCP configuration${NC}"



# Switch to app user and execute the original command
# Use exec to replace this process, preserving signal handling
exec su -s /bin/bash app -c "cd /workspace && exec $*"
