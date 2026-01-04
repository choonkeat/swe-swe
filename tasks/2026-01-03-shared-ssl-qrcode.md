# Shared Self-Signed SSL with QR Code Mobile Installation

**Date:** 2026-01-03
**Status:** Complete

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

1. **[DONE] Add UUID generation on startup** (`swe-swe-server/main.go`)
   - Generate a random token using `crypto/rand` (16 hex chars)
   - Store in a package-level variable for the handler to access

2. **[DONE] Add certificate file path configuration**
   - Add environment variable: `TLS_CERT_PATH`
   - Default to `/etc/traefik/tls/server.crt`
   - swe-swe-server container needs the tls volume mounted

3. **[DONE] Add HTTP handler for `/ssl/{uuid}/ca.crt`**
   - Route: `GET /ssl/{token}/ca.crt` where `{token}` must match generated UUID
   - Read cert file from configured path
   - Set headers:
     - `Content-Type: application/x-x509-ca-cert`
     - `Content-Disposition: attachment; filename="swe-swe-ca.crt"`
   - Return 404 if token doesn't match

4. **[DONE] Update docker-compose.yml to mount tls into swe-swe-server**
   - Add volume mount to swe-swe service: `${HOME}/.swe-swe/tls:/etc/traefik/tls:ro`

5. **[DONE] Log the download URL on startup**
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

## Phase 3: Homepage Certificate Link (Simplified)

### What was changed

Instead of QR code terminal output (which had issues with log visibility and hostname discovery),
we added a simple download link to the homepage footer.

### Steps

1. **[DONE] Simplify endpoint to fixed path `/ssl/ca.crt`**
   - Removed random token (auth middleware already protects the endpoint)
   - Certificate available at predictable URL

2. **[DONE] Add download link to selection.html footer**
   - Shows "📱 Install SSL Certificate" link when SSL cert exists
   - Conditionally rendered based on `HasSSLCert` template variable

### Why this is better

- No need to see server logs
- No hostname discovery issues
- URL doesn't change on restart
- Users are already looking at the web UI

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
