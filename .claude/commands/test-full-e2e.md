---
description: Run the full pre-release suite -- unit + containerized e2e + dockerless + Agent View remote browser
---

Run swe-swe's full test suite: the fast unit gate, the containerized e2e
(simple + compose Traefik modes), the host-native dockerless e2e, and the
Agent View remote-browser e2e. This is the heavy pre-release umbrella; it boots
real containers and drives real browsers, so it is deliberately NOT part of the
fast `make test` gate.

## What runs

`make test-full-e2e` chains these targets left-to-right, fail-fast:

1. `make test` -- fast unit gate (ascii/sync checks + all Go unit tests)
2. `make test-e2e` -- containerized e2e: brings up **simple** (dockerfile-only)
   and **compose** (Traefik) modes, runs Playwright against each, tears down
3. `make test-e2e-dockerless` -- host-native (no Docker) full flow
4. `make test-e2e-agent-view-remote` -- Agent View over a remote browser-backend
   (binary tier; needs a display stack)

Because make prerequisites stop at the first failure, a late failure still
means every earlier tier passed.

## Steps

### 1. Run the suite

```bash
make test-full-e2e
```

- If `$ARGUMENTS` is non-empty, treat it as extra e2e args and pass through:
  `make test-full-e2e E2E_ARGS="$ARGUMENTS"`.
- This is long-running. Use `send_progress` to report which tier is executing
  (unit -> container e2e -> dockerless -> agent-view) rather than going silent.

### 2. On failure

- Do NOT silently retry the whole suite. Report which tier failed and the
  relevant tail of output.
- If only the Agent View tier fails and the environment lacks a display stack,
  note that the other three tiers passed and that agent-view-remote needs a
  display stack (or run the image tier with `make test-e2e-agent-view-remote-image`,
  which needs Docker).
- Containers are torn down by the e2e targets' own traps; if a run is
  interrupted, run `make e2e-down` to clean up leftover containers before
  retrying.

### 3. Report

Reply with a one-line pass/fail per tier and the overall verdict.
