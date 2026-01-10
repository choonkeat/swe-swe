#!/bin/bash
# Configure systemd service for swe-swe
set -euo pipefail

echo "==> Configuring systemd service..."

# Create config directory
mkdir -p /etc/swe-swe

# Reload systemd to pick up the new service file
systemctl daemon-reload

# Enable service to start on boot (don't start yet - happens at first boot)
systemctl enable swe-swe.service

echo "==> Systemd configuration complete"
