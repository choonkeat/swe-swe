#!/bin/bash
# Configure systemd service for swe-swe
set -euo pipefail

echo "==> Configuring systemd service..."

# Create swe-swe user if it doesn't exist (UID 1000 to match container)
if ! id swe-swe &>/dev/null; then
    echo "Creating swe-swe user..."
    useradd -m -u 1000 -s /bin/bash swe-swe
else
    echo "swe-swe user already exists"
fi

# Create config directory
mkdir -p /etc/swe-swe
chown swe-swe:swe-swe /etc/swe-swe
chmod 755 /etc/swe-swe

# Ensure workspace directory exists and has proper ownership
mkdir -p /workspace
chown swe-swe:swe-swe /workspace
chmod 755 /workspace

# Add swe-swe user to docker group so it can access Docker socket
# (Docker group is created by docker.sh script)
usermod -aG docker swe-swe || true

# Reload systemd to pick up the new service file
systemctl daemon-reload

# Enable service to start on boot (don't start yet - happens at first boot)
systemctl enable swe-swe.service

echo "==> Systemd configuration complete"
