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

### Finding Your Droplet IP

1. Go to https://cloud.digitalocean.com/droplets
2. Find your newly created droplet in the list
3. The IP address is shown in the droplet details

### Password & Credentials Workflow

The first time you access your droplet, follow this workflow:

**Step 1: SSH into the droplet**

```bash
ssh root@{Droplet-IP}
```

You'll see a welcome message (MOTD) that displays:
- **URL**: `http://{Droplet-IP}:1977`
- **Password**: A randomly generated password

**Step 2: Save the password**

The password is shown only once in the MOTD. Take note of it before closing the SSH connection.

**Step 3: Access swe-swe in your browser**

Open `http://{Droplet-IP}:1977` in your web browser and enter the password.

**Step 4: Subsequent logins**

To retrieve the password later:

```bash
ssh root@{Droplet-IP}
# MOTD displays again with password
```

Or directly from the command line:

```bash
ssh root@{Droplet-IP} cat /etc/swe-swe/credentials
```

### Managing the MOTD

The MOTD is stored in `/etc/update-motd.d/99-swe-swe` on the droplet.

**View the current MOTD**:

```bash
ssh root@{Droplet-IP} cat /etc/update-motd.d/99-swe-swe
```

**Edit the MOTD** (if you want to customize it):

```bash
ssh root@{Droplet-IP}
sudo nano /etc/update-motd.d/99-swe-swe
```

**Disable the MOTD** (if you prefer not to see it on login):

```bash
ssh root@{Droplet-IP}
sudo chmod -x /etc/update-motd.d/99-swe-swe
```

**Re-enable the MOTD** later:

```bash
ssh root@{Droplet-IP}
sudo chmod +x /etc/update-motd.d/99-swe-swe
```

### Changing the Password

To generate a new password:

```bash
ssh root@{Droplet-IP}
# Stop swe-swe service
sudo systemctl stop swe-swe

# Generate new password (update both env file and credentials file)
sudo bash -c 'cat /etc/swe-swe/env | grep -v SWEBSESSION_PASSWORD > /tmp/env && echo "SWEBSESSION_PASSWORD=$(openssl rand -base64 16)" >> /tmp/env && mv /tmp/env /etc/swe-swe/env'
sudo bash -c 'echo "URL: http://$(hostname -I | awk "{print $1}"):1977" > /etc/swe-swe/credentials && echo "Password: $(grep SWEBSESSION_PASSWORD /etc/swe-swe/env | cut -d= -f2)" >> /etc/swe-swe/credentials'

# Restart service
sudo systemctl start swe-swe

# View new credentials
cat /etc/swe-swe/credentials
```

### Access swe-swe

**In your browser**:
```
http://{Droplet-IP}:1977
```

Enter the password from the MOTD or credentials file.

**Available interfaces**:
- **Dashboard**: Main swe-swe interface
- **VS Code**: `http://{Droplet-IP}:1977/vscode` — Browser-based code editor
- **Chrome VNC**: `http://{Droplet-IP}:1977/chrome` — Graphical browser environment

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

### Collecting Diagnostics

If swe-swe isn't working, run these commands to gather diagnostic information. Copy and paste the entire block:

```bash
echo "=== SYSTEMD SERVICE STATUS ===" ; \
systemctl status swe-swe ; \
echo "" ; \
echo "=== DOCKER CONTAINERS ===" ; \
docker ps -a ; \
echo "" ; \
echo "=== FIRST BOOT SCRIPT OUTPUT ===" ; \
tail -100 /var/log/cloud-init-output.log ; \
echo "" ; \
echo "=== CLOUD-INIT STATUS ===" ; \
cloud-init status ; \
echo "" ; \
echo "=== SWEBSWE SERVICE LOGS ===" ; \
journalctl -u swe-swe -n 100 ; \
echo "" ; \
echo "=== CONFIG FILES ===" ; \
ls -la /etc/swe-swe/ ; \
echo "" ; \
echo "==> Copy everything above and share for debugging"
```

Then check:
- **Service status**: Should show "Active: active (running)"
- **Docker containers**: Should show 5+ running containers
- **Cloud-init output**: Should show "==> First-boot initialization complete!"
- **Cloud-init status**: Should show "status: done"

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
