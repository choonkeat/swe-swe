# Chrome Container SSL Certificate Flaw - Deep Dive Research

**Date:** 2025-12-27
**Status:** Investigation Complete - Flaw Identified

## Problem Statement

The Chrome container in swe-swe fails to establish HTTPS connections with enterprise SSL certificates, while the main swe-swe container works correctly.

**Observed Error:**
```
net::ERR_CERT_AUTHORITY_INVALID at https://example.com/
```

## Root Cause Analysis

### The Flaw: Chromium Uses NSS, Not System CA Store

The Chrome container's `entrypoint.sh` installs certificates using the standard Debian approach:

```bash
cp /swe-swe/certs/*.pem /usr/local/share/ca-certificates/
update-ca-certificates
```

**This works for:**
- OpenSSL-based tools (curl, wget, Python requests, Node.js https)
- These tools read from `/etc/ssl/certs/ca-certificates.crt`

**This does NOT work for Chromium because:**
- Chromium on Linux uses **NSS (Network Security Services)** shared database
- NSS maintains its own certificate database, separate from the system CA store
- The database is typically at `~/.pki/nssdb/` for the user, or system-wide at `/etc/pki/nssdb/`

### Current Implementation Comparison

| Aspect | Main swe-swe Container | Chrome Container |
|--------|------------------------|------------------|
| Certificate Installation | `update-ca-certificates` | `update-ca-certificates` |
| Trust Store Used | System CA (`/etc/ssl/certs`) | **NSS database** (ignored!) |
| NODE_EXTRA_CA_CERTS | ✅ Set in docker-compose.yml | ❌ Not set |
| Tools Affected | curl, npm, Node.js ✅ | Chromium ❌ |
| Result | SSL works | SSL fails |

### docker-compose.yml Evidence

**swe-swe service (lines 70-75):**
```yaml
environment:
  - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
  - NODE_EXTRA_CA_CERTS=${NODE_EXTRA_CA_CERTS}
  - SSL_CERT_FILE=${SSL_CERT_FILE}
  - BROWSER_WS_ENDPOINT=ws://chrome:9223
```

**chrome service (lines 20-43):**
```yaml
# NO environment variables for certificates!
volumes:
  - ./certs:/swe-swe/certs:ro  # Mounted but not used by Chromium
```

## The Missing Piece: NSS Certificate Installation

Chromium requires certificates to be added to NSS database using `certutil`:

```bash
# Create NSS database if it doesn't exist
mkdir -p /home/chrome/.pki/nssdb
certutil -d sql:/home/chrome/.pki/nssdb -N --empty-password

# Add certificate to NSS database
certutil -d sql:/home/chrome/.pki/nssdb -A -t "C,," -n "enterprise-ca" -i /swe-swe/certs/cert.pem
```

## Why Main Container Works

The main swe-swe container works because:

1. **Tools use OpenSSL:** curl, npm, Node.js all use OpenSSL which respects system CA store
2. **NODE_EXTRA_CA_CERTS:** Explicitly set in docker-compose.yml for Node.js
3. **No browser:** Doesn't run Chromium, so NSS issue doesn't apply

## Solution Options

### Option 1: Add NSS certutil to Chrome entrypoint (Recommended)

Update `chrome/entrypoint.sh` to install certificates into NSS database:

```bash
#!/bin/bash
set -e

if [ -d /swe-swe/certs ] && [ "$(find /swe-swe/certs -type f -name '*.pem' 2>/dev/null | wc -l)" -gt 0 ]; then
    echo "→ Installing enterprise certificates for Chrome..."

    # Install to system CA store (for other tools)
    if cp /swe-swe/certs/*.pem /usr/local/share/ca-certificates/ 2>/dev/null; then
        update-ca-certificates && echo "✓ System CA store updated"
    fi

    # Install to NSS database (for Chromium)
    NSS_DB="/home/chrome/.pki/nssdb"
    mkdir -p "$NSS_DB"
    chown chrome:chrome "$NSS_DB"

    # Initialize NSS database if needed
    if [ ! -f "$NSS_DB/cert9.db" ]; then
        certutil -d sql:"$NSS_DB" -N --empty-password
        chown chrome:chrome "$NSS_DB"/*
    fi

    # Add each certificate to NSS database
    for cert in /swe-swe/certs/*.pem; do
        if [ -f "$cert" ]; then
            certname=$(basename "$cert" .pem)
            certutil -d sql:"$NSS_DB" -A -t "C,," -n "$certname" -i "$cert"
            echo "✓ Added $certname to NSS database"
        fi
    done

    chown -R chrome:chrome "$NSS_DB"
fi

exec "$@"
```

### Option 2: Use Chromium flag to ignore certificate errors (Not recommended for production)

Add `--ignore-certificate-errors` to Chromium launch args in supervisord.conf:

```ini
command=/usr/bin/chromium --ignore-certificate-errors ...
```

**Downside:** Defeats the purpose of SSL validation.

### Option 3: Use system-wide NSS database

Install certificates to `/etc/pki/nssdb/` instead of user-specific database.

### Additional Requirements

1. **Add libnss3-tools to Dockerfile:**
   ```dockerfile
   RUN apt-get update && apt-get install -y libnss3-tools
   ```

2. **Ensure NSS database directory exists before Chromium starts**

## Files to Modify

1. `/workspace/cmd/swe-swe/templates/host/chrome/Dockerfile`
   - Add `libnss3-tools` package

2. `/workspace/cmd/swe-swe/templates/host/chrome/entrypoint.sh`
   - Add NSS certificate installation logic

## Testing Plan

1. Build Chrome container with changes
2. Verify certificates are installed in NSS database:
   ```bash
   docker exec -it chrome-container certutil -d sql:/home/chrome/.pki/nssdb -L
   ```
3. Test HTTPS access via Playwright MCP:
   ```
   mcp__playwright__browser_navigate to https://example.com
   ```

## References

- [Chromium NSS Database](https://chromium.googlesource.com/chromium/src/+/main/docs/linux/cert_management.md)
- [certutil documentation](https://developer.mozilla.org/en-US/docs/Mozilla/Projects/NSS/tools/NSS_Tools_certutil)
- [Debian CA Certificates](https://wiki.debian.org/SecureApt)
