#!/bin/bash
# Cleanup script to prepare image for DigitalOcean Marketplace
# Removes sensitive data and prepares for first-boot
set -euo pipefail

echo "==> Running marketplace cleanup..."

# Remove SSH host keys (regenerated on first boot)
rm -f /etc/ssh/ssh_host_*

# Remove root's authorized_keys (user will set their own)
rm -f /root/.ssh/authorized_keys

# Clear bash history
rm -f /root/.bash_history
history -c || true

# Remove temporary files
rm -rf /tmp/*
rm -rf /var/tmp/*

# Clear apt cache
apt-get clean
rm -rf /var/lib/apt/lists/*

# Clear cloud-init state (allows re-run on new instance)
rm -rf /var/lib/cloud/instances/*
rm -rf /var/lib/cloud/instance
rm -rf /var/lib/cloud/data/*

# Clear log files
find /var/log -type f -name "*.log" -delete
find /var/log -type f -name "*.gz" -delete
truncate -s 0 /var/log/wtmp || true
truncate -s 0 /var/log/lastlog || true

# Remove machine-id (regenerated on first boot)
truncate -s 0 /etc/machine-id
rm -f /var/lib/dbus/machine-id

# Ensure MOTD script is executable
chmod +x /etc/update-motd.d/99-swe-swe

echo "==> Cleanup complete"
