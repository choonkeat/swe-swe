# Dockerless e2e harness

## Status

**Phase 1 shipped (curl-based core).** `make e2e-dockerless`
(`scripts/e2e-dockerless.sh`) boots `swe-swe init --dockerless` + `swe-swe up`
with NO Docker daemon and asserts the dockerless contract: all 7 dumped
binaries + `swe-swe-open` shim + browser symlinks + `mode=dockerless` marker +
project `.mcp.json`; the server serves the homepage (200); and a session page
renders `<terminal-ui>` rooted at the project dir (path-agnostic server). Runs
green even on the shared dogfood box (uses clean env + non-colliding port
ranges, avoids the global `@swe-swe-broker`). Keeps the dockerless path
(`tasks/2026-06-27-dockerless-single-binary.md`) from regressing.

**Phase 2 shipped (Playwright live tabs).** `e2e/tests/dockerless-tabs.spec.js`
(gated on `E2E_DOCKERLESS`, open-auth empty cookie jar; `global-setup.js` skips
login in that mode) drives a real browser/websocket against the live dockerless
server and asserts the parts curl cannot reach: session connects + per-session
proxy ports delivered over WS; **Files md-serve (npx) answers on
`filesProxyPort`** (the key host-dependency tab); Preview iframe wired to the
preview proxy. The harness runs it as Phase 6 (`E2E_SKIP_PLAYWRIGHT=1` escape
hatch for bare runners). 3/3 green against a real instance; full harness 18
PASS / 1 WARN / 0 FAIL.

**Umbrella targets (option 2).** `make test-e2e-dockerless` (full, with
Playwright) and `make test-e2e-dockerless-smoke` (`E2E_SKIP_PLAYWRIGHT=1`,
curl-only contract for runners with no chromium/agent CLI) mirror the
container `test-e2e` convention. Deliberately NOT in `make test` -- they boot a
server + drive a browser, too heavy/flaky for the fast unit gate. There is no
GitHub Actions in this repo today; a hosted-CI job (`runs-on: ubuntu-latest` +
`make test-e2e-dockerless`, separate from the unit job) is only meaningful once
CI infra exists.

**Still TODO:** Agent Chat probe-success (needs a working agent / LLM auth) and
**Agent View** (needs the browser stack -- single-binary plan Phase 5,
local/remote variants); the tunnel variant.

## Problem

Every existing e2e runs through Docker. `scripts/e2e.sh` literally
"builds a real container in dockerfile-only mode" and runs `docker
compose build` / `docker compose down`; the Makefile e2e targets
(`e2e-up-simple`, `e2e-up-compose`, `e2e-test`, `e2e-down`) are all
container-based. There is **zero** coverage of a host-native run, so the
dockerless DX could silently rot.

## Goal

A host-native e2e that exercises `swe-swe init --dockerless` + `swe-swe
up` and asserts all six tabs are functional without any Docker daemon.

## Approach

- New target (e.g. `make e2e-dockerless`) that, on a Linux runner with
  the dependencies present (git, npx, an agent CLI, browser stack):
  1. `swe-swe init --dockerless` in a temp repo.
  2. `swe-swe up` (background); wait for `http://localhost:$SWE_PORT`.
  3. Drive the UI with the existing Playwright e2e suite
     (`scripts/e2e-test.sh` uses `npx playwright test`) pointed at the
     host URL instead of the container URL.
  4. Assert each tab: Agent Terminal (PTY streams), Terminal, Preview
     (proxy to a dummy dev server), Files (md-serve), Agent Chat
     (agent-chat MCP up), Agent View (browser starts on demand).
  5. `swe-swe down`; clean temp repo.
- **Agent View coverage** has two variants:
  - `local` backend: requires the browser stack on the runner.
  - `remote` backend: stand up `swe-swe/browser-backend` (see
    `tasks/2026-06-27-browser-backend-service.md`) and run with
    `--agent-view=<url>`. Gate this variant on the backend image
    existing.
- **Tunnel variant** (optional): `--tunnel-server-url` against a test
  tunnel server, assert reachability, no Docker in path.

## Reuse, do not duplicate

- The Playwright assertions in `scripts/e2e-test.sh` are mostly
  URL-agnostic -- parameterize the base URL so the same specs cover both
  container and dockerless runs.
- Keep the agent CLI deterministic for CI (a stub/`-shell` assistant is
  fine for tab-plumbing assertions that do not need a real model).

## Verify

- `make e2e-dockerless` green on a clean Linux box with no running
  Docker daemon (prove it by stopping docker first).
- A deliberately broken tab (e.g. remove `npx` from PATH) fails the Files
  + Agent Chat assertions -- i.e. the harness actually detects breakage.
- CI wiring so the dockerless path runs on PRs alongside the container
  e2e.

## Non-goals

- Replacing the container e2e (`scripts/e2e.sh`) -- both run.
- macOS runners (Linux-only for now).
