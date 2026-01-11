#!/bin/bash
# Moderate OS hardening for swe-swe DigitalOcean droplet
# - Disable root SSH login
# - Configure UFW firewall
# - Install and configure Fail2ban
# - Enable auto-updates

set -euo pipefail

echo "==> Applying moderate hardening..."

# 1. Disable root SSH login - password auth already disabled by cloud-init
# Allow key-based auth for root to work via DigitalOcean console if needed
sed -i 's/^#PermitRootLogin yes/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config || true
sed -i 's/^PermitRootLogin yes/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config || true

# 2. Configure UFW firewall
echo "==> Configuring UFW firewall..."
apt-get update
apt-get install -y ufw

# Set default policies
ufw --force enable
ufw default deny incoming
ufw default allow outgoing

# Allow SSH (22) and swe-swe (1977)
ufw allow 22/tcp
ufw allow 1977/tcp

echo "==> UFW firewall configured"

# 3. Install and configure Fail2ban for SSH bruteforce protection
echo "==> Installing and configuring Fail2ban..."
apt-get install -y fail2ban

# Create local config to enable SSH jail
cat > /etc/fail2ban/jail.local <<EOF
[DEFAULT]
bantime = 3600
findtime = 600
maxretry = 5

[sshd]
enabled = true
port = ssh
logpath = %(sshd_log)s
maxretry = 3
EOF

systemctl enable fail2ban
systemctl restart fail2ban

echo "==> Fail2ban configured"

# 4. Enable auto-updates
echo "==> Enabling automatic security updates..."
apt-get install -y unattended-upgrades
systemctl enable apt-daily-upgrade.service
systemctl enable apt-daily.service

echo "==> Moderate hardening complete!"
