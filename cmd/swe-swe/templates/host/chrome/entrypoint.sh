#!/bin/bash
set -e

# Chrome Container Entrypoint
# This script:
# 1. Copies noVNC wrapper to serve from /chrome basepath
# 2. Installs enterprise certificates into:
#    - System CA store (for curl, wget, and other tools)
#    - NSS database (for Chromium browser - it doesn't use system CA store)
#
# Required for:
# - noVNC to connect to WebSocket at correct /chrome/websockify path
# - Chrome to trust enterprise HTTPS sites (corporate proxies, internal sites)
# - Chromium uses NSS database at ~/.pki/nssdb for certificate validation

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Copy noVNC wrapper to serve from /chrome basepath with correct WebSocket path
if [ -f /app/novnc-wrapper.html ]; then
    cp /app/novnc-wrapper.html /usr/share/novnc/index.html
    echo -e "${GREEN}✓ noVNC wrapper installed for /chrome basepath${NC}"
fi

# Install certificates if mounted
if [ -d /swe-swe/certs ] && [ "$(find /swe-swe/certs -type f -name '*.pem' 2>/dev/null | wc -l)" -gt 0 ]; then
    echo -e "${YELLOW}→ Installing enterprise certificates for Chrome...${NC}"

    # 1. Install to system CA store (for curl, wget, etc.)
    if cp /swe-swe/certs/*.pem /usr/local/share/ca-certificates/ 2>/dev/null; then
        if update-ca-certificates 2>/dev/null; then
            echo -e "${GREEN}✓ System CA store updated${NC}"
        fi
    fi

    # 2. Install to NSS database (for Chromium browser)
    NSS_DB="/home/chrome/.pki/nssdb"
    mkdir -p "$NSS_DB"

    # Initialize NSS database if it doesn't exist
    if [ ! -f "$NSS_DB/cert9.db" ]; then
        certutil -d sql:"$NSS_DB" -N --empty-password 2>/dev/null
    fi

    # Add each certificate to NSS database
    for cert in /swe-swe/certs/*.pem; do
        if [ -f "$cert" ]; then
            certname=$(basename "$cert" .pem)
            # -A: add cert, -t "C,,": trusted CA for SSL
            if certutil -d sql:"$NSS_DB" -A -t "C,," -n "$certname" -i "$cert" 2>/dev/null; then
                echo -e "${GREEN}✓ Added $certname to NSS database${NC}"
            else
                echo -e "${YELLOW}⚠ Failed to add $certname to NSS database${NC}"
            fi
        fi
    done

    # Set ownership for chrome user
    chown -R chrome:chrome "$NSS_DB"

    echo -e "${GREEN}✓ Enterprise certificates installed${NC}"
fi

# Execute the original command (supervisord)
exec "$@"
