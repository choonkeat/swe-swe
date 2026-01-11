# DigitalOcean Marketplace 1-Click App for swe-swe

Build a DigitalOcean Marketplace image for swe-swe using Packer.

## Prerequisites

- [Packer](https://developer.hashicorp.com/packer/install) (v1.14.0+)
- DigitalOcean account with API token
- Built swe-swe binary (requires `make build` at repo root)

## Getting a DigitalOcean API Token

1. Go to https://cloud.digitalocean.com/account/api/tokens
2. Click **Generate New Token**
3. Name it (e.g., "packer-swe-swe")
4. Select **Custom Scopes** and grant permissions:

   **Required**:
   - `droplet:create` — Create temporary build Droplet
   - `droplet:read` — Monitor Droplet status
   - `droplet:delete` — Destroy Droplet and create snapshot
   - `ssh_key:create` — Create temporary SSH key
   - `ssh_key:delete` — Remove temporary SSH key

   **Recommended** (for graceful shutdown):
   - `droplet:power` — Gracefully shut down Droplet after build (optional but recommended)

5. Copy the token (shown only once)
6. Export it:

```bash
export DIGITALOCEAN_API_TOKEN=dop_v1_xxxxx
```

> **Security Note**: The API token is used only by Packer on your local machine. It is never copied to or stored in the Droplet image.

## Building the Image

### Step 1: Build swe-swe Binary

The Packer image bundles a pre-built swe-swe binary. Build it first:

```bash
# At repo root
make build

# Verify the binary was created
ls -lh ./dist/swe-swe.linux-amd64
```

If the binary doesn't exist, Packer will fail with a file not found error.

### Step 2: Initialize and Validate Packer

```bash
cd deploy/digitalocean

# Initialize Packer plugins
packer init template.pkr.hcl

# Validate the template
packer validate template.pkr.hcl
```

### Step 3: Configure and Build

**Required variables**:
- `region` — DigitalOcean region (e.g., `nyc3`, `sfo3`, `lon1`)
- `image_version` — Version tag for the snapshot

Generate a dynamic image version using git:

```bash
# Use git tag (if available) + short SHA, or fall back to YYYYMMDD + short SHA
IMAGE_VERSION=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || date +%Y%m%d)-$(git rev-parse --short HEAD)
echo "Building version: $IMAGE_VERSION"
```

**Build command**:

```bash
IMAGE_VERSION=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || date +%Y%m%d)-$(git rev-parse --short HEAD)

packer build \
  -var "region=nyc3" \
  -var "image_version=$IMAGE_VERSION" \
  template.pkr.hcl
```

To customize the build droplet size (default: `s-2vcpu-4gb`):

```bash
packer build \
  -var "region=sfo3" \
  -var "droplet_size=s-4vcpu-8gb" \
  -var "image_version=$IMAGE_VERSION" \
  template.pkr.hcl
```

**Available regions**: Visit the [DigitalOcean API docs](https://docs.digitalocean.com/reference/api/list-regions/) to see all available regions (e.g., `nyc1`, `nyc3`, `sfo3`, `lon1`, `sgp1`, `tor1`, etc.).

**Available droplet sizes** (for building): Common sizes:
- `s-1vcpu-1gb` — 1 vCPU, 1GB RAM ($6/month) — minimum
- `s-1vcpu-2gb` — 1 vCPU, 2GB RAM ($12/month)
- `s-2vcpu-2gb` — 2 vCPU, 2GB RAM ($18/month)
- `s-2vcpu-4gb` — 2 vCPU, 4GB RAM ($24/month) — **default (recommended)**
- `s-4vcpu-8gb` — 4 vCPU, 8GB RAM ($48/month)

See the [DigitalOcean API docs](https://docs.digitalocean.com/reference/api/list-sizes/) for the complete list.

**Optional variables**:
- `image_name` (default: `swe-swe`) — Base name for the snapshot
- `droplet_size` (default: `s-2vcpu-4gb`) — Build Droplet size
- `do_token` (from `$DIGITALOCEAN_API_TOKEN`) — DigitalOcean API token

## Testing the Image

After the build completes, test the snapshot:

1. Create a Droplet from the snapshot in DigitalOcean console
2. SSH into the Droplet - the MOTD will display credentials
3. Visit `http://<IP>:1977` to access swe-swe
4. Verify all services are running:

```bash
# On the Droplet
systemctl status swe-swe
docker ps
```

### Test Checklist

- [ ] MOTD displays URL and password on SSH login
- [ ] `http://<IP>:1977` shows login page
- [ ] Password authentication works
- [ ] `http://<IP>:1977/vscode` loads VS Code
- [ ] `http://<IP>:1977/chrome` loads Chrome VNC
- [ ] `systemctl status swe-swe` shows active
- [ ] `docker ps` shows 5+ containers

## Image Validation

Run the DigitalOcean image validation tool before submission:

```bash
# On the Droplet (as root)
sudo /root/99-img-check.sh
```

This checks for marketplace compliance:
- No SSH keys or passwords
- Cloud-init installed
- Firewall configured
- No sensitive data in logs

## Marketplace Submission

1. Go to https://marketplace.digitalocean.com/vendors
2. Create a vendor account if needed
3. Submit your image with:
   - Snapshot ID (from `manifest.json`)
   - Application description
   - Support documentation
   - Pricing (free)

## Directory Structure

```
deploy/digitalocean/
├── template.pkr.hcl              # Packer configuration
├── README.md                     # This file
├── scripts/
│   ├── 010-docker.sh             # Install Docker + Docker Compose
│   ├── 020-swe-swe.sh            # Download swe-swe binary
│   ├── 030-systemd.sh            # Enable systemd service
│   ├── 090-ufw.sh                # Configure firewall
│   ├── 99-img-check.sh           # DO validation tool
│   └── 900-cleanup.sh            # Security cleanup
└── files/
    ├── etc/
    │   ├── update-motd.d/
    │   │   └── 99-swe-swe        # Login message
    │   └── systemd/system/
    │       └── swe-swe.service   # Systemd unit file
    └── var/
        └── lib/cloud/scripts/per-instance/
            └── 001_onboot        # First-boot initialization
```

## Troubleshooting

### Build fails: "swe-swe binary not found"

This means `make build` wasn't run at the repo root. The binary must exist before running Packer:

```bash
# At repo root
make build
ls -lh ./dist/swe-swe.linux-amd64

# Then run Packer again
cd deploy/digitalocean
packer build ...
```

### Build fails: "Could not get lock /var/lib/dpkg/lock-frontend"

This is a race condition where Packer tries to install packages while cloud-init is still running background updates. The template includes a `cloud-init status --wait` step to prevent this.

If you see this error:
- It's safe to retry — Packer will destroy the temporary Droplet and start fresh
- Run `make deploy/digitalocean` again or `packer build` directly with the same variables
- The wait mechanism will prevent the lock issue on the next attempt

### Build fails with authentication error

Ensure your API token has the required Custom Scopes and is correctly exported:

```bash
echo $DIGITALOCEAN_API_TOKEN
```

Verify the token has: `droplet:create`, `droplet:read`, `droplet:delete`, `ssh_key:create`, `ssh_key:delete`

If you see `403 You are not authorized to perform this operation` during shutdown:
- Your token is missing the optional `droplet:power` scope
- Add this scope to your API token in DigitalOcean console
- The build will still complete and create the snapshot, but Packer will force-destroy instead of gracefully shutdown

### Build fails with "image not found"

The base image `ubuntu-24-04-x64` must be available in the selected region. Try a different region:

```bash
IMAGE_VERSION=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || date +%Y%m%d)-$(git rev-parse --short HEAD)
packer build \
  -var "region=sfo3" \
  -var "image_version=$IMAGE_VERSION" \
  template.pkr.hcl
```

Available regions: `nyc1`, `nyc3`, `sfo2`, `sfo3`, `lon1`, `sgp1`, `blr1`, `tor1`, `ams3`, `fra1`, `jpt1`, `mad1`

## Finding Your Snapshots on DigitalOcean

After a successful build, find your snapshot:

1. Log in to https://cloud.digitalocean.com/
2. Left sidebar → **Backups & Snapshots** (under MANAGE section)
3. Click the **Snapshots** tab
4. Your snapshot will be named like: `swe-swe-2.6.0-a2d88bf4-20260111-102030`

From there you can:
- Create a Droplet from the snapshot
- Copy to other regions
- Delete when no longer needed

### swe-swe doesn't start on first boot

Check cloud-init logs:

```bash
cat /var/log/cloud-init-output.log
```

Check the first-boot script directly:

```bash
cat /var/lib/cloud/scripts/per-instance/001_onboot
```

### Firewall blocking connections

Verify UFW rules:

```bash
ufw status
# Should show ports 22 and 1977 allowed
```

## Estimated Costs

- Packer build (~10 min): ~$0.02
- Test Droplet (~30 min): ~$0.01
- Snapshot storage: ~$0.05/GB/month
