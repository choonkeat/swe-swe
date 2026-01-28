# SSL Let's Encrypt Support

## Goal

Add Let's Encrypt SSL support to swe-swe via `--ssl=letsencrypt@domain.com` flag, leveraging Traefik's native ACME provider for automatic certificate issuance and renewal.

## Key Decisions

- **Email**: Required when using letsencrypt (for expiry warnings if renewal fails)
- **Port 80**: Auto-added to docker-compose for ACME challenges + HTTP→HTTPS redirect
- **SWE_PORT**: Remains dynamic - cert works on any port, port 80 only for ACME
- **Staging mode**: `--ssl=letsencrypt-staging@domain` for internal testing (avoids rate limits)
- **Fail behavior**: Fail hard if cert acquisition fails (no silent fallback)
- **Domain validation**: Check domain resolves at init time (fail fast)

---

## Phase 1: Flag Parsing & Validation ✅ COMPLETE

### What will be achieved
- New flag variants: `--ssl=letsencrypt@domain` and `--ssl=letsencrypt-staging@domain`
- New required flag: `--email=user@example.com` (required when using letsencrypt)
- Domain resolution validation at init time (fail fast if domain doesn't resolve)
- `InitConfig` struct updated to store email and parsed SSL mode
- No template changes yet - just flag infrastructure

### Small steps

1. **Add `--email` flag to init.go** - Optional string flag, required only when ssl=letsencrypt*
2. **Extend SSL flag parsing** - Recognize `letsencrypt@domain` and `letsencrypt-staging@domain` prefixes
3. **Add domain validation helper** - `validateDomain(domain string) error` that does DNS lookup
4. **Update InitConfig struct** - Add `Email string` field, save to init.json
5. **Add validation logic** - Error if letsencrypt without email, error if domain doesn't resolve
6. **Update flag reuse logic** - `--previous-init-flags=reuse` should restore email too

### Workflow

1. **Before starting** - `make build golden-update` to confirm clean baseline (no unexpected diffs)
2. **Add test variants** in `main_test.go` (tests will fail - no implementation yet)
3. **Implement flag parsing** in `init.go`
4. **After implementation** - `make build golden-update` to generate new golden files
5. **Verify diff** - `git diff --cached -- cmd/swe-swe/testdata/golden` should show:
   - New `init.json` files with `ssl` and `email` fields
   - No changes to existing golden files (regression check)
6. **Commit** - Baseline commit with flag parsing + golden tests

### Verification (TDD style)

- **Red**: Add test cases for new variants (expect failures initially)
- **Green**: Implement flag parsing until tests pass
- **Refactor**: Clean up, ensure existing `with-ssl-selfsign` tests still pass
- **Regression guarantee**: All existing golden tests must pass unchanged

---

## Phase 2: Template Conditionals & ACME Configuration

### What will be achieved
- Template processor extended to handle `{{IF LETSENCRYPT}}` markers
- `docker-compose.yml` template updated with port 80 mapping and ACME volumes
- `traefik-dynamic.yml` template updated with ACME certificateResolver config
- HTTP→HTTPS redirect on port 80
- Services use letsencrypt cert resolver instead of static TLS files

### Small steps

1. **Extend `processSimpleTemplate()` in templates.go**
   - Add `{{IF LETSENCRYPT}}` / `{{IF NO_LETSENCRYPT}}` marker handling
   - Pass email and domain as template variables

2. **Update `docker-compose.yml` template**
   - Add port 80 mapping (for ACME challenges + redirect)
   - Add ACME storage volume mount (`~/.swe-swe/acme:/etc/traefik/acme`)
   - Update entrypoints for letsencrypt mode

3. **Update `traefik-dynamic.yml` template**
   - Add `certificatesResolvers.letsencrypt.acme` config block
   - Configure HTTP-01 challenge on port 80 entrypoint
   - Add HTTP→HTTPS redirect middleware
   - Use staging vs production ACME server based on flag

4. **Update service router labels**
   - Services use `tls.certresolver=letsencrypt` instead of static cert

5. **Create `~/.swe-swe/acme/` directory** in init if letsencrypt mode

### Verification (TDD style)

- **Red**: Golden tests from Phase 1 now expect template output (will fail - templates unchanged)
- **Green**: Update templates until golden output matches expected ACME config
- **Refactor**: Ensure `{{IF SSL}}` (selfsign) paths still work

### Regression guarantee
- `with-ssl-selfsign` golden files must not change
- `with-ssl-no` (default) golden files must not change
- Only new `with-ssl-letsencrypt*` variants show ACME config

### After implementation
- `make build golden-update`
- Verify diff shows template changes only in new letsencrypt variants
- Commit - Implementation commit completing two-commit TDD

---

## Phase 3: Golden Tests & Integration

### What will be achieved
- Complete golden test coverage for all letsencrypt variants
- Verified generated configs are syntactically valid YAML
- End-to-end validation that Traefik would accept the generated config
- All existing tests pass (regression-free)

### Small steps

1. **Add test variants in `main_test.go`**
   - `with-ssl-letsencrypt` - production mode with domain + email
   - `with-ssl-letsencrypt-staging` - staging mode with domain + email
   - Negative test: letsencrypt without email (expect error)
   - Negative test: letsencrypt with invalid domain (expect error)

2. **Generate golden files**
   - `make build golden-update`
   - Review generated `docker-compose.yml` for correct port 80, volumes, labels
   - Review generated `traefik-dynamic.yml` for correct ACME resolver config

3. **Manual inspection of golden output**
   - Verify ACME server URL is correct (staging vs production)
   - Verify email placeholder is substituted
   - Verify domain appears in cert config
   - Verify HTTP→HTTPS redirect middleware present

4. **Run full test suite**
   - `make test` passes all variants
   - No regressions in existing `with-ssl-selfsign`, `with-ssl-no` variants

### Verification

- **Golden file diff review**: `git diff -- cmd/swe-swe/testdata/golden` shows only expected additions
- **YAML validity**: Golden files parse as valid YAML (test suite validates this)
- **Regression check**: Existing golden files byte-for-byte identical

### After implementation
- `make build golden-update && make test`
- Commit completes the "implementation" side of the two-commit TDD approach

---

## Phase 4: Documentation

### What will be achieved
- User-facing docs explain how to use `--ssl=letsencrypt@domain`
- Requirements clearly stated (port 80, public domain, email)
- Staging mode documented for internal testing
- Troubleshooting guidance for common issues

### Small steps

1. **Update main usage docs** (likely `docs/` or README)
   - Add `--ssl=letsencrypt@domain.com --email=user@example.com` example
   - Explain port 80 requirement and auto-redirect behavior
   - Explain staging mode for internal testing

2. **Add troubleshooting section**
   - "Domain doesn't resolve" - check DNS
   - "Rate limited" - use staging mode, wait, or use different subdomain
   - "Port 80 blocked" - firewall/cloud security group guidance
   - "Cert not renewing" - check container is running

3. **Update ADR-016 or create new ADR**
   - Document the Let's Encrypt decision
   - Explain why Traefik native ACME vs certbot sidecar
   - Note iOS Safari now works with real certs

4. **Update `swe-swe init --help` output**
   - SSL flag help text includes letsencrypt options:
     ```
     --ssl string
         SSL mode:
         - 'no' (default)
         - 'selfsign' or 'selfsign@hostname'
         - 'letsencrypt@domain.com' (requires --email)
         - 'letsencrypt-staging@domain.com' (internal testing, certs not browser-trusted)
     ```

### Verification

- **Docs review**: Read through as a new user - is it clear?
- **Help output**: `swe-swe init --help` shows new flags correctly
- **No code changes**: This phase is docs-only, `make test` still passes

---

## Summary

| Phase | Commit Type | Key Output |
|-------|-------------|------------|
| 1 | Baseline | Flag parsing, init.json stores ssl+email |
| 2 | Implementation | Templates generate ACME config |
| 3 | Testing | Golden tests verify all variants |
| 4 | Docs | User documentation complete |
