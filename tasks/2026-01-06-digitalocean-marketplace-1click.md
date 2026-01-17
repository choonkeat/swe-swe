# Task: DigitalOcean Marketplace 1-Click App for swe-swe

> **Date**: 2026-01-06
> **Status**: In Progress
> **Research**: [2026-01-06-deploy-button-hosting.md](../research/2026-01-06-deploy-button-hosting.md)

---

## Goal

Create a DigitalOcean Marketplace 1-Click App for swe-swe that allows users to:
1. Click a "Deploy to DigitalOcean" badge in the README
2. Sign up/log in to DigitalOcean
3. Select a Droplet size and deploy
4. Get a running swe-swe instance with credentials displayed

---

## Deliverables

```
deploy/digitalocean/
├── template.pkr.hcl              # Packer HCL config
├── scripts/
│   ├── 010-docker.sh             # Install Docker + Docker Compose
│   ├── 020-swe-swe.sh            # Install swe-swe binary
│   ├── 030-systemd.sh            # Enable systemd service
│   ├── 090-ufw.sh                # Configure firewall
│   └── 900-cleanup.sh            # Security cleanup
├── files/
│   ├── etc/
│   │   ├── update-motd.d/
│   │   │   └── 99-swe-swe        # Login message
│   │   └── systemd/system/
│   │       └── swe-swe.service   # Systemd unit file
│   └── var/
│       └── lib/cloud/scripts/per-instance/
│           └── 001_onboot        # First-boot initialization
└── README.md                     # Build instructions
```

---

## Phases

### Phase 1: Create Packer Template and Directory Structure [DONE]

**What Will Be Achieved**
A complete Packer HCL configuration that can build a DigitalOcean snapshot, plus the directory structure for all files that will be provisioned onto the Droplet.

**Steps**

| Step | Description |
|------|-------------|
| 1.1 | Create `deploy/digitalocean/` directory structure |
| 1.2 | Create `template.pkr.hcl` with DigitalOcean builder configuration |
| 1.3 | Configure Packer variables (API token, image name, application metadata) |
| 1.4 | Add file provisioner to copy `files/` to Droplet |
| 1.5 | Add shell provisioner to run installation scripts |
| 1.6 | Create placeholder scripts (empty files) to validate Packer syntax |

**Packer Configuration**
- Source: `ubuntu-24-04-x64`
- Size: `s-2vcpu-4gb` (2 vCPU, 4GB RAM - default, customizable)
- Region: **required** — must be specified via `-var "region=..."`
- Image version: **required** — must be specified via `-var "image_version=..."` (recommend using git tag or YYYYMMDD-SHA format)
- SSH username: `root`
- Snapshot naming: `swe-swe-{image_version}-{timestamp}`

**Verification**

| Test | Method |
|------|--------|
| Packer syntax valid | `packer validate template.pkr.hcl` |
| Packer can init plugins | `packer init template.pkr.hcl` |
| Directory structure correct | Manual inspection |

---

### Phase 2: Write Installation Scripts [DONE]

**What Will Be Achieved**
Shell scripts that install all required software onto the Droplet during the Packer build. These run at **image build time**, not at user deploy time.

**Steps**

| Step | Description |
|------|-------------|
| 2.1 | Create `scripts/010-docker.sh` - Install Docker Engine + Docker Compose plugin |
| 2.2 | Create `scripts/020-swe-swe.sh` - Download swe-swe binary |
| 2.3 | Create `scripts/030-systemd.sh` - Enable systemd service |
| 2.4 | Create `scripts/090-ufw.sh` - Configure firewall (ports 22, 1977) |
| 2.5 | Run `shellcheck` on all scripts |
| 2.6 | Optional: Test in local Docker container (Ubuntu 24.04) |

**Script Details**

`010-docker.sh`:
- Install Docker Engine via get.docker.com
- Install Docker Compose plugin (separate install on servers)
- Handle both amd64 and arm64 architectures
- Verify installation

`020-swe-swe.sh`:
- Detect architecture (x86_64 → amd64, aarch64 → arm64)
- Download from GitHub releases to /usr/local/bin/swe-swe
- Verify with `swe-swe --version`

`030-systemd.sh`:
- `systemctl daemon-reload`
- `systemctl enable swe-swe.service`
- Do NOT start (happens at first boot)

`090-ufw.sh`:
- Allow port 22 (SSH)
- Allow port 1977 (swe-swe)
- Enable firewall

**Verification**

| Test | Method |
|------|--------|
| Scripts are valid bash | `shellcheck scripts/*.sh` |
| Docker installs | Script includes version check |
| Docker Compose installs | Script includes version check |
| swe-swe binary works | Script includes version check |

---

### Phase 3: Write First-Boot Scripts [DONE]

**What Will Be Achieved**
Scripts and files that run when a **user deploys** the Droplet (not at image build time). This generates unique passwords, starts swe-swe, and shows the user their credentials.

**Steps**

| Step | Description |
|------|-------------|
| 3.1 | Create `files/var/lib/cloud/scripts/per-instance/001_onboot` |
| 3.2 | Create `files/etc/systemd/system/swe-swe.service` |
| 3.3 | Create `files/etc/update-motd.d/99-swe-swe` |
| 3.4 | Ensure `/etc/swe-swe/` directory is created for credentials |

**Script Details**

`001_onboot` (runs once per Droplet instance):
```bash
# 1. Generate random SWE_SWE_PASSWORD (16 chars)
# 2. Save to /etc/swe-swe/env
# 3. Get public IP from DO metadata API (169.254.169.254)
# 4. Run: swe-swe init --project-directory /workspace --agents=claude
# 5. Start: systemctl start swe-swe
# 6. Write credentials to /etc/swe-swe/credentials
# 7. Re-enable SSH if locked during build
```

`swe-swe.service`:
```ini
[Unit]
Description=swe-swe AI Development Environment
After=docker.service network-online.target
Requires=docker.service

[Service]
Type=simple
EnvironmentFile=/etc/swe-swe/env
WorkingDirectory=/workspace
ExecStart=/usr/local/bin/swe-swe up --project-directory /workspace
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

`99-swe-swe` (MOTD):
- Read credentials from /etc/swe-swe/credentials
- Display URL, password, and available services

**Verification**

| Test | Method |
|------|--------|
| 001_onboot syntax | `shellcheck` + `bash -n` |
| MOTD executable | File has +x permission |
| Full flow | Phase 5 integration test |

---

### Phase 4: Add Validation and Cleanup Scripts [DONE]

**What Will Be Achieved**
Scripts that prepare the image for marketplace submission by removing sensitive data and validating requirements.

**Steps**

| Step | Description |
|------|-------------|
| 4.1 | Create `scripts/900-cleanup.sh` |
| 4.2 | Download `img_check.sh` from DigitalOcean's marketplace-partners repo |
| 4.3 | Add cleanup script to Packer provisioners (runs last) |

**Cleanup Script Actions**
- Remove SSH host keys (regenerated on first boot)
- Remove root's authorized_keys
- Clear bash history
- Remove temporary files
- Clear cloud-init state
- Clear apt cache
- Clear log files
- Remove machine-id

**Verification**

| Test | Method |
|------|--------|
| Cleanup runs | Packer build succeeds |
| No SSH keys | `img_check.sh` passes |
| Cloud-init reset | `img_check.sh` passes |
| Firewall enabled | `img_check.sh` passes |

---

### Phase 5: Testing with Packer Build

**What Will Be Achieved**
A fully tested DigitalOcean snapshot that can be deployed as a Droplet with swe-swe running.

**Cost Estimate**
- Packer build (~10 min, `s-2vcpu-4gb`): ~$0.01
- Test Droplet (~30 min, `s-1vcpu-2gb`): ~$0.01
- Snapshot storage: ~$0.05/GB/month
- **Total: < $0.15** (or less if you delete test resources promptly)

---

#### Step 5.1: Install Packer

See https://developer.hashicorp.com/packer/install for the latest installation methods.

**On macOS with Homebrew:**
```bash
brew tap hashicorp/tap
brew install hashicorp/tap/packer
```

**On Linux (Ubuntu/Debian):**
```bash
wget -O - https://apt.releases.hashicorp.com/gpg | sudo gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(grep -oP '(?<=UBUNTU_CODENAME=).*' /etc/os-release || lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/hashicorp.list
sudo apt update && sudo apt install packer
```

**Verify installation:**
```bash
packer --version
# Should show v1.14.0 or higher
```

---

#### Step 5.2: Create a DigitalOcean Account

1. Go to https://www.digitalocean.com/
2. Click **Sign Up**
3. Create account with email or GitHub/Google
4. Add a payment method (required even for small charges)

---

#### Step 5.3: Generate a DigitalOcean API Token

1. Log in to DigitalOcean: https://cloud.digitalocean.com/
2. Click **API** in the left sidebar (or go to https://cloud.digitalocean.com/account/api/tokens)
3. Click **Generate New Token**
4. Enter a name: `packer-swe-swe`
5. Set expiration (e.g., 90 days)
6. **Scopes**: Select **Custom Scopes** (recommended for security)
   - Search for and select the following permissions:
     - `droplet:create` — Create temporary build Droplet (includes snapshot:read as required scope)
     - `droplet:read` — Monitor Droplet status during build
     - `droplet:delete` — Destroy temporary Droplet and create snapshot
     - `ssh_key:create` — Create temporary SSH key for Packer
     - `ssh_key:delete` — Remove temporary SSH key after build
7. Click **Generate Token**
8. **IMPORTANT**: Copy the token immediately - it's shown only once!

The token looks like: `dop_v1_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx`

> **Note**: Custom scopes follow the principle of least privilege. If you prefer simpler setup, you can use "Full Access" instead, but fine-grained permissions are recommended for security.

---

#### Step 5.4: Build the Image

Open a terminal and run these commands:

```bash
# Set the API token (paste your actual token)
export DIGITALOCEAN_API_TOKEN=dop_v1_your_token_here

# Navigate to the Packer directory
cd deploy/digitalocean

# Download the DigitalOcean Packer plugin
packer init template.pkr.hcl

# Validate the template (should show "The configuration is valid.")
packer validate template.pkr.hcl

# Generate dynamic image version (git tag + SHA, or YYYYMMDD + SHA)
IMAGE_VERSION=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || date +%Y%m%d)-$(git rev-parse --short HEAD)
echo "Building version: $IMAGE_VERSION"

# Build the image (takes ~10 minutes) with required variables
packer build \
  -var "region=nyc3" \
  -var "image_version=$IMAGE_VERSION" \
  template.pkr.hcl
```

Replace `nyc3` with your preferred region (e.g., `sfo3`, `lon1`, `sgp1`). See the [DigitalOcean regions API](https://docs.digitalocean.com/reference/api/list-regions/) for available options.

**What happens during build:**
1. Packer creates a temporary Droplet in the specified region
2. Runs all installation scripts (Docker, swe-swe, firewall, etc.)
3. Runs cleanup script to remove sensitive data
4. Creates a snapshot of the Droplet
5. Destroys the temporary Droplet
6. Outputs the snapshot ID

**Success output looks like:**
```
==> digitalocean.swe-swe: Gracefully shutting down droplet...
==> digitalocean.swe-swe: Creating snapshot: swe-swe-1.0.0-20260110-143052
==> digitalocean.swe-swe: Waiting for snapshot to complete...
==> digitalocean.swe-swe: Destroying droplet...
==> digitalocean.swe-swe: Deleting temporary ssh key...
Build 'digitalocean.swe-swe' finished after 10 minutes 23 seconds.
```

A `manifest.json` file is created with the snapshot ID.

---

#### Step 5.5: Create a Test Droplet from the Snapshot

1. Go to https://cloud.digitalocean.com/droplets
2. Click **Create** → **Droplets**
3. In **Choose an image**, click the **Snapshots** tab
4. Select the snapshot named `swe-swe-1.0.0-YYYYMMDD-HHMMSS`
5. **Choose Size**: Select **Basic** → **Regular** → **$12/mo** (`s-1vcpu-2gb` or larger)
6. **Choose Region**: Any region (e.g., New York 1)
7. **Authentication**: Choose **SSH Key** and select your key (or create one)
   - If you don't have an SSH key, click **New SSH Key** and follow the instructions
8. **Hostname**: Enter `swe-swe-test`
9. Click **Create Droplet**

Wait ~60 seconds for the Droplet to boot and run first-boot scripts.

---

#### Step 5.6: Get the Droplet IP Address

1. On the Droplets page, find your new `swe-swe-test` Droplet
2. Copy the **IP address** (e.g., `164.90.xxx.xxx`)

---

#### Step 5.7: SSH into the Droplet and Check MOTD

```bash
ssh root@164.90.xxx.xxx
```

**Expected output** (the MOTD - Message of the Day):
```
************************************************************
*                     swe-swe                              *
************************************************************

Your swe-swe instance is ready!

  URL:      http://164.90.xxx.xxx:1977
  Password: xxxxxxxxxxxxxxxx

To view this message again: cat /etc/swe-swe/credentials

************************************************************
```

If you see this, the first-boot script worked correctly.

---

#### Step 5.8: Test swe-swe Services

**In your browser:**

1. Visit `http://164.90.xxx.xxx:1977`
   - Should show swe-swe login page

2. Enter the password from the MOTD
   - Should grant access to the dashboard

3. Visit `http://164.90.xxx.xxx:1977/vscode`
   - Should load VS Code in browser

4. Visit `http://164.90.xxx.xxx:1977/chrome`
   - Should load Chrome VNC

**On the Droplet (via SSH):**

```bash
# Check swe-swe service status
systemctl status swe-swe
# Should show "active (running)"

# Check Docker containers
docker ps
# Should show 5+ containers running

# Check firewall
ufw status
# Should show ports 22 and 1977 allowed
```

---

#### Step 5.9: Run DigitalOcean Image Validation

While SSH'd into the Droplet:

```bash
sudo bash /var/lib/cloud/scripts/per-instance/99-img-check.sh 2>/dev/null || sudo bash ~/99-img-check.sh
```

**Note**: The validation script may not be at a standard location. Check with:
```bash
find / -name "*img*check*" 2>/dev/null
```

All checks should pass:
- ✅ No root password set
- ✅ No SSH keys in authorized_keys
- ✅ Bash history cleared
- ✅ Cloud-init installed
- ✅ Firewall active

---

#### Step 5.10: Clean Up Test Resources

**Delete the test Droplet:**

1. Go to https://cloud.digitalocean.com/droplets
2. Click on `swe-swe-test`
3. Click **Destroy** in the left sidebar
4. Click **Destroy this Droplet**
5. Type the Droplet name to confirm
6. Click **Destroy**

**Keep or delete the snapshot:**

- **If tests passed**: Keep the snapshot for marketplace submission
- **If tests failed**: Delete and rebuild after fixing issues

To delete a snapshot:
1. Go to https://cloud.digitalocean.com/images/snapshots/droplets
2. Click the **...** menu on your snapshot
3. Click **Delete**
4. Confirm deletion

---

#### Phase 5 Checklist

| # | Test | Expected Result | Pass? |
|---|------|-----------------|-------|
| 1 | `packer build` completes | Snapshot created, manifest.json generated | ☐ |
| 2 | SSH into Droplet | MOTD displays URL + password | ☐ |
| 3 | Visit `http://<IP>:1977` | Login page loads | ☐ |
| 4 | Enter password | Access granted | ☐ |
| 5 | Visit `http://<IP>:1977/vscode` | VS Code loads | ☐ |
| 6 | Visit `http://<IP>:1977/chrome` | Chrome VNC loads | ☐ |
| 7 | `systemctl status swe-swe` | Active (running) | ☐ |
| 8 | `docker ps` | 5+ containers running | ☐ |
| 9 | `ufw status` | Ports 22, 1977 allowed | ☐ |
| 10 | Image validation script | All checks pass | ☐ |

**If all pass**: Phase 5 complete! Mark as [DONE] and proceed to marketplace submission.

**If any fail**: Debug using logs:
```bash
# Cloud-init logs (first-boot script)
cat /var/log/cloud-init-output.log

# swe-swe service logs
journalctl -u swe-swe -n 50

# Docker logs
docker logs <container_name>
```

---

### Phase 6: Documentation and Deploy Button [DONE]

**What Will Be Achieved**
Complete documentation and a deploy button badge for the README.

**Steps**

| Step | Description |
|------|-------------|
| 6.1 | Create `deploy/digitalocean/README.md` |
| 6.2 | Document getting API token |
| 6.3 | Document build process |
| 6.4 | Document Vendor Portal submission |
| 6.5 | Create badge markdown for main README |
| 6.6 | Add troubleshooting section |

**Getting DigitalOcean API Token**

| Step | Action |
|------|--------|
| 1 | Go to https://cloud.digitalocean.com/account/api/tokens |
| 2 | Click "Generate New Token" |
| 3 | Name it (e.g., "packer-swe-swe") |
| 4 | Select **Custom Scopes** and grant: `droplet:create`, `droplet:read`, `droplet:delete`, `ssh_key:create`, `ssh_key:delete` |
| 5 | Copy the token (shown only once) |
| 6 | Export: `export DIGITALOCEAN_API_TOKEN=dop_v1_xxxxx` |

**Security Note**
The `DIGITALOCEAN_API_TOKEN` is used only by Packer on your local machine to communicate with the DigitalOcean API. It is never copied to or stored in the Droplet image.

**Deploy Button Badge**
```markdown
[![Deploy to DigitalOcean](https://www.deploytodo.com/do-btn-blue.svg)](https://cloud.digitalocean.com/droplets/new?image=swe-swe&size=s-1vcpu-2gb)
```

> Note: The actual image slug will be assigned after marketplace approval.

---

## Verification Summary

| Phase | Test Method |
|-------|-------------|
| 1 | `packer validate` + `packer init` |
| 2 | `shellcheck` + optional Docker dry-run |
| 3 | `shellcheck` + `bash -n` |
| 4 | `img_check.sh` validation |
| 5 | Full Packer build + Droplet test |
| 6 | Documentation review |

---

## No Regression Risk

All work is in a new `deploy/digitalocean/` directory. No existing swe-swe code is modified. The swe-swe binary is downloaded from GitHub releases during the build.

---

*Plan created: 2026-01-06*
