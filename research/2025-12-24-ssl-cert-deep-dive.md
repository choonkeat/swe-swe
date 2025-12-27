# Enterprise SSL Certificate Handling in swe-swe: Deep Research

## Problem Summary
When running `swe-swe run`, the Claude CLI fails to connect with:
- Error: `SELF_SIGNED_CERT_IN_CHAIN`
- Warning: `Ignoring extra certs from '/Users/choonkeatchew/.ssl/Cloudflare_CA.pem'`

The warning reveals the root cause: host environment variables with host paths are being passed into the Docker container.

## Root Cause Analysis

### The Environment Variable Flow
1. **Host setup**: User has `NODE_EXTRA_CA_CERTS=/Users/choonkeatchew/.ssl/Cloudflare_CA.pem` in their shell environment
2. **swe-swe init**: Detects this env var, copies the cert to `.swe-swe/certs/Cloudflare_CA.pem`, creates `.swe-swe/.env` with:
   ```
   NODE_EXTRA_CA_CERTS=/swe-swe/certs/Cloudflare_CA.pem
   ```
3. **swe-swe run** (main.go:206-207): Does `env := os.Environ()` which captures **ALL host environment variables**, including the original `NODE_EXTRA_CA_CERTS=/Users/...`
4. **docker-compose**: Receives two conflicting sources:
   - Loads `.env` file from current directory with container paths
   - Also receives host environment variables passed via `syscall.Exec` with host paths
5. **Inside container**: Both paths exist as env vars, but:
   - Host path doesn't exist → error/warning
   - Container path should work, but host path takes precedence or causes confusion

### Docker-Compose Env Loading Order
Docker-compose loads environment variables in this order:
1. `.env` file in the same directory as docker-compose.yml (`.swe-swe/.env`)
2. Environment variables passed to the docker-compose process
3. Environment variables in the service definition (docker-compose.yml)
4. Environment variables in `.env.override` if it exists

**The problem**: Host environment variables passed to docker-compose (step 2) don't override .env file variables, but they are still visible inside the container if explicitly set. This creates confusion because the container has both the host path and container path.

### Why NODE_EXTRA_CA_CERTS Fails Anyway
Even if NODE_EXTRA_CA_CERTS correctly points to the container path:
1. **Node.js respects NODE_EXTRA_CA_CERTS** for Node.js processes, BUT:
   - It only **adds** extra certificates to Node's built-in trust store
   - Does NOT replace the system CA bundle entirely
   - Self-signed root CAs must be properly chained

2. **The SELF_SIGNED_CERT_IN_CHAIN error** means:
   - Claude CLI is using an HTTPS library (probably Node's native https module)
   - The certificate chain includes a self-signed root
   - Even with NODE_EXTRA_CA_CERTS, Node.js might not trust the chain if:
     - The root cert isn't in the bundle
     - The intermediate certs aren't properly ordered
     - The cert file format is wrong (needs PEM with proper line endings)

3. **System tools bypass NODE_EXTRA_CA_CERTS**:
   - curl, wget, apt-get, npm (for package downloads) use system OpenSSL
   - They don't read NODE_EXTRA_CA_CERTS at all
   - They only trust certs in the system CA store (`/etc/ssl/certs`)

## Current Code Issues

### Issue 1: Environment Variable Leakage (main.go:206-240)
```go
env := os.Environ()  // Captures ALL host env vars
// ...
if err := syscall.Exec(executable, args, env); err != nil {
```

**Problem**: The host's `NODE_EXTRA_CA_CERTS=/Users/...` is passed into docker-compose, which passes it to the container.

**Solution**: Filter out certificate-related env vars before passing to docker-compose:
```go
// Filter out cert env vars that have host paths
var filteredEnv []string
for _, envVar := range os.Environ() {
    if !strings.HasPrefix(envVar, "NODE_EXTRA_CA_CERTS=") &&
       !strings.HasPrefix(envVar, "SSL_CERT_FILE=") &&
       !strings.HasPrefix(envVar, "NODE_EXTRA_CA_CERTS_BUNDLE=") {
        filteredEnv = append(filteredEnv, envVar)
    }
}
```

### Issue 2: Certificate Not in System Trust Store
The Dockerfile creates the comment (lines 42-53) but doesn't implement it:
```dockerfile
# This enables tools like curl, npm, and apt-get to use custom certificates.
```

**Current state**: Comment only, no actual implementation

**Missing code**:
```dockerfile
# Install enterprise certificates into system trust store
COPY certs/ /usr/local/share/ca-certificates/extra/
RUN update-ca-certificates
```

But this only works if certificates exist at build time. Since certs are copied by swe-swe init, we need conditional logic.

### Issue 3: Certificate Validation Issues
The certificate might not be a complete chain:
- Root CA might be self-signed
- Intermediate CAs might be missing
- Bundle order might be wrong (leaf → intermediate → root)

**Missing validation**: swe-swe init doesn't validate certificate format or chain.

## Solution Architecture

### Three-Layer Approach

#### Layer 1: Clean Environment Passing (Quick Fix)
**What**: Filter certificate env vars in `swe-swe run` so host paths don't leak into containers

**Why**: Prevents confusion and allows .env file to be authoritative

**Implementation**: Modify main.go:206-240 to filter NODE_EXTRA_CA_CERTS, SSL_CERT_FILE, NODE_EXTRA_CA_CERTS_BUNDLE

**Impact**: ✅ Prevents host path errors, 🔲 doesn't fix SELF_SIGNED_CERT_IN_CHAIN yet

#### Layer 2: System Certificate Installation (Robust Fix)
**What**: Modify Dockerfile to install certs from .swe-swe/certs into system trust store

**Why**: Makes certs available to ALL tools (curl, npm, openssl, Node.js), not just NODE_EXTRA_CA_CERTS

**Implementation options**:
- **Option A (Conditional)**: Check if /swe-swe/certs/ exists and has files, then copy + update-ca-certificates
- **Option B (Entrypoint script)**: Create entrypoint that runs update-ca-certificates at startup
- **Option C (Hybrid)**: Both A and B for maximum compatibility

**Code change**:
```dockerfile
# After the npm install, add:
RUN if [ -d /swe-swe/certs ] && [ "$(ls -A /swe-swe/certs)" ]; then \
    cp /swe-swe/certs/*.pem /usr/local/share/ca-certificates/ 2>/dev/null || true && \
    update-ca-certificates; \
fi
```

Wait - this requires certs to exist at build time. But certs are copied by swe-swe init BEFORE the image is built.

**Better approach**: Create an entrypoint script
```dockerfile
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
```

entrypoint.sh:
```bash
#!/bin/bash
# Install any certificates mounted at /swe-swe/certs
if [ -d /swe-swe/certs ] && [ "$(ls -A /swe-swe/certs)" ]; then
    cp /swe-swe/certs/*.pem /usr/local/share/ca-certificates/ 2>/dev/null || true
    update-ca-certificates
fi

# Continue with original command
exec "$@"
```

**Impact**: ✅ Works for all tools, ✅ Dynamic cert loading, ✅ No rebuild needed

#### Layer 3: Certificate Validation (Quality Fix)
**What**: Validate certificates in swe-swe init

**Why**: Catch issues early, provide clear error messages

**Implementation**:
- Parse PEM files to validate format
- Check for self-signed roots
- Warn if certificate is expired or about to expire
- Validate certificate chain can be built

**Impact**: ✅ Better debugging, ✅ Clear error messages, 🔲 Doesn't fix technical issues but helps users understand them

## Implementation Plan

### Phase 1: Quick Win (Environment Filtering)
1. Modify main.go handleRun() to filter NODE_EXTRA_CA_CERTS, SSL_CERT_FILE, NODE_EXTRA_CA_CERTS_BUNDLE
2. Test that host paths don't appear in container warning messages
3. Rebuild and test with `swe-swe run`

**Expected outcome**: No more `/Users/...` warning, but SELF_SIGNED_CERT_IN_CHAIN might still occur

### Phase 2: System Trust Store (Robust Fix)
1. Create entrypoint.sh script in Dockerfile
2. Script automatically installs certs from /swe-swe/certs into system store
3. Ensure CMD passes through to original command
4. Rebuild and test

**Expected outcome**: All tools (curl, npm, Node.js) can use certificates, SELF_SIGNED_CERT_IN_CHAIN should be resolved

### Phase 3: Certificate Validation (Optional)
1. Add certificate parsing and validation in handleCertificates()
2. Display warnings about expired certs, self-signed roots, etc.
3. Provide guidance on certificate issues

**Expected outcome**: Better error messages and user education

## Technical Notes

### PEM Certificate Format
- Multiple certs can be in one file (concatenated)
- Order matters: leaf → intermediate CAs → root
- Format: `-----BEGIN CERTIFICATE-----` to `-----END CERTIFICATE-----`
- Need proper line endings (LF, not CRLF)

### update-ca-certificates Behavior
- Reads `.pem` files from `/usr/local/share/ca-certificates/`
- Creates hash symlinks in `/etc/ssl/certs/`
- Updates `/etc/ssl/certs/ca-certificates.crt` (bundle file)
- Works on Debian, Ubuntu, Alpine, etc.

### Docker Entry Point Best Practice
- Keep original CMD: `CMD ["/usr/local/bin/swe-swe-server", ...]`
- Use ENTRYPOINT for setup: `ENTRYPOINT ["entrypoint.sh"]`
- Entrypoint runs, then execs the CMD
- Preserves signal handling for Ctrl+C

### Node.js Certificate Handling
- NODE_EXTRA_CA_CERTS: Point to PEM file with extra CAs
- Adds to built-in bundle, doesn't replace it
- Only works for Node.js processes
- Also respects system CA store if available

## Files to Modify

1. `cmd/swe-swe/main.go`: Filter env vars in handleRun()
2. `cmd/swe-swe/templates/Dockerfile`: Add entrypoint script
3. `cmd/swe-swe/templates/entrypoint.sh`: New file for cert installation
4. `cmd/swe-swe/templates/docker-compose.yml`: Optional volume mount for certs (already there)

## Testing Strategy

### Test 1: Environment Variables
```bash
swe-swe init
swe-swe run  # Should not show warning about /Users/... paths
```

### Test 2: Certificate Installation
```bash
docker exec swe-swe-server ls /etc/ssl/certs/ | grep -i cloudflare
docker exec swe-swe-server curl https://api.anthropic.com/  # Should not fail with SELF_SIGNED_CERT_IN_CHAIN
```

### Test 3: Claude CLI
```bash
docker exec swe-swe-server claude version
docker exec swe-swe-server claude chat "test message"
```

### Test 4: System Tools
```bash
docker exec swe-swe-server curl https://api.anthropic.com/
docker exec swe-swe-server npm install somepackage  # Over HTTPS
```

## Summary

The current architecture has good infrastructure (environment detection, cert copying, mounting) but is incomplete:

1. **Host paths leak into containers** → Filter in handleRun()
2. **Certs not in system trust store** → Use entrypoint.sh to install
3. **No validation or user feedback** → Add cert validation in init (optional)

The recommended solution is a **two-layer approach**:
- Layer 1 (Quick): Filter environment variables in handleRun()
- Layer 2 (Robust): Add entrypoint.sh to install certs into system trust store

This addresses both the warning about host paths AND the SELF_SIGNED_CERT_IN_CHAIN error.
