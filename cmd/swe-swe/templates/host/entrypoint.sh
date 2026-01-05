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

# Ensure .swe-swe/uploads directory exists and is writable by app user
# (the .swe-swe directory may have been created by a different user on the host)
if [ -d /workspace/.swe-swe ]; then
    mkdir -p /workspace/.swe-swe/uploads
    if chown -R app:app /workspace/.swe-swe 2>/dev/null; then
        echo -e "${GREEN}✓ Ensured .swe-swe directory is writable${NC}"
    else
        echo -e "${YELLOW}⚠ Warning: chown /workspace/.swe-swe failed; continuing anyway${NC}"
    fi
fi

# Prefer running as app user, but fall back to root if bind-mount permissions prevent it.
if su -s /bin/bash app -c "test -w /workspace && test -w /home/app" >/dev/null 2>&1; then
    exec su -s /bin/bash app -c "cd /workspace && exec $*"
fi

echo -e "${YELLOW}⚠ Warning: /workspace or /home/app not writable as app; running as root${NC}"
exec bash -lc "cd /workspace && exec $*"
