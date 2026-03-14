# Task: `--dockerfile-only` flag for single-container deployment

**Goal**: Add a `--dockerfile-only` flag to `swe-swe init` that generates a single Dockerfile instead of the full docker-compose setup. Enables deployment on platforms like Fly.io, Railway, and Render that only support single containers.

**Key Design Decisions**:
- Server listens on port from `SWE_PORT` env var (default 1977) — same convention as compose mode
- Auth logic (login, cookie verify, rate limiting) embedded directly in swe-swe-server
- Auth activated when `SWE_SWE_PASSWORD` env var is set — same convention as compose mode
- No TLS in container (platform provides it)
- No Traefik, no separate auth service
- Existing docker-compose flow completely unaffected

---

## Phase 1: Baseline flag (TDD commit 1)

**What**: Add `--dockerfile-only` flag to CLI and `InitConfig`. No behavior change yet.

### Steps

- [x] **1.1** Add `DockerfileOnly bool` field to `InitConfig` struct in `cmd/swe-swe/init.go`
- [x] **1.2** Add `--dockerfile-only` flag parsing in `cmd/swe-swe/main.go`, wire to config
- [x] **1.3** Add golden test variant `dockerfile-only` in `cmd/swe-swe/main_test.go`
- [x] **1.4** Run `make build golden-update`

### Verification

- `make test` passes
- Golden diff shows **only** `init.json` change (`"dockerfileOnly": true`)
- No template output differences yet

### Commit

```
feat: add --dockerfile-only flag (baseline, no effect yet)
```

---

## Phase 2: Embed auth middleware in swe-swe-server

**What**: Add auth (login page, cookie verify, rate limiting) to the swe-swe-server. Controlled by `SWE_SWE_PASSWORD` env var — when unset, no auth (existing compose behavior). When set, all routes are wrapped with cookie-based auth.

### Steps

- [ ] **2.1** Create `cmd/swe-swe/templates/host/swe-swe-server/auth.go` with auth logic extracted from `cmd/swe-swe/templates/host/auth/main.go`:
  - Cookie signing/verification (HMAC-SHA256, 7-day expiry)
  - Rate limiter (per-IP, 10 attempts / 5 min window)
  - Login form HTML (GET handler)
  - Login POST handler (password validation, cookie set, redirect)
  - `authMiddleware(next http.Handler) http.Handler` — wraps any handler with cookie check
- [ ] **2.2** In `main.go`, after setting up all handlers:
  - Read `SWE_SWE_PASSWORD` env var
  - If set, register `/swe-swe-auth/login` and wrap `http.DefaultServeMux` with `authMiddleware`
  - If unset, no change (Traefik/auth-service handle it in compose mode)
- [ ] **2.3** Auth middleware exemptions:
  - `/swe-swe-auth/login` — login page itself
  - `/ssl/ca.crt` — certificate download (pre-auth)
  - `/mcp` — uses its own key-based auth
  - Static assets needed for login page (CSS if any)
- [ ] **2.4** Run `make build golden-update` — golden files should be unchanged since auth.go is a new file in the template but doesn't affect init output structure

### Verification

- `make test` passes
- Golden diff shows only the new `auth.go` file appearing in all golden variants
- Unit test: `verifyCookie(signCookie(secret), secret)` returns true
- Unit test: expired cookie returns false
- Unit test: middleware redirects unauthenticated requests
- Unit test: middleware passes through authenticated requests

### Commit

```
feat: embed auth middleware in swe-swe-server (activated by SWE_SWE_PASSWORD)
```

---

## Phase 3: Template generation for dockerfile-only mode

**What**: When `--dockerfile-only` is set, `swe-swe init` generates only the files needed for a single-container deployment. Server uses `SWE_PORT` (default 1977) and `SWE_SWE_PASSWORD` for embedded auth.

### Steps

- [ ] **3.1** In `init.go`, add conditional logic when `DockerfileOnly` is true:
  - **Generate**: `Dockerfile`, `entrypoint.sh`, `.env`, `swe-swe-server/` source, `home/` directory
  - **Skip**: `docker-compose.yml`, `traefik-dynamic.yml`, `auth/` directory
- [ ] **3.2** Modify Dockerfile template (or create variant) for dockerfile-only mode:
  - `EXPOSE ${SWE_PORT:-1977}`
  - `CMD` uses `-addr 0.0.0.0:${SWE_PORT:-1977}` instead of `:9898`
  - `ENV SWE_SWE_PASSWORD=changeme` to activate embedded auth
  - `ENV SWE_PORT=1977`
- [ ] **3.3** Generate `.env` for dockerfile-only mode:
  - `SWE_PORT=1977`
  - `SWE_SWE_PASSWORD=changeme`
  - API keys (same as compose mode)
- [ ] **3.4** Skip `swe-swe up` docker-compose commands when `DockerfileOnly` is true — print instructions for `docker build` and `docker run` instead
- [ ] **3.5** Run `make build golden-update` — dockerfile-only variant should show:
  - No `docker-compose.yml`
  - No `traefik-dynamic.yml`
  - No `auth/` directory
  - Dockerfile with port 1977 and SWE_SWE_PASSWORD

### Verification

- `make test` passes
- Golden diff: `dockerfile-only` variant has correct files, other variants unchanged
- **Playwright test**: Boot test container from generated Dockerfile:
  1. Navigate to `http://localhost:1977` → redirected to `/swe-swe-auth/login`
  2. Enter wrong password → error message shown
  3. Enter correct password → redirected to main UI (assistant selection page)
  4. Subsequent requests use cookie (no re-auth)

### Commit

```
feat: generate single Dockerfile in --dockerfile-only mode
```

---

## Phase 4: Documentation

**What**: ADR and changelog for the new feature.

### Steps

- [ ] **4.1** Write `docs/adr/0037-dockerfile-only-mode.md`:
  - Context: Single-container platforms (Fly, Railway, Render) can't use docker-compose
  - Decision: `--dockerfile-only` flag generates single Dockerfile with embedded auth
  - Trade-offs: No built-in TLS (platform provides it), no Traefik routing features
- [ ] **4.2** Update `CHANGELOG.md` with the feature
- [ ] **4.3** Add usage example to docs (optional):
  ```
  cd my-project
  swe-swe init --dockerfile-only
  docker build -t my-swe -f .swe-swe/Dockerfile .swe-swe/
  docker run -p 1977:1977 -e SWE_SWE_PASSWORD=mypass -v $(pwd):/workspace my-swe
  ```

### Verification

- Docs are accurate and match implementation
- ADR follows existing format (see ADR-0035, ADR-0036)

### Commit

```
docs: add ADR-0037 and usage docs for --dockerfile-only mode
```

---

## Key Files

| File | Role |
|------|------|
| `cmd/swe-swe/init.go` | Init orchestration, `InitConfig` struct |
| `cmd/swe-swe/main.go` | Flag parsing |
| `cmd/swe-swe/main_test.go` | Golden test variants |
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | Server source template |
| `cmd/swe-swe/templates/host/swe-swe-server/auth.go` | **New**: embedded auth |
| `cmd/swe-swe/templates/host/auth/main.go` | Existing standalone auth (unchanged) |
| `cmd/swe-swe/templates/host/Dockerfile` | Container image template |
| `cmd/swe-swe/templates/host/docker-compose.yml` | Compose template (unchanged) |
| `cmd/swe-swe/templates/host/entrypoint.sh` | Container entrypoint |

## Environment Variables (dockerfile-only mode)

| Variable | Default | Purpose |
|----------|---------|---------|
| `SWE_PORT` | `1977` | Server listen port |
| `SWE_SWE_PASSWORD` | `changeme` | Auth password (activates embedded auth) |
| `ANTHROPIC_API_KEY` | — | Claude API key |
| (other API keys) | — | Same as compose mode |
