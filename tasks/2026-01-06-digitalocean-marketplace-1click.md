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
- Size: `s-1vcpu-2gb` (minimum for swe-swe)
- Region: `nyc3` (default)
- SSH username: `root`
- Snapshot naming: `swe-swe-{{timestamp}}`

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

### Phase 4: Add Validation and Cleanup Scripts

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

**Steps**

| Step | Description |
|------|-------------|
| 5.1 | Set up `DIGITALOCEAN_API_TOKEN` environment variable |
| 5.2 | Run `packer init template.pkr.hcl` |
| 5.3 | Run `packer validate template.pkr.hcl` |
| 5.4 | Run `packer build template.pkr.hcl` |
| 5.5 | Deploy test Droplet from snapshot |
| 5.6 | Verify swe-swe is running |
| 5.7 | Verify MOTD shows credentials |
| 5.8 | Clean up test resources |

**Test Checklist**

| Test | Expected Result |
|------|-----------------|
| SSH into Droplet | MOTD displays URL + password |
| Visit `http://<IP>:1977` | swe-swe login page |
| Enter password | Access granted |
| `http://<IP>:1977/vscode` | VS Code loads |
| `http://<IP>:1977/chrome` | Chrome VNC loads |
| `systemctl status swe-swe` | Active (running) |
| `docker ps` | 5+ containers running |

**Cost Estimate**
- Packer build (~10 min): ~$0.02
- Test Droplet (~30 min): ~$0.01
- Snapshot storage: ~$0.05/GB/mo
- **Total: < $0.10**

---

### Phase 6: Documentation and Deploy Button

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
| 4 | Select **Read + Write** scope |
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
