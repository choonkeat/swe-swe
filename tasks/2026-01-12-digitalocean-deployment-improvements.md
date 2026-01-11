# DigitalOcean Deployment Improvements - Implementation Plan

**Date**: 2026-01-12
**Status**: Planning Phase
**Goal**: Enhance DigitalOcean 1-Click deployment with better password handling, startup visibility, and optional security hardening.

---

## High-Level Overview

Three implementation phases:
1. **Password Management** - Non-echoing password prompt with confirmation
2. **MOTD Health Check** - Two-phase MOTD with startup progress indicator
3. **Hardening Options** - User-prompted hardening level selection (None/Moderate/Comprehensive)

---

## Phase 1: Password Management

### What will be achieved
- When user runs `make deploy/digitalocean`, they're prompted for a password
- Password input doesn't echo to terminal
- Empty input (just pressing Enter) is treated as invalid - user is re-prompted
- Confirmation prompt (also non-echoing) validates passwords match
- If mismatch, user is re-prompted from the beginning
- Password is passed to Packer as a variable and made available to provisioning scripts

### Small implementation steps

1. Read existing `Makefile` to understand `deploy/digitalocean` target structure
2. Create a new shell helper script (`scripts/prompt-password.sh`) that:
   - Loop: Prompts "Enter swe-swe password:" with no echo (using `read -s`)
   - If empty, print error "Password cannot be empty" and loop back
   - If non-empty, prompt "Confirm password:" with no echo
   - If empty confirmation, print error and loop back
   - If passwords don't match, print error and loop back
   - If they match, output the password and exit
3. Modify `Makefile` `deploy/digitalocean` target to:
   - Call the password helper script, capture result in variable
   - Pass result to Packer as `-var swe_swe_password=<password>`
4. Verify Packer is correctly receiving the variable by checking Packer logs
5. Verify the password variable is being passed to provisioning scripts

### Verification
Manual testing:
- Run `make deploy/digitalocean` and test empty input → re-prompt
- Test password mismatch → re-prompt
- Test valid entry → proceeds to Packer
- Check Packer logs confirm variable was passed

---

## Phase 2: MOTD Health Check

### What will be achieved
- At first boot, 001_onboot script writes a "Initialization in Progress" MOTD to `/etc/motd`
- MOTD includes estimated time remaining and instructions for monitoring logs
- 001_onboot script starts a background health check loop that curls `https://localhost:1977`
- When curl succeeds (service is actually responding), MOTD is updated to "Ready" state with credentials and service URLs
- User sees progress indicator on SSH login, then final message after service is healthy

### Small implementation steps

1. Read current `deploy/digitalocean/files/var/lib/cloud/scripts/per-instance/001_onboot` to understand structure
2. Read current MOTD generation logic (if any exists)
3. Create Phase 1 MOTD template (initialization in progress message)
4. Create Phase 2 MOTD template (ready message with credentials and URLs)
5. Modify 001_onboot to:
   - Write Phase 1 MOTD at start of script (before any slow operations)
   - Extract password from environment (set by Packer/provisioning)
   - Start a background health check loop that:
     - Loop: curl -k -L https://localhost:1977 and check if swe-swe in response
     - Sleep 10 seconds between attempts
     - Add timeout/max iterations (e.g., fail after 20 minutes or 120 attempts)
     - When health check passes, write Phase 2 MOTD with actual credentials
6. Verify by deploying to DigitalOcean staging and checking MOTD transitions

### Verification
Manual testing:
- Deploy to DigitalOcean staging
- SSH in during initialization, confirm MOTD shows "Initialization in Progress"
- Monitor logs with: `ssh root@<ip> "tail -f /var/log/cloud-init-output.log"`
- After service is ready, reconnect SSH and confirm MOTD shows credentials and URLs

---

## Phase 3: Hardening Options

### What will be achieved
- When user runs `make deploy/digitalocean`, after password prompt, they're prompted to choose hardening level: None / Moderate (default) / Comprehensive
- **Moderate hardening** (~1-2 min): Disable root SSH, UFW firewall (allow 22, 1977), Fail2ban, auto-updates
- **Comprehensive hardening** (~2-5 min): All moderate items + auditd, aide, rkhunter, stronger SSH ciphers, sysctl hardening
- Packer receives a variable indicating which hardening scripts to include
- Provisioning includes appropriate hardening scripts based on user choice

### Small implementation steps

1. Read existing Packer configuration to understand how provisioning scripts are selected
2. Create `deploy/digitalocean/scripts/011-hardening-moderate.sh` with:
   - Disable root SSH login (PermitRootLogin no in sshd_config)
   - Configure UFW firewall (allow 22, 1977 only)
   - Install and configure Fail2ban (SSH bruteforce protection)
   - Enable auto-updates (apt-daily-upgrade.service)
3. Create `deploy/digitalocean/scripts/012-hardening-comprehensive.sh` with:
   - All items from moderate, plus:
   - Install auditd with basic audit rules
   - Install aide (file integrity monitoring)
   - Install rkhunter (intrusion detection)
   - Configure stronger SSH ciphers and key exchange algorithms
   - Apply sysctl hardening (kernel.kptr_restrict, kernel.dmesg_restrict, etc.)
4. Modify `scripts/prompt-password.sh` to also prompt for hardening level:
   - After password is confirmed, prompt: "Choose hardening level: (1) None  (2) Moderate (default)  (3) Comprehensive"
   - Return selection or default to Moderate
5. Modify `Makefile` `deploy/digitalocean` target to:
   - Capture hardening choice from prompt script
   - Pass to Packer as `-var hardening_level=<none|moderate|comprehensive>`
6. Modify Packer configuration to:
   - Conditionally include `011-hardening-moderate.sh` if hardening_level is moderate or comprehensive
   - Conditionally include `012-hardening-comprehensive.sh` if hardening_level is comprehensive
7. Manual verification of each hardening level

### Verification - Manual Commands

**For Moderate hardening:**
```bash
# Check UFW firewall is enabled and has correct rules
sudo ufw status

# Check Fail2ban is running
sudo systemctl status fail2ban
sudo fail2ban-client status

# Check root SSH is disabled
ssh -v root@<ip> 2>&1 | grep -i "permission denied\|auth"

# Check auto-updates
systemctl status apt-daily-upgrade.service
```

**For Comprehensive hardening (all above plus):**
```bash
# Check auditd is running
sudo systemctl status auditd
sudo auditctl -l | head

# Check aide is installed and initialized
sudo aide --version
sudo aide --check

# Check rkhunter is installed
rkhunter --version
sudo rkhunter --check --report-warnings-only

# Check SSH ciphers
ssh -Q cipher localhost

# Check sysctl hardening parameters
sysctl kernel.kptr_restrict
sysctl kernel.dmesg_restrict
```

---

## Files to Modify/Create

- `scripts/prompt-password.sh` (new) - Password and hardening level prompts
- `Makefile` - Modify `deploy/digitalocean` target
- `deploy/digitalocean/files/var/lib/cloud/scripts/per-instance/001_onboot` - MOTD + health check
- `deploy/digitalocean/scripts/011-hardening-moderate.sh` (new)
- `deploy/digitalocean/scripts/012-hardening-comprehensive.sh` (new)
- Packer configuration - Conditional script inclusion based on hardening level

---

## Implementation Progress

### Phase 1: ✅ COMPLETED
- [x] Read existing Makefile to understand deploy/digitalocean target
- [x] Created scripts/prompt-password.sh with non-echoing password prompts
- [x] Modified Makefile deploy/digitalocean target to call password prompt
- [x] Password variable passed to Packer as `-var swe_swe_password=<password>`

### Phase 2: ✅ COMPLETED
- [x] Read current 001_onboot provisioning script
- [x] Create Phase 1 MOTD template (initialization in progress)
- [x] Create Phase 2 MOTD template (ready with credentials)
- [x] Modify 001_onboot to write initial MOTD at script start
- [x] Add background health check loop with timeout (120 attempts, 10s intervals)
- [x] Update MOTD to Phase 2 when health check passes
- [x] Modified 001_onboot to use Packer swe_swe_password variable if provided

### Phase 3: ✅ COMPLETED
- [x] Read Packer configuration for script selection
- [x] Added swe_swe_password and hardening_level variables to Packer template
- [x] Created 011-hardening-moderate.sh with UFW, Fail2ban, SSH hardening, auto-updates
- [x] Created 012-hardening-comprehensive.sh with auditd, AIDE, rkhunter, sysctl hardening
- [x] Extended password prompt script to include hardening level selection (1-3)
- [x] Modified Makefile to capture both password and hardening level from prompt
- [x] Updated Packer provisioner to conditionally include hardening scripts based on level

## Implementation Order

1. **Phase 1**: Password management (self-contained, foundation for other phases) ✅
2. **Phase 2**: MOTD health check (works independently, improves UX) 🚧
3. **Phase 3**: Hardening options (integrates with Phases 1 & 2) ⏳

---

## Notes

- All manual verification to be performed by user on DigitalOcean staging droplet
- Hardening timing estimates (1-2 min moderate, 2-5 min comprehensive) to be validated during implementation
- All credentials (passwords) passed through environment variables, not baked into image
