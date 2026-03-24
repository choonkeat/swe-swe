# Consolidate E2E Testing for Both Modes

## Goal

Replace the fragmented test infrastructure (scripts/e2e.sh for dockerfile-only, scripts/test-container/* for compose) with a unified e2e framework that:
- Tests both **dockerfile-only** and **compose (Traefik)** modes
- Provides standalone `up`/`test`/`down` targets for manual and automated use
- Runs sequentially (one mode at a time) to conserve memory
- Uses the `swe-swe` CLI (not raw docker compose) for realism

## Port Assignments

| Mode | SWE_PORT | Preview Ports | Agent Chat Ports | VNC Ports | Public Ports |
|------|----------|--------------|-----------------|-----------|-------------|
| simple (dockerfile-only) | 9780 | 3200-3219 (ext: 23200-23219) | 4200-4219 (ext: 24200-24219) | 7200-7219 (ext: 27200-27219) | 5200-5219 |
| compose (Traefik) | 9770 | 3100-3119 (ext: 23100-23119) | 4100-4119 (ext: 24100-24119) | 7100-7119 (ext: 27100-27119) | 5100-5119 |

These avoid conflicts with the production stack (default 3000-3019 / 23000-23019).

## Compose Mode Strategy

Use `--with-vscode` flag during `swe-swe init` to trigger compose mode (with Traefik) without SSL. This keeps everything HTTP (no cert issues). Then start only `swe-swe` and `traefik` services via `swe-swe up -d swe-swe traefik` to skip unnecessary VS Code containers and save memory.

---

## Phase 1: Refactor e2e scripts into composable pieces

### What it achieves
Split the monolithic e2e.sh into standalone up/test/down scripts. Add Makefile targets. Verify each manually.

### Steps

1. **Create `scripts/e2e-up.sh <mode>`** (mode = `simple` | `compose`)
   - Accepts MODE as first argument
   - Builds CLI (`make build-cli`)
   - Creates test workspace in `/workspace/tmp/e2e-{mode}/`
   - For `simple`: runs `swe-swe init` with no SSL (dockerfile-only auto-detected)
   - For `compose`: runs `swe-swe init --with-vscode` (triggers Traefik compose)
   - Creates docker-compose.override.yml for Docker-in-Docker path translation
   - Starts container: `swe-swe up -d` (simple) or `swe-swe up -d swe-swe traefik` (compose)
   - Waits for server ready (curl health check)
   - Writes state file (`/workspace/tmp/e2e-{mode}/.e2e-state`) with port, project path, mode
   - **Manual verification**: run `make e2e-up-simple`, visit via MCP browser at `http://host.docker.internal:9780/`, verify login page loads

2. **Create `scripts/e2e-down.sh [mode]`**
   - If mode given, tears down that mode only
   - If no mode, tears down all (simple + compose)
   - Reads state file for project path
   - Runs `swe-swe down` or `docker compose down` (since swe-swe down needs correct working dir)
   - Cleans up state file

3. **Create `scripts/e2e-test.sh [mode]`**
   - If mode given, tests that mode only
   - If no mode, tests all running servers (reads state files)
   - For each running server: runs `npx playwright test` with correct `E2E_BASE_URL`
   - Reports pass/fail per mode

4. **Update Makefile**
   ```
   e2e-up-simple:   scripts/e2e-up.sh simple
   e2e-up-compose:  scripts/e2e-up.sh compose
   e2e-test:        scripts/e2e-test.sh
   e2e-down:        scripts/e2e-down.sh
   test-e2e:        sequential: up-simple, test simple, down simple, up-compose, test compose, down compose
   ```

5. **Manual verification of compose mode**: run `make e2e-up-compose`, visit via MCP browser at `http://host.docker.internal:9770/`, verify login page loads through Traefik

### Verification
- `make e2e-up-simple` starts container, curl returns HTTP 200/302
- `make e2e-up-compose` starts Traefik + swe-swe, curl returns HTTP 200/302
- `make e2e-test` runs existing login.spec.js against both
- `make e2e-down` cleans up all containers
- `make test-e2e` runs the full sequential flow end-to-end
- Existing tests (login, agent-browser) still pass in simple mode

### Regression safety
- Existing Playwright tests are unchanged; they get base URL from env var
- Old e2e.sh kept temporarily (renamed to e2e-legacy.sh) until new framework is validated
- Port ranges chosen to not conflict with production or each other

---

## Phase 2: Add port connectivity tests

### What it achieves
New Playwright tests that verify VNC and preview port mappings work correctly. These would have caught today's VNC port bug.

### Steps

1. **Create `e2e/tests/ports.spec.js`**
   - Test: "preview proxy port responds" -- after login, create a session, verify fetch to preview proxy port returns something (502 Bad Gateway is OK, means proxy is running)
   - Test: "VNC proxy port responds" -- after login, create a session with browser started, verify VNC proxy port serves vnc_lite.html (or at least doesn't give ERR_EMPTY_RESPONSE)
   - Test: "agent chat proxy port responds" -- verify agent chat proxy port returns the waiting page (502 with agent-chat HTML)
   - These tests read port info from the session API response (which includes previewProxyPort, vncProxyPort, agentChatProxyPort)

2. **Update playwright.config.js** if needed
   - May need longer timeout for session creation
   - May need to allow cross-origin requests to proxy ports

### Verification
- Run `make e2e-up-simple && make e2e-test` -- ports.spec.js passes
- Run `make e2e-up-compose && make e2e-test` -- ports.spec.js passes
- Intentionally break VNC port mapping (revert the fix), verify test fails (red-green)

### Regression safety
- New test file, doesn't modify existing tests
- Tests are additive

---

## Phase 3: Consolidate documentation

### What it achieves
Single source of truth for testing, remove stale scripts.

### Steps

1. **Update `docs/dev/swe-swe-server-workflow.md`**
   - Update E2E section to reference new targets (e2e-up-simple, e2e-up-compose, etc.)
   - Add compose mode testing instructions
   - Add manual testing workflow (up, browse, down)

2. **Update `docs/dev/test-container-workflow.md`**
   - Add deprecation notice pointing to new e2e framework
   - Or merge content into swe-swe-server-workflow.md and redirect

3. **Remove old scripts/test-container/** (or mark deprecated)
   - Only after verifying new framework covers all use cases

### Verification
- Docs are accurate by running through the documented commands
- No broken references

### Regression safety
- Old scripts preserved until new framework validated
- Docs changes are non-functional
