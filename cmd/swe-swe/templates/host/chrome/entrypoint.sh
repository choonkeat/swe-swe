#!/bin/bash
set -e

# Enterprise Certificate Installation Entrypoint for Chrome container
# This script installs enterprise certificates into the system trust store
# before starting supervisord (which manages Chrome, VNC, etc.)
#
# Required for:
# - Chrome to trust enterprise HTTPS sites (corporate proxies, internal sites)
# - Chromium uses NSS/system CA store for certificate validation

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Install certificates if mounted
if [ -d /swe-swe/certs ] && [ "$(find /swe-swe/certs -type f -name '*.pem' 2>/dev/null | wc -l)" -gt 0 ]; then
    echo -e "${YELLOW}→ Installing enterprise certificates for Chrome...${NC}"

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

# Execute the original command (supervisord)
exec "$@"
