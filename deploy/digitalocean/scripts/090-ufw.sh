#!/bin/bash
# Configure UFW firewall
set -euo pipefail

echo "==> Configuring firewall..."

# Allow SSH (port 22)
ufw allow 22/tcp

# Allow swe-swe (port 1977)
ufw allow 1977/tcp

# Enable firewall (non-interactive)
ufw --force enable

# Show status
ufw status verbose

echo "==> Firewall configuration complete"
