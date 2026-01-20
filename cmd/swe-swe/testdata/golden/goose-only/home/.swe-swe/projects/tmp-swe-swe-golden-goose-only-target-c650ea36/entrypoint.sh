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

# Ensure /worktrees directory exists and is owned by app user
# (bind mount from host may create it with root ownership)
if [ -d /worktrees ]; then
    chown app: /worktrees
fi

# Switch to app user and execute the original command
# Use exec to replace this process, preserving signal handling
exec su -s /bin/bash app -c "cd /workspace && exec $*"
