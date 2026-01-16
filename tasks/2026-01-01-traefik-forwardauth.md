# Traefik ForwardAuth Implementation Plan

## Goal

Add ForwardAuth to provide unified authentication for swe-swe:

- HTML login form (password manager compatible)
- Session cookies (browser session based, mobile-friendly)
- Single password protecting: vscode, swe-swe, chrome, and Traefik dashboard
- Disable code-server's built-in auth (rely on Traefik layer)
- Built in Go (single binary, minimal image)

All auth endpoints use `/swe-swe-auth/*` namespace to avoid conflicts.

---

## Phase 1: Auth Service Container

### What will be achieved

A standalone Go HTTP service (`auth`) that:
- Listens on port 4180 (internal to docker network only)
- Serves HTML login form at `GET /swe-swe-auth/login`
- Accepts password submission at `POST /swe-swe-auth/login`
- Sets HMAC-signed session cookie on successful login
- Provides `GET /swe-swe-auth/verify` endpoint returning 200 (valid) or 401 (invalid) for Traefik ForwardAuth
- Reads password from `SWE_SWE_PASSWORD` environment variable

### Steps

| Step | Description |
|------|-------------|
| **1.1** | ✅ Project scaffolding — Create `auth/` directory with `go.mod`, empty `main.go`, `main_test.go` |
| **1.2** | ✅ Cookie signing utilities (TDD) — `signCookie()` and `verifyCookie()` functions with HMAC |
| **1.3** | ✅ Verify endpoint (TDD) — `GET /swe-swe-auth/verify` checks cookie, returns 200 or 401 |
| **1.4** | ✅ Login form endpoint (TDD) — `GET /swe-swe-auth/login` serves HTML form |
| **1.5** | ✅ Login POST handler (TDD) — `POST /swe-swe-auth/login` validates password, sets cookie, redirects |
| **1.6** | Wire up main() — HTTP server with routes, read secret from env |
| **1.7** | Dockerfile — Multi-stage build, scratch base, < 20MB image |

### Verification

**Unit/Integration (fast, direct):**
```bash
cd cmd/swe-swe/templates/host/auth
go test ./...                    # unit tests
docker build -t auth-test .      # Dockerfile works
```

**Unit tests (TDD red-green-refactor for steps 1.2–1.5):**
```
TestSignCookie_ProducesNonEmptySignature
TestVerifyCookie_ValidSignature_ReturnsTrue
TestVerifyCookie_TamperedValue_ReturnsFalse
TestVerifyHandler_NoCookie_Returns401
TestVerifyHandler_ValidCookie_Returns200
TestLoginGetHandler_Returns200_WithPasswordField
TestLoginPostHandler_WrongPassword_Returns401
TestLoginPostHandler_CorrectPassword_SetsCookie
```

**Regression (full cycle):**
```bash
go build -o ./dist/swe-swe ./cmd/swe-swe
./dist/swe-swe init /tmp/test-project
cd /tmp/test-project && ./dist/swe-swe up
# verify existing services still work (auth not wired yet)
```

---

## Phase 2: Traefik Integration

### What will be achieved

Traefik configured to:
- Route `/swe-swe-auth/*` to auth service (unprotected — must be accessible to show login form)
- Protect all other routes (swe-swe, vscode, chrome) via ForwardAuth middleware
- Protect Traefik dashboard API
- Disable code-server's built-in auth (`--auth=none`), rely solely on Traefik

### Steps

| Step | Description |
|------|-------------|
| **2.1** | Add auth service to docker-compose.yml — build context, port 4180, `SWE_SWE_PASSWORD` env var, Traefik labels for `/swe-swe-auth` route (high priority, no auth middleware) |
| **2.2** | Add ForwardAuth middleware to traefik-dynamic.yml — points to `http://auth:4180/swe-swe-auth/verify` |
| **2.3** | Apply middleware to protected routers — add `forwardauth@file` to swe-swe, vscode, chrome labels |
| **2.4** | Protect Traefik dashboard — add router with ForwardAuth in dynamic config |
| **2.5** | Disable code-server's built-in auth — change command to `--auth=none`, remove `PASSWORD` env var |

### Verification (MCP Playwright)

**Test 1: Unauthenticated access redirects to login**
1. Navigate to `http://localhost:9899/` → verify redirect to `/swe-swe-auth/login`
2. Navigate to `http://localhost:9899/vscode` → verify redirect to `/swe-swe-auth/login`
3. Navigate to `http://localhost:9899/chrome/` → verify redirect to `/swe-swe-auth/login`

**Test 2: Login flow works**
1. Navigate to `/swe-swe-auth/login`
2. Fill password field with correct password
3. Submit form
4. Verify redirect to `/` (or original URL)
5. Verify cookie is set

**Test 3: Authenticated access works**
1. After login, navigate to `/vscode` → verify access granted (no redirect)
2. Navigate to `/chrome/` → verify access granted
3. Navigate to `/` → verify access granted

**Test 4: Wrong password rejected**
1. Navigate to `/swe-swe-auth/login`
2. Submit wrong password
3. Verify stays on login page (401 or error shown)

**Regression (full cycle):**
```bash
go build -o ./dist/swe-swe ./cmd/swe-swe
./dist/swe-swe init /tmp/test-project
cd /tmp/test-project && ./dist/swe-swe up
# MCP Playwright tests above
```

---

## Phase 3: Polish & Edge Cases

### What will be achieved

Production-ready login experience with:
- Cookie security hardening
- Original URL preserved on redirect (deep linking works)
- Mobile-responsive login form
- Error feedback for wrong password

### Steps

| Step | Description |
|------|-------------|
| **3.1** | Cookie security attributes — `HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` if HTTPS (detect via `X-Forwarded-Proto`) |
| **3.2** | Original URL redirect — read `X-Forwarded-Uri` from ForwardAuth request, pass to login form as query param `?redirect=`, redirect there after successful login |
| **3.3** | Mobile-responsive login — inline CSS with viewport meta, touch-friendly input sizes, centered form |
| **3.4** | Password field attributes — `autocomplete="current-password"` for password manager compatibility |
| **3.5** | Error feedback — wrong password shows inline error message on login page (not just 401 status) |

### Verification (MCP Playwright)

**Test 1: Original URL preserved**
1. Navigate to `http://localhost:9899/vscode` (not logged in)
2. Verify redirect to `/swe-swe-auth/login?redirect=/vscode`
3. Submit correct password
4. Verify redirect to `/vscode` (not `/`)

**Test 2: Deep link preserved**
1. Navigate to `http://localhost:9899/chrome/?some=param`
2. Login
3. Verify redirect back to `/chrome/?some=param`

**Test 3: Mobile viewport**
1. Set viewport to 375x667 (iPhone SE)
2. Navigate to `/swe-swe-auth/login`
3. Verify form elements visible and usable (no horizontal scroll)

**Test 4: Password manager compatibility**
1. Inspect password field
2. Verify `autocomplete="current-password"` attribute present
3. Verify `type="password"` attribute present

**Test 5: Error feedback**
1. Navigate to `/swe-swe-auth/login`
2. Submit wrong password
3. Verify error message visible on page
4. Verify can retry with correct password

**Test 6: Cookie security**
1. Login successfully
2. Inspect cookie attributes
3. Verify `HttpOnly`, `SameSite=Lax`, `Path=/` present

**Regression (full cycle):**
```bash
go build -o ./dist/swe-swe ./cmd/swe-swe
./dist/swe-swe init /tmp/test-project
cd /tmp/test-project && ./dist/swe-swe up
# All Phase 2 tests still pass
# All Phase 3 tests pass
```

---

## File Changes Summary

| File | Change |
|------|--------|
| `cmd/swe-swe/templates/host/auth/go.mod` | New — Go module |
| `cmd/swe-swe/templates/host/auth/main.go` | New — auth service implementation |
| `cmd/swe-swe/templates/host/auth/main_test.go` | New — unit tests |
| `cmd/swe-swe/templates/host/auth/Dockerfile` | New — multi-stage Go build |
| `cmd/swe-swe/templates/host/docker-compose.yml` | Add auth service, update middlewares, modify code-server |
| `cmd/swe-swe/templates/host/traefik-dynamic.yml` | Add forwardAuth middleware config |

---

## Development Workflow

**Phase 1 (auth service in isolation):**
```bash
cd cmd/swe-swe/templates/host/auth
go test ./...                    # TDD cycle
docker build -t auth-test .      # verify Dockerfile
```

**Phase 2+ (full integration):**
```bash
go build -o ./dist/swe-swe ./cmd/swe-swe
./dist/swe-swe init /tmp/test-project
cd /tmp/test-project && ./dist/swe-swe up
# MCP Playwright tests
```
