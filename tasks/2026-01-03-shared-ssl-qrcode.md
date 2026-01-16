# Shared Self-Signed SSL with QR Code Mobile Installation

**Date:** 2026-01-03
**Status:** In Progress (Phase 1 Complete)

## Goal

Implement shared self-signed SSL certificates with mobile-friendly QR code installation flow for swe-swe.

## Problem

Currently:
- Self-signed certs are generated per-project at `~/.swe-swe/projects/<hash>/tls/`
- Users must trust a new cert for each project
- Mobile (iOS Safari) cert installation is tedious — no easy way to get the cert onto the device
- Cloud VPC users can't use AirDrop

## Solution

1. Generate cert once at shared location (`~/.swe-swe/tls/`)
2. Serve cert via random URL path (`/ssl/{uuid}/ca.crt`) for secure download
3. Print QR code to terminal on server startup for easy mobile scanning

---

## Phase 1: Shared Certificate Location

### What will be achieved

Self-signed certificates will be generated once at `~/.swe-swe/tls/` and reused across all projects:
- First `swe-swe init --ssl=selfsign` creates the cert
- Subsequent inits reuse the existing cert
- Users only need to trust one cert for all projects
- docker-compose mounts the shared location instead of per-project tls dir

### Steps

1. **[DONE] Modify `generateSelfSignedCert` call location in `handleInit()`** (`main.go:903-912`)
   - Change `tlsDir` from `filepath.Join(sweDir, "tls")` to `filepath.Join(homeDir, ".swe-swe", "tls")`
   - Add existence check: only generate if `server.crt` doesn't exist
   - Keep the "Generated self-signed SSL certificate" message, but add "Reusing existing SSL certificate" for reuse case

2. **[DONE] Update `docker-compose.yml` template** (`templates/host/docker-compose.yml`)
   - Find the tls volume mount (currently `./tls:/etc/traefik/tls:ro`)
   - Change to mount from `~/.swe-swe/tls` using `${HOME}/.swe-swe/tls`

3. **[DONE] Update golden tests**
   - The `with-ssl-selfsign` golden test will need updating to reflect new mount path

### Verification

**Red (failing test first):**
- Modify `main_test.go` to add a test case that verifies:
  - Cert is created at `$HOME/.swe-swe/tls/server.crt` (not in project dir)
  - docker-compose.yml mounts from `${HOME}/.swe-swe/tls`
  - Running init twice with `--ssl=selfsign` doesn't regenerate the cert

**Green (make it pass):**
- Implement the changes above
- Run `make build golden-update`
- Verify golden diff shows only the mount path change

**Manual verification:**
```bash
# Clean slate
rm -rf ~/.swe-swe/tls

# First init
swe-swe init --ssl=selfsign --project-directory /tmp/project1
ls ~/.swe-swe/tls/  # should have server.crt, server.key
stat ~/.swe-swe/tls/server.crt  # note mtime

# Second init (different project)
swe-swe init --ssl=selfsign --project-directory /tmp/project2
stat ~/.swe-swe/tls/server.crt  # mtime should be unchanged

# Verify mount in docker-compose
grep -A2 "tls:" ~/.swe-swe/projects/*/docker-compose.yml
```

---

## Phase 2: Certificate Download Endpoint

### What will be achieved

The swe-swe-server will serve the SSL certificate at a random URL path (`/ssl/{uuid}/ca.crt`) that changes on each boot:
- Returns the certificate with `Content-Type: application/x-x509-ca-cert`
- iOS Safari recognizes it as a certificate and prompts "Profile Downloaded"
- UUID prevents predictable URLs

### Steps

1. **Add UUID generation on startup** (`swe-swe-server/main.go`)
   - Generate a random token using `crypto/rand` (16 hex chars)
   - Store in a package-level variable for the handler to access

2. **Add certificate file path configuration**
   - Add environment variable: `TLS_CERT_PATH`
   - Default to `/etc/traefik/tls/server.crt`
   - swe-swe-server container needs the tls volume mounted

3. **Add HTTP handler for `/ssl/{uuid}/ca.crt`**
   - Route: `GET /ssl/{token}/ca.crt` where `{token}` must match generated UUID
   - Read cert file from configured path
   - Set headers:
     - `Content-Type: application/x-x509-ca-cert`
     - `Content-Disposition: attachment; filename="swe-swe-ca.crt"`
   - Return 404 if token doesn't match

4. **Update docker-compose.yml to mount tls into swe-swe-server**
   - Add volume mount to swe-swe service: `${HOME}/.swe-swe/tls:/etc/traefik/tls:ro`

5. **Log the download URL on startup**
   - Print: `SSL certificate available at: https://<host>/ssl/{uuid}/ca.crt`

### Verification

**Red (failing test first):**
```go
func TestSSLCertEndpoint(t *testing.T) {
    // Setup: create temp cert file
    // Start server with TLS_CERT_PATH pointing to it
    // GET /ssl/{wrong-token}/ca.crt → expect 404
    // GET /ssl/{correct-token}/ca.crt → expect 200, correct Content-Type, cert content
}
```

**Green (make it pass):**
- Implement the handler and routing
- Run tests

**Manual verification:**
```bash
# Start server using ./scripts (ask user for host:port)
# curl to verify correct Content-Type and cert content
# curl with wrong token to verify 404
# Test on iOS Safari → should prompt "Profile Downloaded"
```

---

## Phase 3: QR Code Terminal Output

### What will be achieved

On server startup, a QR code is printed to terminal containing the certificate download URL. Users scan with phone camera and are taken directly to the cert download.

### Steps

1. **Add QR code library dependency**
   - Add `github.com/skip2/go-qrcode` to `swe-swe-server/go.mod`

2. **Create QR code generation function**
   - Input: the full URL
   - Output: ASCII/Unicode art string for terminal
   - Use `qrcode.New(url, qrcode.Medium).ToSmallString(false)`

3. **Determine the server's accessible URL**
   - Use environment variable `SWE_SWE_HOST` (user provides, e.g., `192.168.1.5:443`)
   - If not set, print URL without QR and note "Set SWE_SWE_HOST to enable QR code"

4. **Print QR code on startup**
   ```
   ══════════════════════════════════════════
   📱 Scan to install SSL certificate:

   [QR CODE HERE]

   Or visit: https://192.168.1.5/ssl/a3f2b1c9/ca.crt
   ══════════════════════════════════════════
   ```

5. **Update docker-compose.yml to pass host info**
   - Add `SWE_SWE_HOST` environment variable to swe-swe service

### Verification

**Red (failing test first):**
```go
func TestGenerateQRCode(t *testing.T) {
    url := "https://example.com/ssl/abc123/ca.crt"
    qr := generateQRCodeString(url)
    // Verify non-empty
    // Verify contains expected Unicode block characters
    // Verify reasonable dimensions
}
```

**Green (make it pass):**
- Implement `generateQRCodeString()` function
- Wire into startup sequence
- Run tests

**Manual verification:**
```bash
# Start server using ./scripts (ask user for host:port)
# Observe terminal output - should see QR code
# Scan QR with phone camera
# Verify it opens the correct URL in browser
```

---

## Files to Modify

| File | Changes |
|------|---------|
| `cmd/swe-swe/main.go` | Shared cert location, existence check |
| `cmd/swe-swe/templates/host/docker-compose.yml` | Mount shared tls dir, add to swe-swe service |
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | Add `/ssl/{uuid}/ca.crt` endpoint, QR code output |
| `cmd/swe-swe/templates/host/swe-swe-server/go.mod` | Add qrcode dependency |
| `cmd/swe-swe/main_test.go` | Update/add tests for shared cert |
| Golden test files | Update expected outputs |

---

## Success Criteria

1. `swe-swe init --ssl=selfsign` generates cert at `~/.swe-swe/tls/` (once)
2. Multiple projects reuse the same cert
3. Server prints QR code on startup
4. Scanning QR on mobile opens cert download URL
5. iOS Safari prompts to install certificate profile
6. All existing tests pass
