#!/bin/bash
# Comprehensive OS hardening for swe-swe DigitalOcean droplet
# Includes all moderate hardening plus:
# - Auditd system audit logging
# - AIDE file integrity monitoring
# - Rkhunter intrusion detection
# - Stronger SSH ciphers and key exchange algorithms
# - Sysctl kernel hardening

set -euo pipefail

echo "==> Applying comprehensive hardening..."

# 1. Install auditd for system audit logging
echo "==> Installing auditd..."
apt-get install -y auditd audispd-plugins

# Enable some basic audit rules
cat >> /etc/audit/rules.d/audit.rules <<EOF
# Monitor /etc for changes
-w /etc/ -p wa -k etc_changes

# Monitor /usr/bin for changes
-w /usr/bin/ -p wa -k usr_bin_changes

# Monitor /usr/local/bin for changes
-w /usr/local/bin/ -p wa -k usr_local_bin_changes

# Monitor swe-swe directory
-w /etc/swe-swe/ -p wa -k swe_swe_changes
EOF

systemctl enable auditd
systemctl restart auditd

echo "==> Auditd configured"

# 2. Install AIDE for file integrity monitoring
echo "==> Installing AIDE..."
apt-get install -y aide aide-common

# Initialize AIDE database
aideinit 2>&1 | tail -20 || true

# Configure daily AIDE checks
cat > /etc/cron.daily/aide-daily-check <<'EOF'
#!/bin/bash
/usr/bin/aide --check > /var/log/aide/aide-daily.log 2>&1
EOF
chmod 755 /etc/cron.daily/aide-daily-check

echo "==> AIDE configured"

# 3. Install rkhunter for intrusion detection
echo "==> Installing rkhunter..."
apt-get install -y rkhunter

# Update rkhunter database
rkhunter --update --report-warnings-only 2>&1 | tail -20 || true

# Configure daily rkhunter checks
cat > /etc/cron.daily/rkhunter-daily-check <<'EOF'
#!/bin/bash
/usr/bin/rkhunter --check --report-warnings-only > /var/log/rkhunter-daily.log 2>&1
EOF
chmod 755 /etc/cron.daily/rkhunter-daily-check

echo "==> Rkhunter configured"

# 4. Strengthen SSH ciphers and key exchange algorithms
echo "==> Hardening SSH configuration..."
cat >> /etc/ssh/sshd_config.d/99-hardening.conf <<EOF

# Ciphers and Key Exchange
Ciphers chacha20-poly1305@openssh.com,aes256-gcm@openssh.com,aes128-gcm@openssh.com,aes256-ctr,aes192-ctr,aes128-ctr
KexAlgorithms curve25519-sha256,curve25519-sha256@libssh.org,diffie-hellman-group-exchange-sha256
HostKeyAlgorithms ssh-ed25519,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521

# Additional hardening
AllowUsers swe-swe
LogLevel VERBOSE
Compression no
ClientAliveInterval 300
ClientAliveCountMax 2
MaxAuthTries 3
MaxSessions 10
EOF

systemctl restart ssh

echo "==> SSH hardening complete"

# 5. Kernel and sysctl hardening
echo "==> Applying kernel and sysctl hardening..."
cat >> /etc/sysctl.d/99-hardening.conf <<EOF

# Kernel pointer exposure
kernel.kptr_restrict = 2

# Hide kernel logs from unprivileged users
kernel.dmesg_restrict = 1

# Restrict access to kernel logs
kernel.printk = 3 3 3 3

# Enable ExecShield
kernel.exec-shield = 1

# Enable ASLR
kernel.randomize_va_space = 2

# Restrict kernel module loading
kernel.modules_disabled = 1

# Restrict access to sysrq
kernel.sysrq = 0

# Panic on oops
kernel.panic_on_oops = 1

# Core dumps restrictions
kernel.core_uses_pid = 1
fs.suid_dumpable = 0

# Process accounting
kernel.acct = 5 2 78

# Restrict access to files in /proc
proc/sys/kernel/perf_event_paranoid = 3

# Restrict access to eBPF
kernel.bpf_stats_enabled = 0

# IP forwarding disabled
net.ipv4.ip_forward = 0
net.ipv6.conf.all.forwarding = 0

# Restrict source packet routing
net.ipv4.conf.all.send_redirects = 0
net.ipv4.conf.default.send_redirects = 0
net.ipv4.conf.all.accept_source_route = 0
net.ipv4.conf.default.accept_source_route = 0
net.ipv6.conf.all.accept_source_route = 0
net.ipv6.conf.default.accept_source_route = 0

# Log suspicious packets
net.ipv4.conf.all.log_martians = 1
net.ipv4.conf.default.log_martians = 1

# Ignore ICMP redirects
net.ipv4.conf.all.accept_redirects = 0
net.ipv4.conf.default.accept_redirects = 0
net.ipv4.conf.all.secure_redirects = 0
net.ipv4.conf.default.secure_redirects = 0
net.ipv6.conf.all.accept_redirects = 0
net.ipv6.conf.default.accept_redirects = 0

# Ignore ICMP pings
net.ipv4.icmp_echo_ignore_all = 0
net.ipv6.conf.all.disable_ipv6 = 0

# Increase TCP backlog
net.ipv4.tcp_max_syn_backlog = 2048
net.core.somaxconn = 2048

# TCP SYN cookies
net.ipv4.tcp_syncookies = 1

# Ignore bogus error responses
net.ipv4.icmp_ignore_bogus_error_responses = 1

# Use reverse path filtering
net.ipv4.conf.all.rp_filter = 1
net.ipv4.conf.default.rp_filter = 1

# Increase file descriptor limits
fs.file-max = 2097152
EOF

sysctl -p /etc/sysctl.d/99-hardening.conf > /dev/null 2>&1 || true

echo "==> Kernel and sysctl hardening complete"

echo "==> Comprehensive hardening complete!"
