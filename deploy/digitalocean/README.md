# DigitalOcean Marketplace 1-Click App for swe-swe

Build a DigitalOcean Marketplace image for swe-swe using Packer.

## Prerequisites

- [Packer](https://developer.hashicorp.com/packer/downloads) (v1.9+)
- DigitalOcean account with API token

## Getting a DigitalOcean API Token

1. Go to https://cloud.digitalocean.com/account/api/tokens
2. Click **Generate New Token**
3. Name it (e.g., "packer-swe-swe")
4. Select **Read + Write** scope
5. Copy the token (shown only once)
6. Export it:

```bash
export DIGITALOCEAN_API_TOKEN=dop_v1_xxxxx
```

> **Security Note**: The API token is used only by Packer on your local machine. It is never copied to or stored in the Droplet image.

## Building the Image

```bash
cd deploy/digitalocean

# Initialize Packer plugins
packer init template.pkr.hcl

# Validate the template
packer validate template.pkr.hcl

# Build the image
packer build template.pkr.hcl
```

### Build Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `do_token` | `$DIGITALOCEAN_API_TOKEN` | DigitalOcean API token |
| `image_name` | `swe-swe` | Base name for the snapshot |
| `image_version` | `1.0.0` | Version tag |
| `droplet_size` | `s-1vcpu-2gb` | Droplet size during build |
| `region` | `nyc3` | Build region |

Override variables with `-var`:

```bash
packer build -var "image_version=1.2.0" template.pkr.hcl
```

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

### Build fails with authentication error

Ensure your API token has Read + Write scope and is correctly exported:

```bash
echo $DIGITALOCEAN_API_TOKEN
```

### Build fails with "image not found"

The base image `ubuntu-24-04-x64` must be available in the selected region. Try a different region:

```bash
packer build -var "region=sfo3" template.pkr.hcl
```

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
