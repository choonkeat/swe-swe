# Research: One-Click Deploy Button for swe-swe

> **Date**: 2026-01-06
> **Status**: Research Complete
> **Goal**: Identify hosting platforms that support Heroku-style "Deploy to X" buttons for swe-swe

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Requirements Analysis](#requirements-analysis)
3. [Platform Categories](#platform-categories)
4. [Detailed Platform Analysis](#detailed-platform-analysis)
   - [Tier 1: VM-Based with Deploy Buttons](#tier-1-vm-based-with-deploy-buttons-recommended)
   - [Tier 2: Self-Hosted PaaS](#tier-2-self-hosted-paas)
   - [Tier 3: Container Platforms](#tier-3-container-platforms-not-viable)
   - [Tier 4: Enterprise/Cloud Providers](#tier-4-enterprisecloud-providers)
5. [Technical Deep Dives](#technical-deep-dives)
6. [Comparison Matrix](#comparison-matrix)
7. [Recommendations](#recommendations)
8. [Implementation Roadmap](#implementation-roadmap)
9. [Sources](#sources)

---

## Executive Summary

### The Heroku Model We Want to Replicate

Heroku pioneered the one-click deploy button:
1. User sees a badge in a GitHub README
2. Clicks the badge → redirected to Heroku
3. Signs up or logs in
4. One more click → app is deployed
5. User gets a running URL

We want this exact experience for swe-swe.

### Critical Constraint: swe-swe Requires a VM with Docker

swe-swe's architecture requires:
- `swe-swe init` generates Docker Compose files and configs
- `swe-swe up` runs `docker compose up` on the **host's Docker daemon**
- The spawned containers run on the host, not nested inside another container

This eliminates all container-as-a-service platforms (Railway, Render, Cloud Run, Fly.io) because they only provide a container runtime—there's no Docker daemon for swe-swe to talk to. swe-swe needs a **VM with Docker installed**, not a container environment.

> **Note**: This is NOT "Docker-in-Docker" (DinD). DinD means running a Docker daemon inside a container. swe-swe simply runs `docker compose` on the host—a standard setup on any VM with Docker installed.

### Top 3 Recommendations

| Rank | Platform | Why |
|------|----------|-----|
| 1 | **Vultr Marketplace** | Best vendor experience, no revenue share, imageless apps supported |
| 2 | **DigitalOcean Marketplace** | Largest user base, mature tooling, great documentation |
| 3 | **Hetzner** | Cheapest VMs in market, but not accepting new apps currently |

---

## Requirements Analysis

### What swe-swe Needs from a Host

| Requirement | Reason | Hard/Soft |
|-------------|--------|-----------|
| Docker daemon + Docker Compose | `swe-swe up` runs `docker compose` (these are separate installs on servers) | **Hard** |
| Persistent VM | Services must stay running | **Hard** |
| 2GB+ RAM | Chrome container alone needs 1GB | **Hard** |
| Public IP / Port 1977 | Users access via browser | **Hard** |
| cloud-init or startup scripts | Run `swe-swe init && up` on boot | **Hard** |
| Deploy button / badge | One-click from README | Soft (can workaround) |
| Vendor program | Official marketplace listing | Soft |

### What swe-swe Does NOT Need

| Feature | Why Not Needed |
|---------|----------------|
| Platform-level Docker Compose parsing | `swe-swe up` runs `docker compose` directly on the host—we just need Docker + Compose installed, not a platform that interprets compose files (like Defang) |
| Kubernetes | Overkill, adds complexity |
| Auto-scaling | Single-user dev environment |
| Managed databases | swe-swe is stateless (no DB) |
| CDN / Edge | Local dev tool, not serving traffic |

### The Ideal User Flow

```
┌─────────────────────────────────────────────────────────────────┐
│  GitHub README                                                   │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │ # swe-swe                                                   ││
│  │                                                             ││
│  │ [![Deploy to Vultr](badge.svg)](https://vultr.com/...)     ││
│  │ [![Deploy to DO](badge.svg)](https://cloud.digitalocean...)││
│  │ [![Deploy to Hetzner](badge.svg)](https://console.hetzner.)││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼ User clicks badge
┌─────────────────────────────────────────────────────────────────┐
│  Hosting Provider                                                │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │ Sign up / Log in                                            ││
│  │ [  Email  ] [  Password  ]                                  ││
│  │            [ Continue ]                                      ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼ User authenticates
┌─────────────────────────────────────────────────────────────────┐
│  Deploy Configuration                                            │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │ Deploy swe-swe                                              ││
│  │                                                             ││
│  │ Region: [ US East ▼ ]                                       ││
│  │ Size:   [ 2GB RAM - $5/mo ▼ ]                              ││
│  │                                                             ││
│  │ Environment Variables:                                      ││
│  │ ANTHROPIC_API_KEY: [ _________________________ ]           ││
│  │                                                             ││
│  │            [ Deploy Now ]                                   ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼ Behind the scenes
┌─────────────────────────────────────────────────────────────────┐
│  VM Boot Sequence (cloud-init)                                   │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │ 1. Provision VM with Docker pre-installed                  ││
│  │ 2. Download swe-swe binary from GitHub releases            ││
│  │ 3. Generate random SWE_SWE_PASSWORD                        ││
│  │ 4. Run: swe-swe init --project-directory /workspace        ││
│  │ 5. Run: swe-swe up --project-directory /workspace          ││
│  │ 6. Write credentials to /etc/motd or dashboard             ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼ ~2-3 minutes later
┌─────────────────────────────────────────────────────────────────┐
│  Success Screen                                                  │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │ ✓ swe-swe is running!                                       ││
│  │                                                             ││
│  │ Access your instance:                                       ││
│  │ URL: http://143.198.xxx.xxx:1977                           ││
│  │ Password: aBcDeFgHiJkL                                      ││
│  │                                                             ││
│  │ Services:                                                   ││
│  │ • Terminal: http://...:1977/                               ││
│  │ • VS Code:  http://...:1977/vscode                         ││
│  │ • Browser:  http://...:1977/chrome                         ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

---

## Platform Categories

### Why Platform Type Matters

swe-swe runs `docker compose` on the host's Docker daemon. This is a standard setup—not Docker-in-Docker. But it means we need a **VM with Docker installed**, not a container environment.

```
┌─────────────────────────────────────────────────────────────────┐
│                     HOSTING PLATFORM TYPES                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────────┐    ┌──────────────────┐                   │
│  │   VM-BASED       │    │   CONTAINER      │                   │
│  │   (IaaS)         │    │   (CaaS/PaaS)    │                   │
│  ├──────────────────┤    ├──────────────────┤                   │
│  │ • Full OS access │    │ • No OS access   │                   │
│  │ • Docker daemon  │    │ • No Docker      │                   │
│  │   on host ✓      │    │   daemon ✗       │                   │
│  │ • Can run        │    │ • Can only run   │                   │
│  │   docker compose │    │   single container│                  │
│  │ • Root access    │    │ • Sandboxed      │                   │
│  ├──────────────────┤    ├──────────────────┤                   │
│  │ Examples:        │    │ Examples:        │                   │
│  │ • DigitalOcean   │    │ • Railway        │                   │
│  │   Droplets       │    │ • Render         │                   │
│  │ • Vultr VMs      │    │ • Fly.io         │                   │
│  │ • Hetzner Cloud  │    │ • Cloud Run      │                   │
│  │ • Linode         │    │ • DO App Platform│                   │
│  │ • AWS EC2        │    │ • Heroku         │                   │
│  └──────────────────┘    └──────────────────┘                   │
│           │                       │                              │
│           ▼                       ▼                              │
│    ✅ CAN RUN              ❌ CANNOT RUN                        │
│    swe-swe                 swe-swe                               │
│    (standard Docker        (no Docker daemon                     │
│     on a VM)               to talk to)                           │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Detailed Platform Analysis

### Tier 1: VM-Based with Deploy Buttons (RECOMMENDED)

#### 1.1 Vultr Marketplace

**Overview**
- **Website**: https://www.vultr.com/marketplace/
- **Type**: VM-based IaaS with marketplace
- **Deploy Button**: Yes, full support
- **Accepting New Apps**: Yes, via vendor program
- **Revenue Share**: None

**Pricing**
| Plan | RAM | Storage | Price |
|------|-----|---------|-------|
| VC2-1C-1GB | 1GB | 25GB SSD | $5/mo |
| VC2-1C-2GB | 2GB | 55GB SSD | $10/mo |
| VC2-2C-4GB | 4GB | 80GB SSD | $20/mo |

**Technical Details**
- **Build Options**: Packer snapshots OR imageless (cloud-init only)
- **App Variables**: Can define variables that auto-generate (e.g., passwords)
- **Helper Scripts**: Official `vultr-helper.sh` with common functions
- **OS Support**: Ubuntu, Debian, CentOS, Fedora, and more

**Vendor Program**
- Apply at: https://www.vultr.com/marketplace/become-a-verified-vendor/
- No revenue sharing required
- Full control over app landing page
- Custom screenshots, documentation, support links

**Key Repository**: https://github.com/vultr/vultr-marketplace

**File Structure**
```
vultr-marketplace-app/
├── packer-example/
│   └── vultr.pkr.hcl           # Packer HCL configuration
├── helper-scripts/
│   └── vultr-helper.sh         # Reusable shell functions
├── scripts/
│   └── provision.sh            # Main installation script
├── cleanup.sh                  # Pre-snapshot cleanup
└── README.md
```

**Imageless App Option**
Vultr uniquely supports "imageless" apps that use only cloud-init vendor data, requiring no pre-built snapshot. This means:
- Faster iteration during development
- Always pulls latest swe-swe release
- No snapshot maintenance

**Pros**
- ✅ Best vendor experience
- ✅ No revenue share
- ✅ Imageless option (no snapshot maintenance)
- ✅ Application variables (auto-gen passwords)
- ✅ 17 global regions
- ✅ Coolify already on marketplace (proves model works)

**Cons**
- ⚠️ Smaller user base than DigitalOcean
- ⚠️ Less documentation/community content

---

#### 1.2 DigitalOcean Marketplace

**Overview**
- **Website**: https://marketplace.digitalocean.com/
- **Type**: VM-based IaaS with marketplace
- **Deploy Button**: Yes, full support
- **Accepting New Apps**: Yes, via Vendor Portal
- **Revenue Share**: None

**Pricing**
| Plan | RAM | Storage | Price |
|------|-----|---------|-------|
| Basic 1GB | 1GB | 25GB SSD | $6/mo |
| Basic 2GB | 2GB | 50GB SSD | $12/mo |
| Basic 4GB | 4GB | 80GB SSD | $24/mo |

**Technical Details**
- **Build System**: Packer with JSON or HCL templates
- **Validation**: Required scripts for security checks
- **OS Support**: Ubuntu LTS, Debian, CentOS, and more
- **cloud-init**: Full support including per-instance scripts

**Key Repositories**
- https://github.com/digitalocean/marketplace-partners (validation tools)
- https://github.com/digitalocean/droplet-1-clicks (Packer templates)

**File Structure**
```
digitalocean-1click/
├── template.json               # Packer configuration
├── scripts/
│   └── installer.sh            # Main installation script
├── files/
│   ├── etc/
│   │   ├── update-motd.d/
│   │   │   └── 99-one-click    # First login message
│   │   └── systemd/system/
│   │       └── swe-swe.service # Systemd unit
│   └── var/
│       └── lib/cloud/scripts/per-instance/
│           └── 001_onboot      # Per-boot initialization
├── common/scripts/
│   ├── 900-cleanup.sh          # Security cleanup
│   └── 999-img_check.sh        # Validation
└── README.md
```

**Deploy Button Format**
```markdown
[![Deploy to DO](https://www.deploytodo.com/do-btn-blue.svg)](https://cloud.digitalocean.com/droplets/new?image=your-app-slug)
```

**Submission Process**
1. Create build Droplet ($6 tier works)
2. Run installation scripts
3. Execute `cleanup.sh` for security hardening
4. Run `img_check.sh` for validation
5. Create snapshot via Packer
6. Submit through Vendor Portal: https://cloud.digitalocean.com/vendorportal

**Pros**
- ✅ Largest user base among indie developers
- ✅ Excellent documentation
- ✅ Mature Packer tooling
- ✅ Strong brand recognition
- ✅ 15 global regions

**Cons**
- ⚠️ Slightly more expensive than competitors
- ⚠️ Stricter validation requirements
- ⚠️ No imageless option (must maintain snapshots)

---

#### 1.3 Hetzner Cloud

**Overview**
- **Website**: https://www.hetzner.com/cloud
- **Apps Repo**: https://github.com/hetznercloud/apps
- **Type**: VM-based IaaS
- **Deploy Button**: Yes, but only for existing apps
- **Accepting New Apps**: NO (currently closed)
- **Revenue Share**: N/A

**Pricing** (SIGNIFICANTLY CHEAPER)
| Plan | RAM | Storage | Price |
|------|-----|---------|-------|
| CX22 | 2GB | 40GB SSD | €3.29/mo (~$3.50) |
| CX32 | 4GB | 80GB SSD | €5.39/mo (~$5.75) |
| CX42 | 8GB | 160GB SSD | €14.49/mo (~$15.50) |

**Why Hetzner is 50%+ Cheaper**
- German company with efficient operations
- Zero-carbon infrastructure
- Less marketing spend
- Focused on European market

**Technical Details**
- **Build System**: Packer + cloud-init
- **User Data**: Full cloud-init support during server creation
- **OS Support**: Ubuntu, Debian, Fedora, Rocky Linux
- **Architecture**: Both x86_64 (amd64) and ARM64 supported

**Deploy Button Limitation**
The button format is:
```
https://console.hetzner.com/deploy/<app-name>
```

But `<app-name>` must be an existing Hetzner-approved app. Custom apps cannot use this URL format.

**Workaround: Docker Base + cloud-init**

Since Hetzner has a "Docker" app, we can:
1. Link to: `https://console.hetzner.com/deploy/docker`
2. Instruct users to paste a cloud-init script

**cloud-init Script for Hetzner**
```yaml
#cloud-config
package_update: true
packages:
  - curl

runcmd:
  # Install Docker Compose plugin (Hetzner's Docker app may not include it)
  # Docker Compose is a separate install from Docker Engine on servers
  - |
    ARCH=$(uname -m)
    case "${ARCH}" in
      x86_64) COMPOSE_ARCH="x86_64" ;;
      aarch64) COMPOSE_ARCH="aarch64" ;;
    esac
    mkdir -p /usr/local/lib/docker/cli-plugins
    curl -fsSL "https://github.com/docker/compose/releases/download/v2.32.4/docker-compose-linux-${COMPOSE_ARCH}" \
      -o /usr/local/lib/docker/cli-plugins/docker-compose
    chmod +x /usr/local/lib/docker/cli-plugins/docker-compose

  # Download latest swe-swe
  - |
    ARCH=$(uname -m)
    case $ARCH in
      x86_64) ARCH="amd64" ;;
      aarch64) ARCH="arm64" ;;
    esac
    curl -fsSL "https://github.com/anthropics/swe-swe/releases/latest/download/swe-swe-linux-${ARCH}" \
      -o /usr/local/bin/swe-swe
    chmod +x /usr/local/bin/swe-swe

  # Generate password
  - echo "SWE_SWE_PASSWORD=$(openssl rand -base64 12)" >> /etc/environment

  # Create workspace
  - mkdir -p /workspace

  # Initialize swe-swe
  - /usr/local/bin/swe-swe init --project-directory /workspace --agents=claude

  # Create systemd service
  - |
    cat > /etc/systemd/system/swe-swe.service << 'EOF'
    [Unit]
    Description=swe-swe
    After=docker.service
    Requires=docker.service

    [Service]
    Type=simple
    EnvironmentFile=/etc/environment
    WorkingDirectory=/workspace
    ExecStart=/usr/local/bin/swe-swe up --project-directory /workspace
    Restart=always
    RestartSec=10

    [Install]
    WantedBy=multi-user.target
    EOF

  - systemctl daemon-reload
  - systemctl enable swe-swe
  - systemctl start swe-swe

final_message: |
  swe-swe installation complete!
  Access at: http://$(curl -s ifconfig.me):1977
  Password in: /etc/environment
```

**Alternative: Pre-built Snapshot**

Build your own image and share via API:
```bash
export HCLOUD_TOKEN=your_token

# Clone their apps repo structure
git clone https://github.com/hetznercloud/apps
cd apps

# Create your app following their structure
mkdir -p apps/hetzner/swe-swe

# Build snapshot
./build.sh swe-swe amd64
```

Users can then deploy via Hetzner CLI:
```bash
hcloud server create --name swe-swe --image YOUR_SNAPSHOT_ID --type cx22
```

**Pros**
- ✅ **Cheapest VMs in market** (50%+ less than DO/Vultr)
- ✅ Zero-carbon infrastructure
- ✅ ARM64 support (even cheaper)
- ✅ European data sovereignty
- ✅ Full cloud-init support
- ✅ Excellent API and CLI

**Cons**
- ❌ Not accepting new marketplace apps
- ⚠️ Deploy button requires manual cloud-init paste
- ⚠️ Smaller US presence (primarily European)
- ⚠️ Less brand recognition in US market

---

#### 1.4 Linode (Akamai)

**Overview**
- **Website**: https://www.linode.com/
- **Type**: VM-based IaaS with StackScripts
- **Deploy Button**: Links only (no badge widget)
- **Accepting New Apps**: Yes, via StackScripts

**Pricing**
| Plan | RAM | Storage | Price |
|------|-----|---------|-------|
| Nanode 1GB | 1GB | 25GB SSD | $5/mo |
| Linode 2GB | 2GB | 50GB SSD | $12/mo |
| Linode 4GB | 4GB | 80GB SSD | $24/mo |

**StackScripts**
StackScripts are bash scripts that run during Linode provisioning:

```bash
#!/bin/bash
# <UDF name="anthropic_api_key" label="Anthropic API Key" />

# Install Docker Engine
curl -fsSL https://get.docker.com | sh

# Install Docker Compose plugin (separate install on servers)
ARCH=$(dpkg --print-architecture)
mkdir -p /usr/local/lib/docker/cli-plugins
curl -fsSL "https://github.com/docker/compose/releases/download/v2.32.4/docker-compose-linux-$(uname -m)" \
    -o /usr/local/lib/docker/cli-plugins/docker-compose
chmod +x /usr/local/lib/docker/cli-plugins/docker-compose

# Install swe-swe
curl -fsSL https://github.com/anthropics/swe-swe/releases/latest/download/swe-swe-linux-amd64 \
  -o /usr/local/bin/swe-swe
chmod +x /usr/local/bin/swe-swe

# Setup and run
export ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY"
swe-swe init --project-directory /workspace --agents=claude
swe-swe up --project-directory /workspace
```

**Deploy Link Format**
```
https://cloud.linode.com/stackscripts/YOUR_SCRIPT_ID
```

No embeddable badge exists, just direct URLs.

**Pros**
- ✅ Simple StackScript model
- ✅ User Defined Fields (UDF) for config
- ✅ Acquired by Akamai (stability)

**Cons**
- ❌ No deploy button badge
- ⚠️ Less marketplace visibility
- ⚠️ StackScripts less discoverable than marketplaces

---

### Tier 2: Self-Hosted PaaS

These platforms require users to first set up the PaaS on their own VPS, then one-click deploy apps within that PaaS.

#### 2.1 Coolify

**Overview**
- **Website**: https://coolify.io/
- **GitHub**: https://github.com/coollabsio/coolify
- **Type**: Self-hosted PaaS (Heroku alternative)
- **Deploy Model**: User installs Coolify first, then deploys apps

**How It Works**
1. User rents a VPS (Hetzner, DO, Vultr, etc.)
2. Installs Coolify: `curl -fsSL https://cdn.coollabs.io/coolify/install.sh | bash`
3. Accesses Coolify dashboard
4. Can deploy from Git repos, Docker Compose, or marketplace

**swe-swe Integration Path**
- Submit swe-swe to Coolify's service catalog
- Users with Coolify can one-click deploy

**Coolify Service Template**
```yaml
# Would be submitted to Coolify's catalog
name: swe-swe
description: AI-powered software engineering environment
documentation: https://github.com/anthropics/swe-swe
compose: |
  services:
    swe-swe:
      image: ghcr.io/anthropics/swe-swe:latest
      ports:
        - "1977:1977"
      environment:
        - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
```

**Pros**
- ✅ 280+ services in catalog
- ✅ Docker Compose native support
- ✅ Git integration (GitHub, GitLab, Bitbucket)
- ✅ Available on Vultr Marketplace

**Cons**
- ⚠️ Requires user to have Coolify installed first
- ⚠️ Extra layer of complexity
- ⚠️ Not truly "one-click from README"

---

#### 2.2 Dokploy

**Overview**
- **Website**: https://dokploy.com/
- **GitHub**: https://github.com/Dokploy/dokploy
- **Type**: Self-hosted PaaS

Similar to Coolify but newer. Supports Docker Compose natively.

---

#### 2.3 CapRover

**Overview**
- **Website**: https://caprover.com/
- **GitHub**: https://github.com/caprover/caprover
- **Type**: Self-hosted PaaS

Older than Coolify, uses "One Click Apps" model.

---

### Tier 3: Container Platforms (NOT VIABLE)

These platforms CANNOT run swe-swe because they only provide a **container runtime**, not a Docker daemon.

**The Core Issue**: swe-swe needs to run `docker compose up` on a Docker daemon. These platforms run your app as a container, but don't give you access to any Docker daemon to spawn additional containers.

```
┌─────────────────────────────────────────────────────────────────┐
│  What swe-swe needs (VM with Docker)                            │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────────────────────┐│
│  │  VM (Ubuntu, etc.)                                          ││
│  │  ┌─────────────────────────────────────────────────────────┐││
│  │  │  Docker Daemon                                          │││
│  │  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐       │││
│  │  │  │ traefik │ │  auth   │ │ swe-swe │ │ chrome  │ ...   │││
│  │  │  └─────────┘ └─────────┘ └─────────┘ └─────────┘       │││
│  │  └─────────────────────────────────────────────────────────┘││
│  │  ↑ swe-swe up talks to this daemon                          ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│  What container platforms provide                               │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────────────────────┐│
│  │  Platform's Infrastructure (hidden)                         ││
│  │  ┌─────────────────────────────────────────────────────────┐││
│  │  │  Container Runtime (not Docker daemon)                  │││
│  │  │  ┌─────────────────────────────────────────────────────┐│││
│  │  │  │  Your App Container (sandboxed)                     ││││
│  │  │  │  - No docker CLI                                    ││││
│  │  │  │  - No access to spawn containers                    ││││
│  │  │  │  - Cannot run docker compose                        ││││
│  │  │  └─────────────────────────────────────────────────────┘│││
│  │  └─────────────────────────────────────────────────────────┘││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

#### 3.1 Railway

**Why Not Viable**
- No Docker daemon access from within containers
- Quote: "You simply can't do such things on Railway"
- Their architecture runs containers, doesn't provide container orchestration to users

**Source**: https://help.railway.com/feature-request/docker-in-docker-d07c4730

---

#### 3.2 Render

**Why Not Viable**
- Builds FROM Dockerfiles but runs containers in their runtime
- No Docker daemon exposed to your application
- Cannot run `docker compose` from inside your container

---

#### 3.3 Fly.io

**Why Not Viable**
- Container-focused platform using Firecracker VMs
- Your app runs as an isolated container
- No Docker daemon access

---

#### 3.4 Google Cloud Run

**Why Not Viable**
- Serverless container platform
- Containers are ephemeral and isolated
- No Docker daemon, no persistent processes

---

#### 3.5 DigitalOcean App Platform

**Why Not Viable**
- Different from DO Droplets (which ARE viable)
- Container-based PaaS, not VMs
- Quote: "You don't have access to a fixed Droplet or VM"

---

### Tier 4: Enterprise/Cloud Providers

These work but have more complex deploy experiences.

#### 4.1 AWS EC2 + CloudFormation

**Deploy Button**: "Launch Stack" button
**Complexity**: High (requires CloudFormation template)

```markdown
[![Launch Stack](https://s3.amazonaws.com/cloudformation-examples/cloudformation-launch-stack.png)](https://console.aws.amazon.com/cloudformation/home?#/stacks/new?stackName=swe-swe&templateURL=https://s3.amazonaws.com/your-bucket/template.yaml)
```

---

#### 4.2 Azure + ARM Templates

**Deploy Button**: "Deploy to Azure" button
**Documentation**: https://learn.microsoft.com/en-us/azure/azure-resource-manager/templates/deploy-to-azure-button

```markdown
[![Deploy to Azure](https://aka.ms/deploytoazurebutton)](https://portal.azure.com/#create/Microsoft.Template/uri/https%3A%2F%2Fraw.githubusercontent.com%2F...)
```

---

#### 4.3 Google Cloud + Deployment Manager

**Deploy Button**: "Deploy on Google Cloud" (limited)
**Complexity**: High

---

## Technical Deep Dives

### Packer Template for swe-swe (DigitalOcean Example)

```json
{
  "variables": {
    "do_api_token": "{{env `DIGITALOCEAN_API_TOKEN`}}",
    "image_name": "swe-swe-{{timestamp}}",
    "application_name": "swe-swe",
    "application_version": "1.0.0"
  },
  "builders": [
    {
      "type": "digitalocean",
      "api_token": "{{user `do_api_token`}}",
      "image": "ubuntu-24-04-x64",
      "region": "nyc3",
      "size": "s-1vcpu-2gb",
      "ssh_username": "root",
      "snapshot_name": "{{user `image_name`}}"
    }
  ],
  "provisioners": [
    {
      "type": "shell",
      "inline": [
        "cloud-init status --wait"
      ]
    },
    {
      "type": "file",
      "source": "files/",
      "destination": "/"
    },
    {
      "type": "shell",
      "scripts": [
        "scripts/010-docker.sh",
        "scripts/020-swe-swe.sh",
        "scripts/030-systemd.sh",
        "scripts/900-cleanup.sh"
      ],
      "environment_vars": [
        "DEBIAN_FRONTEND=noninteractive",
        "APPLICATION_NAME={{user `application_name`}}",
        "APPLICATION_VERSION={{user `application_version`}}"
      ]
    }
  ]
}
```

### Docker Install Script (scripts/010-docker.sh)

```bash
#!/bin/bash
set -euo pipefail

echo "Installing Docker Engine..."
curl -fsSL https://get.docker.com | sh

echo "Installing Docker Compose plugin..."
# Docker Compose is a separate install on servers (not bundled like Docker Desktop)
ARCH=$(uname -m)
case "${ARCH}" in
    x86_64) COMPOSE_ARCH="x86_64" ;;
    aarch64) COMPOSE_ARCH="aarch64" ;;
    *) echo "Unsupported architecture: ${ARCH}" && exit 1 ;;
esac

mkdir -p /usr/local/lib/docker/cli-plugins
curl -fsSL "https://github.com/docker/compose/releases/download/v2.32.4/docker-compose-linux-${COMPOSE_ARCH}" \
    -o /usr/local/lib/docker/cli-plugins/docker-compose
chmod +x /usr/local/lib/docker/cli-plugins/docker-compose

# Verify installation
docker --version
docker compose version

echo "Docker + Compose installed successfully"
```

### swe-swe Install Script (scripts/020-swe-swe.sh)

```bash
#!/bin/bash
set -euo pipefail

echo "Installing swe-swe..."

# Detect architecture
ARCH=$(uname -m)
case $ARCH in
  x86_64) ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Download latest release
DOWNLOAD_URL="https://github.com/anthropics/swe-swe/releases/latest/download/swe-swe-linux-${ARCH}"
curl -fsSL "$DOWNLOAD_URL" -o /usr/local/bin/swe-swe
chmod +x /usr/local/bin/swe-swe

# Verify installation
/usr/local/bin/swe-swe --version

echo "swe-swe installed successfully"
```

### Systemd Service (files/etc/systemd/system/swe-swe.service)

```ini
[Unit]
Description=swe-swe AI Development Environment
After=docker.service network-online.target
Requires=docker.service
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=-/etc/environment
EnvironmentFile=-/etc/swe-swe/env
WorkingDirectory=/workspace
ExecStartPre=/usr/local/bin/swe-swe init --project-directory /workspace --agents=claude
ExecStart=/usr/local/bin/swe-swe up --project-directory /workspace
ExecStop=/usr/bin/docker compose -f /root/.swe-swe/projects/workspace/docker-compose.yml down
Restart=always
RestartSec=10

# Security hardening
NoNewPrivileges=false
ProtectSystem=false
PrivateTmp=false

[Install]
WantedBy=multi-user.target
```

### First-Boot Script (files/var/lib/cloud/scripts/per-instance/001_onboot)

```bash
#!/bin/bash
set -euo pipefail

# Generate random password if not set
if [ -z "${SWE_SWE_PASSWORD:-}" ]; then
  SWE_SWE_PASSWORD=$(openssl rand -base64 16 | tr -d '/+=' | head -c 16)
  echo "SWE_SWE_PASSWORD=$SWE_SWE_PASSWORD" >> /etc/environment
  echo "SWE_SWE_PASSWORD=$SWE_SWE_PASSWORD" > /etc/swe-swe/env
fi

# Create workspace directory
mkdir -p /workspace

# Get public IP
PUBLIC_IP=$(curl -s http://169.254.169.254/metadata/v1/interfaces/public/0/ipv4/address 2>/dev/null || curl -s ifconfig.me)

# Write credentials for user
cat > /etc/motd << EOF
********************************************************************************
                           swe-swe is ready!
********************************************************************************

Access your development environment:

  URL:      http://${PUBLIC_IP}:1977
  Password: ${SWE_SWE_PASSWORD}

Services available:
  • Terminal:  http://${PUBLIC_IP}:1977/
  • VS Code:   http://${PUBLIC_IP}:1977/vscode
  • Browser:   http://${PUBLIC_IP}:1977/chrome
  • Dashboard: http://${PUBLIC_IP}:1977/dashboard

To view this message again: cat /etc/motd

********************************************************************************
EOF

# Start swe-swe service
systemctl start swe-swe

# Re-enable SSH (if it was locked during image creation)
sed -e '/Match User root/d' -e '/.*ForceCommand.*droplet.*/d' -i /etc/ssh/sshd_config
systemctl restart ssh
```

### MOTD Display (files/etc/update-motd.d/99-one-click)

```bash
#!/bin/bash

cat /etc/motd
```

---

## Comparison Matrix

### Feature Comparison

| Feature | Vultr | DigitalOcean | Hetzner | Linode |
|---------|-------|--------------|---------|--------|
| Deploy Button Badge | ✅ | ✅ | ⚠️ Limited | ❌ |
| Accepting Apps | ✅ | ✅ | ❌ | ✅ |
| Vendor Portal | ✅ | ✅ | ❌ | ❌ |
| Revenue Share | None | None | N/A | N/A |
| App Variables | ✅ | ❌ | ❌ | ✅ (UDF) |
| Imageless Apps | ✅ | ❌ | ❌ | ✅ |
| Packer Support | ✅ | ✅ | ✅ | ❌ |
| cloud-init | ✅ | ✅ | ✅ | ✅ |
| ARM64 Support | ✅ | ✅ | ✅ | ❌ |

### Pricing Comparison (2GB RAM tier)

| Provider | Monthly | Hourly | Storage | Notes |
|----------|---------|--------|---------|-------|
| **Hetzner** | **€3.29** (~$3.50) | €0.005 | 40GB | Cheapest |
| Vultr | $10.00 | $0.015 | 55GB | |
| DigitalOcean | $12.00 | $0.018 | 50GB | |
| Linode | $12.00 | $0.018 | 50GB | |

### Deploy Experience Comparison

| Step | Vultr | DigitalOcean | Hetzner | Linode |
|------|-------|--------------|---------|--------|
| Click badge in README | ✅ | ✅ | ⚠️ Manual step | ❌ Link only |
| Lands on pre-filled page | ✅ | ✅ | ⚠️ Need paste | ✅ |
| Enter API keys | ✅ App vars | ⚠️ Env vars | ⚠️ In cloud-init | ✅ UDF |
| One click to deploy | ✅ | ✅ | ✅ | ✅ |
| See credentials after | ✅ Dashboard | ⚠️ SSH in | ⚠️ SSH in | ⚠️ SSH in |

---

## Recommendations

### Primary Recommendation: Vultr

**Why Vultr is #1:**

1. **Best Developer Experience**
   - App variables auto-generate passwords
   - Dashboard shows credentials post-deploy
   - No SSH required to get started

2. **Imageless Option**
   - Can iterate without rebuilding snapshots
   - Always pulls latest swe-swe release
   - Reduces maintenance burden

3. **No Revenue Share**
   - Keep 100% of any future monetization
   - No per-deployment fees

4. **Proven Model**
   - Coolify is on their marketplace
   - Similar architecture to swe-swe

### Secondary Recommendation: DigitalOcean

**Why DO is #2:**

1. **Largest Indie Developer Audience**
   - Most developers already have DO accounts
   - Strong brand recognition

2. **Best Documentation**
   - Extensive Packer examples
   - Clear validation requirements

3. **Community**
   - Many reference implementations
   - Active community tutorials

### Tertiary Recommendation: Hetzner

**Why Hetzner is #3:**

1. **Cost-Conscious Users**
   - 50%+ cheaper than alternatives
   - Great for demos/trials

2. **European Users**
   - Data sovereignty compliance
   - Low-latency in EU

3. **Workaround Available**
   - Docker app + cloud-init paste
   - Still achievable, just extra step

---

## Implementation Roadmap

### Phase 1: Vultr Marketplace (Week 1-2)

```
□ Apply to Vultr vendor program
□ Create packer-example/ with HCL config
□ Write provision.sh installer script
□ Define app variables (SWE_SWE_PASSWORD, ANTHROPIC_API_KEY)
□ Create cleanup.sh for image preparation
□ Build and test snapshot
□ Create app landing page with screenshots
□ Submit for review
□ Add badge to README once approved
```

### Phase 2: DigitalOcean Marketplace (Week 2-3)

```
□ Apply to DO vendor program
□ Fork droplet-1-clicks template
□ Create template.json Packer config
□ Write installer.sh and 001_onboot scripts
□ Add MOTD and systemd service
□ Run validation scripts (cleanup.sh, img_check.sh)
□ Build snapshot
□ Submit via Vendor Portal
□ Add badge to README once approved
```

### Phase 3: Hetzner Documentation (Week 3)

```
□ Create cloud-init YAML template
□ Write user documentation for manual process
□ Create "Deploy to Hetzner" section in docs
□ Link to Docker app deploy button
□ Provide copy-paste cloud-init
□ Monitor for when Hetzner opens submissions
```

### Phase 4: Self-Hosted PaaS Templates (Week 4+)

```
□ Create Coolify service template
□ Submit to Coolify catalog
□ Create Dokploy template
□ Document CapRover one-click app
```

---

## Sources

### Official Documentation

- [DigitalOcean Marketplace Partners](https://github.com/digitalocean/marketplace-partners)
- [DigitalOcean 1-Click Developer Guide](https://github.com/digitalocean/droplet-1-clicks/blob/master/DEVELOPER-GUIDE.md)
- [Vultr Marketplace Docs](https://docs.vultr.com/vultr-marketplace)
- [Vultr Marketplace Tools](https://github.com/vultr/vultr-marketplace)
- [Hetzner Cloud Apps](https://github.com/hetznercloud/apps)
- [Hetzner Cloud Apps Overview](https://docs.hetzner.com/cloud/apps/overview/)
- [Hetzner Packer Plugin](https://developer.hashicorp.com/packer/integrations/hetznercloud/hcloud)
- [Linode StackScripts](https://www.linode.com/docs/guides/platform/stackscripts/)

### Deploy Button Documentation

- [Deploy to Azure Button](https://learn.microsoft.com/en-us/azure/azure-resource-manager/templates/deploy-to-azure-button)
- [Railway Templates](https://docs.railway.com/reference/templates)
- [Render Deploy Button](https://render.com/docs/deploy-to-render)
- [Cloudflare Deploy Button](https://blog.cloudflare.com/deploy-workers-applications-in-seconds/)

### Platform Limitations

- [Railway Docker-in-Docker Discussion](https://help.railway.com/feature-request/docker-in-docker-d07c4730)
- [DigitalOcean App Platform Docker Compose](https://www.digitalocean.com/community/questions/app-docker-compose)

### Tutorials

- [Hetzner Packer Tutorial](https://community.hetzner.com/tutorials/custom-os-images-with-packer/)
- [DigitalOcean Packer Example](https://www.digitalocean.com/blog/using-packer-to-create-a-1-click-nkn-image-on-digitalocean)
- [Coolify on Vultr](https://docs.vultr.com/how-to-use-vultr-s-coolify-marketplace-application)

### Self-Hosted PaaS

- [Coolify](https://coolify.io/)
- [Dokploy](https://dokploy.com/)
- [CapRover](https://caprover.com/)

---

## Appendix A: Badge Assets

### DigitalOcean
```markdown
[![Deploy to DigitalOcean](https://www.deploytodo.com/do-btn-blue.svg)](https://cloud.digitalocean.com/droplets/new?image=swe-swe)
```

### Vultr
```markdown
[![Deploy to Vultr](https://www.vultr.com/media/marketplace-badge.svg)](https://www.vultr.com/marketplace/apps/swe-swe/)
```

### Hetzner (with Docker base)
```markdown
[![Deploy to Hetzner](https://console.hetzner.cloud/assets/deploy-button.svg)](https://console.hetzner.com/deploy/docker)
```

### Generic (shields.io)
```markdown
[![Deploy](https://img.shields.io/badge/Deploy-swe--swe-blue?style=for-the-badge&logo=docker)](https://your-url.com)
```

---

## Appendix B: Cost Calculator

### Monthly Cost for Running swe-swe

| Usage | Hetzner (CX22) | Vultr (2GB) | DigitalOcean (2GB) |
|-------|----------------|-------------|-------------------|
| Always-on | €3.29 ($3.50) | $10.00 | $12.00 |
| 8hr/day weekdays | €1.10 ($1.17) | $3.33 | $4.00 |
| On-demand (10hr/wk) | €0.22 ($0.23) | $0.65 | $0.78 |

*Hetzner is 65-70% cheaper for equivalent resources.*

---

## Appendix C: Security Considerations

### Pre-Snapshot Cleanup Checklist

```bash
# Remove SSH keys
rm -rf /root/.ssh/authorized_keys
rm -rf /home/*/.ssh/authorized_keys

# Remove bash history
rm -f /root/.bash_history
rm -f /home/*/.bash_history

# Remove cloud-init artifacts
cloud-init clean --logs --machine-id

# Remove temporary files
rm -rf /tmp/*
rm -rf /var/tmp/*

# Remove apt cache
apt-get clean
rm -rf /var/lib/apt/lists/*

# Zero free space (optional, reduces image size)
dd if=/dev/zero of=/EMPTY bs=1M || true
rm -f /EMPTY
```

### Runtime Security

1. **Password Generation**: Always generate random passwords on first boot
2. **No Default Credentials**: Never ship with default passwords
3. **Firewall**: Enable UFW with only required ports (22, 1977)
4. **Auto-Updates**: Consider unattended-upgrades for security patches
5. **TLS**: Encourage users to use `--ssl=selfsign` or reverse proxy

---

*Last updated: 2026-01-06*
