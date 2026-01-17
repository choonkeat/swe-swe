# swe-swe on DigitalOcean

Deploy swe-swe with one click on DigitalOcean.

## Quick Start

1. Go to the [DigitalOcean Marketplace](https://marketplace.digitalocean.com/apps/swe-swe) (once published)
2. Click **"Deploy to DigitalOcean"**
3. Sign in to your DigitalOcean account
4. Select:
   - **Droplet size**: $12/month or higher (2GB RAM minimum)
   - **Region**: Any available region
   - **Authentication**: SSH key (recommended) or password
   - **Hostname**: Choose a name (e.g., `swe-swe-dev`)
5. Click **"Create Droplet"**
6. Wait ~60 seconds for first-boot scripts to complete

## After Deployment

When you SSH into your droplet, you'll see a welcome message (MOTD) with:
- **URL**: `http://{IP}:1977`
- **Password**: Randomly generated (shown in MOTD)

Save these credentials!

### Access swe-swe

**In your browser**:
```
http://{Droplet-IP}:1977
```

Enter the password from the MOTD.

**Available interfaces**:
- **Dashboard**: Main swe-swe interface
- **VS Code**: `http://{IP}:1977/vscode` — Browser-based code editor
- **Chrome VNC**: `http://{IP}:1977/chrome` — Graphical browser environment

### SSH Access

```bash
ssh root@{Droplet-IP}
```

The MOTD will display again with all credentials.

## System Details

The droplet includes:
- **OS**: Ubuntu 24.04 LTS
- **Runtime**: Docker + Docker Compose
- **Services**:
  - swe-swe AI development environment
  - VS Code server
  - Chrome VNC server
  - Systemd service for auto-start

## Troubleshooting

### Can't connect to swe-swe (port 1977)

Check if the service is running:

```bash
systemctl status swe-swe
docker ps
```

View recent logs:

```bash
journalctl -u swe-swe -n 50
cat /var/log/cloud-init-output.log
```

### Forgot the password

SSH into the droplet and check:

```bash
cat /etc/swe-swe/credentials
```

### Services not starting

Check cloud-init initialization:

```bash
cat /var/log/cloud-init-output.log
```

Check the first-boot script:

```bash
cat /var/lib/cloud/scripts/per-instance/001_onboot
```

### Firewall blocking connections

Verify ports 22 (SSH) and 1977 (swe-swe) are allowed:

```bash
ufw status
```

Should show:
```
22/tcp  ALLOW  Anywhere
1977/tcp  ALLOW  Anywhere
```

## Costs

- **Droplet**: Varies by size
  - $12/month for 2GB RAM
  - $24/month for 4GB RAM
  - Higher tiers available
- **Storage**: Snapshot storage ~$0.05/GB/month (after deletion)

Stop the droplet anytime to halt charges. Snapshots persist.

## Building Your Own Image

Are you a developer who wants to customize the image or build from source?

See [**DEVELOPER.md**](./DEVELOPER.md) for instructions on:
- Building images with Packer
- Customizing installation scripts
- Submitting to the marketplace
- Testing before deployment

## Support

For issues with swe-swe, see the [main repository](https://github.com/anthropics/swe-swe).

For DigitalOcean-specific issues, consult [DigitalOcean documentation](https://docs.digitalocean.com/).
