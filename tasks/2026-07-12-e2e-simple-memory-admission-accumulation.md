# Full e2e umbrella goes red from session-memory admission accumulation

Status: open follow-up (NOT a product bug; does not block the current release).

## Symptom

`make test-full-e2e` (and `make test-e2e`) exits red on this box even though the
product code is correct. Failures cluster entirely in the specs that need a
fully-booted, probing session:

- `e2e/tests/terminal-ui-tabs.spec.js` -- 10 tests (tab-switch probe flip,
  localStorage override, mobile nav, blank-regression, Files tab loads md-serve
  at filesProxyPort, preview iframe not stuck on "Connecting...").
- `e2e/tests/tunnel.spec.js` -- 1 test (frontend reflects SWE_PUBLIC_HOSTNAME).

All fail as `page.waitForFunction` 90s timeouts -- the awaited UI state never
arrives because the session never finishes booting.

Specs that only exercise UI forms (login, credentials, env-vars, new-session
dialog, mcp-create-session, agent-browser) all pass.

## Root cause (confirmed 2026-07-12)

Not a regression. It is the server's own memory-admission gate refusing new
top-level sessions once free memory drops too low.

- Gate: `checkMemoryForNewSession()` in
  `cmd/swe-swe/templates/host/swe-swe-server/main.go` (~L7555), called from
  `getOrCreateSession()` (~L4883, top-level sessions only, ParentUUID == "").
- It compares `getMaxSessionRSS()` (the largest active session's WHOLE
  descendant process-tree RSS via `/proc/<pid>/statm`: chromium + node +
  md-serve + agent-chat) against `MemAvailable` from `/proc/meminfo`, and
  returns `insufficient memory: largest session uses <X> but only <Y> available`
  when `maxRSS > avail`.

The single-worker suite accumulates Chrome + npx sidecars across specs, so
`MemAvailable` degrades monotonically. Evidence from surviving Playwright
error-contexts (18 of 22): available fell 1.5 GB -> 1.1 GB -> 1003 MB -> 876 ->
770 -> 561 -> 287 MB. Once the gate flips, every subsequent top-level session is
refused, the chat probe never succeeds, and the dependent tests time out.

## Proof the code is clean

Running only `terminal-ui-tabs` + `tunnel` against a fresh simple container
passed 15/15 (2.5m). Dockerless e2e (4/4) and Agent View remote (1/1) also pass.
Only the long accumulating full run trips the gate.

## Deeper diagnosis (2026-07-13)

Two mechanisms, both in play:

1. RSS over-counting in the gate (primary suspect for FALSE refusals).
   `getProcessTreeRSS` sums `/proc/<pid>/statm` RSS across every process in the
   session tree. A chromium session is ~8 processes (browser, N renderers, gpu,
   utility/network) that SHARE most of their mapped pages (libs, v8 snapshot).
   RSS counts those shared pages once PER process, so the sum wildly
   over-estimates the session's true unique footprint. Live sample on this box:
   chromium procs summed to ~2.8 GB RSS while real unique usage is a fraction of
   that. Result: the gate believes a session "uses 1.6 GB" and refuses new ones
   even when the host has plenty free -> the cascade. Using PSS
   (`/proc/<pid>/smaps_rollup` -> `Pss:`) instead of RSS counts each shared page
   once across the tree and reflects true footprint. This is the cleanest fix
   but it CHANGES production admission behavior, so it needs an explicit
   decision + a full simple-mode e2e re-run to confirm.

2. Failed-test cleanup skip amplifies it. `terminal-ui-tabs` afterEach ends
   sessions only on PASS (failed tests keep their session for inspection). Once
   the gate starts refusing mid-file, subsequent tests fail, skip cleanup, and
   leave sessions resident -> positive feedback loop that drives MemAvailable
   down further (287 MB by the end).

Note: the gate reads host `/proc/meminfo` MemAvailable (NOT cgroup-aware), so a
container memory limit is not the lever in simple mode; the host genuinely
tightens as sidecars accumulate.

Compose-mode variant: the endSessions POST to `/api/session/{uuid}/end` runs the
FULL synchronous teardown in the request path (handleSessionEndAPI ->
endSessionByUUID -> Close: stopSessionAgentView + stopSessionMdServe +
killSessionProcessGroup + killProcessesOnPorts). Ending a browser session (test 9
starts one) can block the response long enough that the afterEach fetch hits
Playwright's 180s hook timeout, then later page.goto's abort. Needs its own
look (bound the browser teardown; or end sessions async and 202 immediately).

## Options (pick one; not mutually exclusive)

1. Raise the e2e-simple (and compose) container memory ceiling so the heaviest
   session's tree stays below MemAvailable across the whole single-worker run.
   Simplest, no code change. Touch: `scripts/e2e-up.sh` / the compose file's
   memory limits.

2. Tighten teardown so sidecars from OTHER specs do not linger. Cross-spec
   leftovers (ports, agent-browser) persist even after their pages close;
   `terminal-ui-tabs` already ends its own sessions in `afterEach` on pass, but
   failed tests skip cleanup by design. Consider a global `afterEach`/
   `globalTeardown` that reaps stray Chrome + npx sidecars between spec files.

3. Make the admission gate less conservative. `getProcessTreeRSS` sums raw RSS
   including shared/cache pages, so it over-counts. Options: discount shared
   pages (use PSS from `/proc/<pid>/smaps_rollup`), or relax the threshold under
   an explicit E2E flag. Higher risk (changes production admission behavior);
   only if 1+2 are insufficient.

Recommendation: start with option 1 (raise the ceiling) since it is zero-risk
and immediately restores a trustworthy one-command gate; add option 2 if the
suite is still marginal. Defer option 3 unless needed.

## Second, separate issue: compose/Traefik e2e mode is stale (no traefik emitted)

Found while trying to run compose mode in isolation on 2026-07-13. Compose
bring-up fails before any test runs:

    service "traefik" has neither an image nor a build context specified:
    invalid compose project

Root cause (NOT a regression from the current release):

- The compose template `cmd/swe-swe/templates/host/docker-compose.yml` gates the
  whole `traefik:` service behind `# {{IF NO_TUNNEL}}` (L2-4, image traefik:v2.11).
- But since 2026-05-21 (commit 5168bdec5), default init is Dockerfile-only:
  `DockerfileOnly = (*sslFlag == "no" && *tunnelServerURL == "")` (init.go ~L952),
  and `ssl` defaults to "no" (init.go ~L628). Dockerfile-only strips the traefik
  service from the generated `docker-compose.yml` (the e2e-compose output has only
  the `swe-swe` service).
- `scripts/e2e-up.sh` compose path passes NO ssl flag (INIT_EXTRA_FLAGS="") yet
  still runs `docker compose up -d swe-swe traefik` and writes a
  `docker-compose.override.yml` that references `traefik`. Mismatch -> invalid
  compose project.

This has been latent: `make test-e2e` runs simple first and fails there (the
memory issue above), so compose mode was never reached to expose it. Net effect:
the Traefik compose path is currently NOT exercised by e2e at all.

Fix LANDED 2026-07-13 (keep-and-fix path). The compose e2e harness now inits with
`--ssl selfsign@host.docker.internal` so `DockerfileOnly=false` and the traefik
service is emitted. Full change set:
- `scripts/e2e-up.sh`: compose INIT_EXTRA_FLAGS = `--ssl selfsign@host.docker.internal`;
  mount the host-translated selfsign TLS certs into traefik; https readiness probe
  (curl -k) for compose; relax the generated `traefik-dynamic.yml` rateLimit
  (average/burst -> 100000) so single-source-IP e2e traffic is not 429'd; write
  SCHEME into the state file.
- `scripts/e2e-test.sh`: build E2E_BASE_URL from SCHEME (compose = https).
- `e2e/playwright.config.js` + `e2e/global-setup.js`: `ignoreHTTPSErrors: true`
  for the self-signed cert.

Verified: compose stack boots over https, `login.spec.js` 4/4 green, and the
first 9 `terminal-ui-tabs` tests green -- proving the traefik/https/cert/rateLimit
wiring is correct.

RESIDUAL (same accumulation class as the memory issue above, tracked here):
from ~test 10 the compose run hits an `afterEach` (endSessions) 180s timeout that
cascades into `page.goto: net::ERR_ABORTED (frame detached)` on every later test.
No "insufficient memory" banner and no 429 this time -- it is a session-teardown
/ resource-accumulation stall specific to the longer compose run, NOT the harness
wiring (9 session-opening tests passed first). Getting the FULL compose suite green
needs the same accumulation fix as options 1-3 above (raise memory / tighten
per-spec teardown so endSessions does not stall). Out of scope for "emit traefik".

If instead compose is being retired per the Dockerfile-only roadmap
([[project_dockerfile_only_roadmap]]): drop the compose e2e mode + this wiring
rather than carry it. Decision still open: keep-and-maintain vs retire.

## Acceptance

- `make test-full-e2e` runs green end-to-end on this box (all four tiers), OR the
  compose tier is intentionally retired and removed from the umbrella.
- No change to production memory-admission behavior unless option 3 is chosen
  and separately justified.
