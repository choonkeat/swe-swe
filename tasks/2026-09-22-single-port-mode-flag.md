# `--single-port`: stop opening the per-session proxy ports at all

**Date**: 2026-09-22
**Status**: phase 1 DONE; phases 2-4 pending
**Follows**: `tasks/2026-09-22-path-based-agent-view-and-single-port-option.md`
(DONE -- every pane, including the live Agent View, now has a same-origin path
form and finds it by probing).

## Goal

An operator who KNOWS the box is reachable on one port says so once, and
swe-swe stops opening the per-session proxy listeners entirely: no
`23000-23019` (preview), `24000-24019` (agent chat), `27000-27019` (VNC),
`29000-29019` (files), no `5000-5019` public band. Every pane rides the
same-origin path form it already has, with no reachability probe and no wait.

### Why, in order of value

1. **80 externally-bound listeners per box become 0.** `startProxyListener`
   binds `:port` (all interfaces), 20 slots x 4 proxy bands. On a single-port
   box not one of them is reachable, so they are pure attack surface and pure
   bookkeeping. The public band (20 more) is unreachable there too.
2. **No probe wait.** A firewall that DROPs rather than REFUSEs leaves each
   pane on `PROBE_TIMEOUT` (5s, `static/modules/proxy-base.js`) before falling
   back. Four panes, up to 20s of dead time on exactly the boxes that most need
   to feel responsive. Declared mode skips every probe.
3. **A second, stronger e2e tier** (phase 4). The current
   `proxy-fallback.spec.js` blocks ports *at the browser*; it cannot see that
   the server still bound them. A single-port instance can assert the ports are
   genuinely free, and can run the WHOLE suite rather than one spec.

### What this does NOT replace

`e2e/tests/proxy-fallback.spec.js` stays exactly as it is. It proves swe-swe
*discovers* the working route with no flag set, which is what almost every user
will actually be in. A suite run WITH the flag tests the flag, not the
discovery.

## What the checks turned up

- **Preview, Agent Chat and Files need no frontend change at all.** Each builds
  its port candidate from the advertised proxy port (`buildPortBasedPreviewUrl`
  / `...AgentChatUrl` / `...FilesUrl`), all of which return `null` for a missing
  port, and `proxyCandidates` drops null bases. Files additionally gates its
  whole resolver on `if (this.filesProxyPort)`. Omit the ports from the status
  payload and all three go straight to the path form, zero probes.
- **Agent View is the exception.** It uses `vncProxyPort` as the answer to
  "does this session have a live view at all":
  - `terminal-ui.js` ~6943: `if (paneId === 'browser') return !!this.vncProxyPort && this.agentViewAvailable !== false;`
  - `getBrowserViewUrl()`: `if (!this.vncProxyPort) return null;`
  - the WS status handler ~2141: the tab re-render fires on `vncProxyPort`
    flipping null <-> set.
  Drop the port and the pane disappears instead of falling back. The right
  signal already exists and is already broadcast: `agentViewAvailable`
  (`browser_backend.go`, in the status payload at `main.go` ~1210).
- **The setup page answer shipped in aa1a8cb4f will change.** Its script is
  currently byte-identical to `anyport`'s, asserted in
  `www/setup-page.test.mjs`. With the flag, `oneport` should hand it out, so
  that assertion becomes "the anyport script plus `SWE_SINGLE_PORT=1`".

## Phases

1. Server: the flag, the skipped listeners, the honest status payload.
2. Frontend: decouple Agent View's existence from its proxy port; prove zero
   probes.
3. `swe-swe init` passthrough + the setup page hands out the flag.
4. e2e: a single-port tier, with per-spec gating.

---

## Phase 1 -- Server: `-single-port` / `SWE_SINGLE_PORT` -- DONE

**Landed**: `single_port.go` (the setting, the env precedence, the conflict
check), `single_port_test.go`, and in `main.go` the `-single-port` flag, the
boot-time conflict fail-fast, the omitted proxy-port keys in the status
payload, and `startPerSessionProxyListeners` -- the four listeners extracted
into one function that returns early in single-port mode.

### Deviations from the plan below

1. **Step 4 (skip the public port allocation) was dropped: there is nothing to
   skip.** `publicPortFromPreview` DERIVES 5000+n from the preview port; no
   allocator reserves it and `startProxyListener` never binds it. It reaches
   the session only as the `PUBLIC_PORT` env var (an app may bind it) and the
   status payload's `publicPort`, which the frontend uses solely for the
   end-session "something is still running" check. Both stay.
2. **The `if !singlePortMode` guard lives INSIDE
   `startPerSessionProxyListeners`, not at the call site.** Same one branch,
   but it is the seam the test drives, so "binds nothing" is asserted against
   the function every caller uses rather than against a condition only
   `getOrCreateSession` contains.
3. **Under docker-compose the host ports are still published.** `init.go`
   writes the `23000-23019` / `24000-24019` / `27000-27019` / `29000-29019` /
   `5000-5019` ranges into the compose `ports:` block at init time, so
   docker-proxy binds them on the host whatever the server does. In that
   runtime the flag removes the in-container listeners (the host ports then
   refuse), and the probe-skip and path-form benefits are unchanged; the
   "80 externally-bound listeners become 0" claim holds fully only for
   `--runtime=host`. Making compose omit those lines is a Phase 3 decision.

### Steps (as planned)

**Achieves**: in single-port mode no per-session proxy listener is bound, the
status payload advertises no proxy ports, and an incompatible combination fails
at boot rather than half-working.

### Steps

1. **RED** -- new `single_port_test.go` in the server template:
   - a. `resolveSinglePort(flagVal, flagSet, lookupEnv)` -- flag beats env,
     env beats default; `SWE_SINGLE_PORT` accepts `1`/`true`/`yes` and treats
     `0`/`false`/empty as off. Mirrors `resolvePublicHostname`
     (`public_hostname.go`) so there is one precedence rule in the codebase,
     not two.
   - b. `singlePortConflicts(singlePort, publicHostname, tunnelUnique)` returns
     an error naming BOTH settings. Wildcard mode routes
     `{port}.{host}` to the per-port listeners, and tunneld dials them
     directly; with them gone, either mode is silently broken.
   - c. the status payload for a session in single-port mode carries no
     `previewProxyPort`, `agentChatProxyPort`, `vncProxyPort` or
     `filesProxyPort` key, and still carries `agentViewAvailable`,
     `previewPort`, `cdpPort` and `vncPort` (the real targets, which still
     bind and which the agent tooling uses).
   - d. after `getOrCreateSession` in single-port mode, `net.Listen` on each
     of the four proxy ports SUCCEEDS -- i.e. nothing bound them. This is the
     test the browser-side e2e can never write.
2. **GREEN** -- `main.go`:
   - resolve the flag next to `-public-hostname` into a package-level
     `singlePortMode` (write-once during `main()`, read-only after, same
     discipline as `configuredPublicHostname`);
   - fail fast on a conflict, with the message from step 1b;
   - in the per-session block, wrap the four `sess.trackProxyServer(
     startProxyListener(...))` calls in `if !singlePortMode`. The `sessMux`
     path routes (`/preview/`, `/agentchat/`, `/files/`, `/vnc/`) are
     registered unconditionally -- they ARE the single-port mode;
   - in the status payload builder, omit the four proxy-port keys when
     `singlePortMode`, the way `agentChatProxyPort` is already omitted when
     `agentChatPort == 0`.
3. **REFACTOR** -- the four skips are one `if` around a contiguous block, not
   four scattered conditions.
4. **Also skip the public port allocation.** `5000-5019` is a second, login-free
   origin by definition; in single-port mode it can never be reached, so
   allocating it reserves a port for nothing. Keep the allocation of the real
   target ports (preview/agent-chat/CDP/VNC/files) -- those are actual servers.
5. `make test`, then `make build golden-update`.

### Non-regression

- Default mode (`singlePortMode == false`) takes the identical code path: the
  `if` is the only new branch, and every existing per-port test
  (`session_port_proxy_test.go`, `files_port_test.go`, `proxy_listener_test.go`)
  stays green unchanged.
- `TestVNCPortListenerAnswersProbe` and `TestVNCSameOriginPathRoute` both stay
  green: the port handler and the path route are built independently.

**Estimate**: ~1.5h.

---

## Phase 2 -- Frontend: panes stop using their proxy port as an existence check -- DONE

**Landed**: `static/modules/pane-availability.js` + its test (`agentViewKnown`,
`filesPaneKnown`), the `filesPort` key in the status payload, the terminal-ui
gates rewired onto both helpers, and a probe-count assertion in
`proxy-base.test.js`.

### Deviations from the plan below

1. **Files needed the same fix, and the plan said it did not.** "Files needs no
   frontend change at all" was checked against its URL builders, which do cope
   with a missing port -- but `_isPaneKnown('files')` is `!!this.filesProxyPort`,
   and so are the mobile-nav option and the WS handler's load kick. In
   single-port mode the Files TAB would have disappeared exactly as Agent View
   would. Its existence signal is the new `filesPort` (the real md-serve port,
   advertised in every mode), which the server did not previously send.
2. **The helpers live in a new `pane-availability.js`, not in
   `url-builder.js`.** Neither is a URL builder, and `url-builder.test.js` is
   already two files long.
3. **`agentViewKnown` requires `agentViewAvailable === true`, not
   `!== false`.** Undefined means no status frame has arrived, and the old
   `vncProxyPort` co-gate was what kept the tab hidden until then; without it,
   `!== false` would flash the tab on every page load.
4. **Step 3's "headline" assertion was already true.** `resolveProxyBase`
   marks the path form `trusted` and never probes it, so a path-only candidate
   list already cost zero probes. The test is now written down rather than
   assumed -- it was the one thing nothing asserted.

### Steps (as planned)

**Achieves**: with no proxy ports advertised, all four panes resolve to the
path form immediately and no probe is issued at all.

### Steps

1. **RED** -- `static/modules/` has no home for this (the logic is in
   `terminal-ui.js`), so assert it where it is testable:
   `url-builder.test.js` gains cases for `buildVNCViewerUrl` with no
   `vncProxyPort` in path mode (already passes -- port is unused there), and a
   new small pure helper `agentViewAvailableFor({agentViewAvailable, uuid})`
   extracted from the `_paneAvailable` branch so it CAN be unit-tested. Cases:
   available with no proxy port; unavailable when `agentViewAvailable === false`;
   unavailable with no uuid.
2. **GREEN** -- `terminal-ui.js`:
   - `_paneAvailable('browser')` -> the new helper (no `vncProxyPort`);
   - `getBrowserViewUrl()` -> `if (!this.uuid || this.agentViewAvailable === false) return null;`
     (today: `if (!this.vncProxyPort) return null`);
   - the WS status re-render trigger (~2141) watches `agentViewAvailable`
     flipping as well as `vncProxyPort`, so the tab appears in single-port mode;
   - `_resolveBrowserViewBase()` keeps its `if (!this.vncProxyPort) return null`
     guard -- with no port there is nothing to probe and the path form is
     already what `getBrowserViewUrl()` returns.
3. **RED->GREEN, the headline** -- `proxy-base.test.js`: `resolveProxyBase` with
   `{subdomainBase: null, portBase: null, pathBase: '/x'}` must return the path
   WITHOUT calling the probe once. Assert the probe call count is 0. That is the
   "no 5s wait" promise, and nothing asserts it today.
4. `make test`.

### Non-regression

- With ports advertised (every existing deployment) the candidate lists are
  unchanged, so `e2e/tests/ports.spec.js` ("Agent View prefers its own port")
  and `agent-view-remote.spec.js` stay green.
- `terminal-ui-tabs.spec.js` covers the Agent View tab appearing on
  `browserStarted`; the trigger change must not break it.

**Estimate**: ~1h.

---

## Phase 3 -- `swe-swe init` passthrough + the setup page hands out the flag

**Achieves**: `SWE_SINGLE_PORT=1 swe-swe up` reaches the server in every
runtime, and the setup page's `oneport` answer produces it.

### Steps

1. `cmd/swe-swe/init.go` ~1623: add `- SWE_SINGLE_PORT=${SWE_SINGLE_PORT:-}`
   beside the existing `SWE_PUBLIC_HOSTNAME` passthrough in the compose
   template. Check the host-native (`--runtime=host`) path carries it too.
2. `make build golden-update`; the diff must be that one line per variant.
3. `www/index.html`: `envPairs()` pushes `SWE_SINGLE_PORT=1` when
   `onePortOnly()`; `ordinary()` drops `oneport` again (its script is no longer
   the plain one).
4. `www/setup-page.test.mjs`: the `oneport` assertion changes from "identical to
   anyport" to "the anyport lines plus `SWE_SINGLE_PORT=1`". Written first.
5. The consequence box gains one line: with the flag set, the extra ports are
   not opened at all.

### Non-regression

- The other four answers keep their current scripts -- `setup-page.test.mjs`
  covers all five, which is why it was written in the previous task.

**Estimate**: ~1h.

---

## Phase 4 -- e2e: a single-port tier

**Achieves**: the whole default suite runs against an instance that was TOLD it
has one port, plus the one assertion the browser-side test cannot make.

### Steps

1. `scripts/e2e-up.sh`: accept `single-port` as a fourth mode (port `9790`, band
   offset `400`, same shape as `simple`), exporting `SWE_SINGLE_PORT=1`.
   `make e2e-up-single-port`.
2. Gate the specs that are meaningless or wrong in that mode behind
   `test.skip(!!process.env.E2E_SINGLE_PORT, ...)`:
   `ports.spec.js` (no proxy ports exist), `public-hostname.spec.js` and
   `preview-vhost.spec.js` (wildcard mode is a rejected combination),
   `proxy-fallback.spec.js` (it tests discovery, which is off).
3. New `single-port.spec.js`: the four proxy ports are NOT listening (fetch
   them and expect a connection error), every pane still loads, and
   `_acProxyMode` / `_filesProxyMode` / `_browserViewProxyMode` are all `path`
   with the Agent View canvas up.
4. Fold into `make test-full-e2e`.

### Non-regression

- The three existing tiers are untouched; the new one is additive and opt-in,
  which matters given the known memory pressure of running e2e environments
  (see `tasks/` notes on e2e-simple memory accumulation).

**Estimate**: ~1.5h. Total: ~5h.

---

## Deliberately out of scope

- Auto-detecting a single-port box. The probe already does that, at a 5s cost
  only on DROP firewalls; a flag the operator sets is honest and cheap. Making
  the timeout configurable is a smaller, separate change if the wait is the
  real complaint.
- Making the per-session target ports (preview app, agent chat, md-serve,
  websockify, CDP) bind loopback-only in this mode. Worth doing, but it is a
  different blast radius and belongs in its own change.
- A `--single-port` equivalent for the browser-backend box.
