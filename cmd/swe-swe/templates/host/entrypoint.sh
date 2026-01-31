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

# {{IF DOCKER}}
# Add app user to docker socket's group for permission to use Docker CLI
if [ -S /var/run/docker.sock ]; then
    DOCKER_GID=$(stat -c '%g' /var/run/docker.sock)
    if ! getent group $DOCKER_GID > /dev/null 2>&1; then
        groupadd -g $DOCKER_GID docker-host
        echo -e "${GREEN}✓ Created docker-host group with GID $DOCKER_GID${NC}"
    fi
    usermod -aG $DOCKER_GID app
    echo -e "${GREEN}✓ Added app user to docker group (GID $DOCKER_GID)${NC}"
fi
# {{ENDIF}}

# {{IF SLASH_COMMANDS}}
# Copy slash commands to agent directories
{{SLASH_COMMANDS_COPY}}
# {{ENDIF}}

# {{IF OPENCODE}}
# Create OpenCode MCP configuration
# OpenCode uses a different schema: type="local" and command as array
mkdir -p /home/app/.config/opencode
cat > /home/app/.config/opencode/opencode.json << 'EOF'
{
  "mcp": {
    "swe-swe-playwright": {
      "type": "local",
      "command": ["npx", "-y", "@playwright/mcp@latest", "--cdp-endpoint", "http://chrome:9223"]
    }
  }
}
EOF
chown -R app: /home/app/.config/opencode
echo -e "${GREEN}✓ Created OpenCode MCP configuration${NC}"
# {{ENDIF}}

# {{IF CODEX}}
# Create Codex MCP configuration (TOML format)
mkdir -p /home/app/.codex
cat > /home/app/.codex/config.toml << 'EOF'
[mcp_servers.swe-swe-playwright]
command = "npx"
args = ["-y", "@playwright/mcp@latest", "--cdp-endpoint", "http://chrome:9223"]
EOF
chown -R app: /home/app/.codex
echo -e "${GREEN}✓ Created Codex MCP configuration${NC}"
# {{ENDIF}}

# {{IF GEMINI}}
# Create Gemini MCP configuration
mkdir -p /home/app/.gemini
cat > /home/app/.gemini/settings.json << 'EOF'
{
  "mcpServers": {
    "swe-swe-playwright": {
      "command": "npx",
      "args": ["-y", "@playwright/mcp@latest", "--cdp-endpoint", "http://chrome:9223"]
    }
  }
}
EOF
chown -R app: /home/app/.gemini
echo -e "${GREEN}✓ Created Gemini MCP configuration${NC}"
# {{ENDIF}}

# {{IF GOOSE}}
# Create Goose MCP configuration (YAML format)
mkdir -p /home/app/.config/goose
cat > /home/app/.config/goose/config.yaml << 'EOF'
extensions:
  swe-swe-playwright:
    type: stdio
    cmd: npx
    args:
      - "-y"
      - "@playwright/mcp@latest"
      - "--cdp-endpoint"
      - "http://chrome:9223"
EOF
chown -R app: /home/app/.config/goose
echo -e "${GREEN}✓ Created Goose MCP configuration${NC}"
# {{ENDIF}}

# Switch to app user and execute the original command
# Use exec to replace this process, preserving signal handling
exec su -s /bin/bash app -c "cd /workspace && exec $*"
