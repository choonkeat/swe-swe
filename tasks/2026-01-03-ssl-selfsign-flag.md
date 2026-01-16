# Add `--ssl` Flag to `swe-swe init`

## Goal

Add a `--ssl` flag to `swe-swe init` that supports two values:
- `no` (default) - Port serves HTTP
- `selfsign` - Port serves HTTPS with self-signed certificates (generated using Go crypto libraries)

## Implementation Approach

Following the CLAUDE.md two-commit TDD approach.

---

## Phase 1: Baseline (Flag Parsing Only)

### What will be achieved
- `--ssl` flag parsed and stored in `InitConfig` / `init.json`
- No functional effect yet

### Steps
1. [x] Add `SSL string` field to `InitConfig` struct
2. [x] Add `--ssl` flag parsing in `handleInit()` (validate: `no` or `selfsign`, default `no`)
3. [x] Wire it up: save to config, restore on `--previous-init-flags=reuse`, show in `swe-swe list`
4. [x] Update `printUsage()` docs
5. [x] Add golden variant `with-ssl-selfsign` in `Makefile` and `main_test.go`

### Verification
1. [x] `make build golden-update`
2. [x] `git add -A cmd/swe-swe/testdata/golden && git diff --cached -- cmd/swe-swe/testdata/golden`
3. [x] **Expect**: Only `"ssl": "selfsign"` in `init.json` for new variant; docker-compose.yml, traefik-dynamic.yml etc. unchanged from default
4. [x] `go test ./cmd/swe-swe/...` passes
5. [x] **Commit**

---

## Phase 2: Implementation (Make Flag Take Effect)

### What will be achieved
- When `--ssl=selfsign`, self-signed certificates are generated using Go crypto
- Traefik configured to serve HTTPS instead of HTTP
- Certificates stored in `certs/` directory

### Steps
1. Add `generateSelfSignedCert(certsDir string)` function using Go's `crypto/x509`, `crypto/rsa`, `crypto/rand`
   - Generates `server.crt` and `server.key` in certsDir
   - Cert valid for `localhost`, `127.0.0.1`, and common local hostnames
2. Call `generateSelfSignedCert()` in `handleInit()` when `ssl == "selfsign"`
3. Update `traefik-dynamic.yml` template with `{{IF SSL}}` block for TLS config
4. Update `docker-compose.yml` template:
   - Change entrypoint from `:7000` to `:7443` when SSL
   - Mount certs volume to traefik
5. Process templates with new `ssl` condition (similar to `withDocker`)
6. Update golden variant to verify functional changes

### Verification
1. `make build golden-update`
2. `git add -A cmd/swe-swe/testdata/golden && git diff --cached -- cmd/swe-swe/testdata/golden`
3. **Expect**: `with-ssl-selfsign` shows changes in `docker-compose.yml`, `traefik-dynamic.yml`, and new cert files in `certs/`
4. `go test ./cmd/swe-swe/...` passes
5. **Commit**
6. **Manual test**: `swe-swe init --ssl=selfsign && swe-swe up` → verify HTTPS works

---

## Key Files to Modify

- `cmd/swe-swe/main.go` - Flag parsing, InitConfig struct, cert generation
- `cmd/swe-swe/main_test.go` - Golden test variants
- `cmd/swe-swe/templates/host/docker-compose.yml` - Conditional SSL entrypoint
- `cmd/swe-swe/templates/host/traefik-dynamic.yml` - TLS certificate config
- `Makefile` - Golden variant for `with-ssl-selfsign`

## Design Decisions

- **Go crypto over openssl CLI**: No external dependency, works everywhere
- **Single port**: Same port (1977) serves HTTP or HTTPS depending on flag
- **Self-signed cert hostnames**: localhost, 127.0.0.1, and common local names
