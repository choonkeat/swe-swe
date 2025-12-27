# Chrome Container NSS SSL Certificate Fix

**Created:** 2025-12-27
**Status:** Not Started

## Problem Summary

Chromium in the Chrome container fails HTTPS connections with enterprise SSL certificates because:
- Current implementation uses `update-ca-certificates` (system CA store)
- Chromium uses NSS database (`~/.pki/nssdb`), not the system CA store
- Certificates are never added to NSS, so Chromium doesn't trust them

## Implementation Steps

### Step 1: Add libnss3-tools to Chrome Dockerfile
- [ ] Add `libnss3-tools` package to apt-get install in `chrome/Dockerfile`
- [ ] Test: Build Dockerfile locally, verify `certutil` command exists

**Files:** `cmd/swe-swe/templates/host/chrome/Dockerfile`

**Test:**
```bash
# User runs outside container:
# 1. Copy updated template to metadata dir (or rebuild binary + init)
# 2. Build and verify:
swe-swe build chrome
swe-swe up chrome -- -d
docker exec <chrome-container> which certutil
# Expected: /usr/bin/certutil
swe-swe down chrome
```

**Commit:** `fix(chrome): add libnss3-tools for NSS certificate management`

---

### Step 2: Update chrome/entrypoint.sh to install certs into NSS database
- [ ] Create NSS database directory `/home/chrome/.pki/nssdb`
- [ ] Initialize NSS database with `certutil -N --empty-password`
- [ ] Add each .pem certificate using `certutil -A -t "C,,"`
- [ ] Set proper ownership for chrome user
- [ ] Keep existing system CA store installation (for curl, wget in container)

**Files:** `cmd/swe-swe/templates/host/chrome/entrypoint.sh`

**Test:**
```bash
# User runs outside container:
swe-swe build chrome
swe-swe up chrome -- -d
docker exec <chrome-container> certutil -d sql:/home/chrome/.pki/nssdb -L
# Expected: List of installed certificates (or "No certificates found" if no certs mounted)
swe-swe down chrome
```

**Commit:** `fix(chrome): install enterprise certs into NSS database for Chromium`

---

### Step 3: Rebuild swe-swe binary with updated templates
- [ ] Run `make build` to rebuild swe-swe with embedded template changes
- [ ] Test: Extract templates from new binary, verify changes are included

**Files:** `dist/swe-swe*` (binaries)

**Test:**
```bash
# Inside dev environment:
make build
# Verify templates are embedded:
./dist/swe-swe init --help  # Should work
```

**Commit:** Not needed (binaries not committed)

---

### Step 4: Integration test with enterprise SSL certificate
- [ ] Run `swe-swe init` in a test directory
- [ ] Verify chrome/Dockerfile contains libnss3-tools
- [ ] Verify chrome/entrypoint.sh contains NSS installation logic
- [ ] User runs `swe-swe build` and `swe-swe up`
- [ ] Test HTTPS via Playwright MCP

**Test:**
```bash
# User runs:
export NODE_EXTRA_CA_CERTS=/path/to/enterprise-cert.pem
swe-swe init
swe-swe build chrome
swe-swe up chrome -- -d

# Then in Claude Code with Playwright MCP:
# Navigate to https://example.com - should work without SSL errors

# Cleanup:
swe-swe down chrome
```

**Commit:** None (integration test only)

---

### Step 5: Update documentation (if any)
- [ ] Check if any docs reference SSL certificate handling
- [ ] Update if necessary

**Commit:** `docs: update SSL certificate handling for Chrome container` (if applicable)

---

## Progress Log

| Step | Status | Date | Notes |
|------|--------|------|-------|
| 1 | Complete | 2025-12-27 | Added libnss3-tools, verified certutil at /usr/bin/certutil |
| 2 | Complete | 2025-12-27 | NSS database created, Cloudflare_CA installed with C,, trust |
| 3 | Complete | 2025-12-27 | Binary rebuilt, templates extracted via swe-swe init |
| 4 | Complete | 2025-12-27 | HTTPS works! https://example.com loaded without SSL errors |
| 5 | Skipped | 2025-12-27 | No docs need updating |

## Files Modified

- `cmd/swe-swe/templates/host/chrome/Dockerfile`
- `cmd/swe-swe/templates/host/chrome/entrypoint.sh`

## Rollback Plan

If issues arise, revert the two commits:
```bash
git revert HEAD~2..HEAD
```

## Notes

- The chrome user runs Chromium, so NSS database must be at `/home/chrome/.pki/nssdb`
- `certutil -A -t "C,,"` adds cert as trusted CA for SSL (C = trusted CA)
- Keep system CA store update for non-Chromium tools (curl, wget)
- Entrypoint runs as root, so it can create dirs and set ownership before exec
