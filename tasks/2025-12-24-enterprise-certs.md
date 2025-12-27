# Task: Enterprise certificate support for docker-compose

## Overview
Users behind corporate firewalls or using VPNs (Cloudflare Warp, corporate proxies) need custom CA certificates for Docker operations.

Currently, docker-compose build/run fails with SSL certificate errors if the system has custom CAs.

This task adds support for:
- Detecting `NODE_EXTRA_CA_CERTS` and `SSL_CERT_FILE` environment variables during `swe-swe init`
- Injecting certificates into docker-compose.yml for containers
- Mounting certificate files into containers for tools that need them

## Implementation Steps

### Phase 1: Detect certificate environment variables during init

#### Step 1.1: Check for certificate env vars in handleInit
- [ ] Detect `NODE_EXTRA_CA_CERTS` environment variable
- [ ] Detect `SSL_CERT_FILE` environment variable
- [ ] Detect `NODE_EXTRA_CA_CERTS_BUNDLE` (alternative format)
- [ ] Store detected certificates in `.swe-swe/certs` directory
- [ ] Create `.swe-swe/.env` file with cert configuration
- **Test**: `NODE_EXTRA_CA_CERTS=/path/to/cert.pem swe-swe init` stores cert

#### Step 1.2: Copy certificate files to .swe-swe/certs
- [ ] If cert file exists, copy to `.swe-swe/certs/`
- [ ] Validate certificate file is readable PEM format
- [ ] Log which certificates are being copied
- [ ] Create `.swe-swe/.env` with paths for docker-compose to read
- **Test**: Certificate file successfully copied and readable

### Phase 2: Inject certificates into docker-compose.yml

#### Step 2.1: Update docker-compose.yml template
- [ ] Add volume mounts for certificates in services
- [ ] Add environment variable pass-through for NODE_EXTRA_CA_CERTS
- [ ] Add environment variable pass-through for SSL_CERT_FILE
- [ ] Reference `.env` file for certificate paths
- **Test**: `docker-compose config` shows cert volumes and env vars

#### Step 2.2: Update Dockerfile template
- [ ] Document how to enable custom certificates in containers
- [ ] Add comments for users who need to extend with cert handling
- **Test**: Template is clear and editable

### Phase 3: Handle certificate injection at runtime

#### Step 3.1: Verify certificates work with docker
- [ ] Test `docker-compose build` with cert mounted
- [ ] Verify curl inside container can access HTTPS with custom CA
- [ ] Document expected behavior
- **Test**: Docker build succeeds with custom cert mounted

---

## Progress Tracking

- [x] Phase 1 complete (cert detection)
  - Step 1.1: ✅ handleCertificates detects NODE_EXTRA_CA_CERTS, SSL_CERT_FILE, NODE_EXTRA_CA_CERTS_BUNDLE
  - Step 1.2: ✅ Certificates copied to .swe-swe/certs/, .env file created with container paths
  - Verified graceful fallback when no certs present

- [x] Phase 2 complete (template updates)
  - Step 2.1: ✅ docker-compose.yml updated with cert volume mounts
  - Step 2.1: ✅ Environment variables passed through for NODE_EXTRA_CA_CERTS and SSL_CERT_FILE
  - Step 2.2: ✅ Dockerfile template includes documentation for users to extend with system cert installation
  - Verified docker-compose config includes all certificate references

- [x] Phase 3 complete (runtime validation)
  - Full docker-compose build succeeds with certificates mounted
  - All regression tests pass
  - Certificate-only test scenario passes
  - No-certificate scenario passes (graceful fallback)

---

## Files to Modify
- `cmd/swe-swe/main.go` - handleInit function
- `cmd/swe-swe/templates/docker-compose.yml` - add cert volumes and env vars
- `cmd/swe-swe/templates/Dockerfile` - add cert documentation

---

## Testing Strategy

```bash
# Test with Cloudflare Warp or enterprise cert
export NODE_EXTRA_CA_CERTS=/opt/homebrew/etc/ca-certificates/cert.pem
swe-swe init --path ./my-project

# Verify cert was detected and copied
test -f ./my-project/.swe-swe/certs/cert.pem

# Verify env file created
cat ./my-project/.swe-swe/.env

# Verify docker-compose has cert mounts
docker-compose -f ./my-project/.swe-swe/docker-compose.yml config | grep -A5 volumes

# Test build with cert
docker-compose -f ./my-project/.swe-swe/docker-compose.yml build --no-cache
```

---

## Summary of Implementation

### What Was Built
1. **Certificate Detection** (`cmd/swe-swe/main.go::handleCertificates`)
   - Detects NODE_EXTRA_CA_CERTS, SSL_CERT_FILE, NODE_EXTRA_CA_CERTS_BUNDLE env vars
   - Validates certificates exist before copying
   - Copies to `.swe-swe/certs/` directory
   - Creates `.swe-swe/.env` with container-side paths

2. **Template Updates**
   - `docker-compose.yml`: Added certificate volume mounts and env var pass-through
   - `Dockerfile`: Added documentation for users to extend with system cert installation

3. **Behavior**
   - ✅ Graceful: No error if certs don't exist or env vars not set
   - ✅ Automatic: docker-compose reads .env file automatically
   - ✅ Extensible: Users can edit Dockerfile to install certs into system trust store
   - ✅ Cross-platform: Works with Cloudflare Warp, corporate proxies, etc

### User Experience
**Without certificates (default):**
```bash
swe-swe init --path my-project
# No .env file, no certs directory populated
# Everything works as before
```

**With Cloudflare Warp or corporate cert:**
```bash
export NODE_EXTRA_CA_CERTS=~/.ssl/my-ca.pem
swe-swe init --path my-project
# ✓ Copied enterprise certificate...
# ✓ Created .swe-swe/.env with certificate configuration
# Certificates automatically available in containers
```

### Advanced: System-Level Certificate Installation
Users can extend the Dockerfile to install certs in system trust store:
```dockerfile
# In .swe-swe/Dockerfile
FROM golang:1.23-bookworm
# ... existing setup ...

# Copy certificates mounted by docker-compose
COPY /swe-swe/certs/ /usr/local/share/ca-certificates/extra/
RUN update-ca-certificates

# Now curl, npm, pip, etc. all use custom CA
```

## Notes
- Certificate paths are absolute (copied to container during init)
- Support both single cert and cert bundle files
- Cert mounting is optional (only if cert exists in `.swe-swe/certs/`)
- Default behavior unchanged for users without custom certs
- No failure if cert detection fails (logs warning instead)
- Certificates are read-only mounted (`:ro`) in containers for safety
