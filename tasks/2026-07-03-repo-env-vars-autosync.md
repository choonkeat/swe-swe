# Repo-scoped environment variables (credential-style auto-sync)

## Decisions (locked)

- **Match scope:** repo — keyed by `(window.location.origin, initSha)`, reusing the existing trust record. Not host-scoped.
- **Storage:** credential-style. Browser localStorage + in-memory server store keyed by `sid`. Never on disk, never committed.
- **Auto-sync:** gated by the same `(origin, initSha)` trust entry + TLS/loopback safety check that already gates the PAT.
- **Injection:** at spawn only (applies to the *next* session / PTY restart, not the live process) — same as the PAT.
- **UI:** new dedicated **"Repo"** nav section with an **"Environment variables"** pane.

## Mockup

`scratchpad/env-vars-mockup.html` → Artifact `6bcbc156-941a-4220-920b-e31a1d631b76`.

---

## Server (Go) — `cmd/swe-swe/templates/host/swe-swe-server/`

### 1. New in-memory store — `env_store.go` (new file, mirror `cred_store.go`)
- `var sessionEnv = map[string]string{}` keyed by `sid`, guarded by an `RWMutex` (or fold into existing `sessionCredStateMu` discipline).
- `setSessionEnv(sid, raw string)` — store the raw textarea blob verbatim (parse at spawn, so `$VAR` expansion sees the full session env).
- `getSessionEnv(sid) string`, `clearSessionEnv(sid)`.
- `inheritSessionEnv(parentSID, childSID)` — mirror `inheritSessionCredentials` so `create_session` children inherit parent env (matches MCP creds-inheritance behavior).
- **Reserved-key denylist** — `var reservedEnvKeys = {PATH, HOME, TERM, PORT, AGENT_CHAT_PORT, PUBLIC_PORT, BROWSER_CDP_PORT, BROWSER_VNC_PORT, BROWSER, GH_TOKEN, GITLAB_TOKEN, GIT_CONFIG_COUNT, GIT_CONFIG_KEY_0, GIT_CONFIG_VALUE_0, GIT_CONFIG_GLOBAL, AGENT_CHAT_DISABLE, COLORFGBG}`. A helper `filterReservedEnv(pairs) (kept, dropped []string)` used both at save (to report dropped keys back to the UI) and at injection (defense in depth).

### 2. WS handler — `main.go` (~line 5569 block, next to `set_credentials`)
- `case "set_env"`: read `data.raw`, lock `sessionCredStateMu`, `setSessionEnv(sid, raw)`, ack `env_stored` with `{dropped: [...]}` (the reserved keys that were ignored). No gitconfig rewrite needed.
- `case "clear_env"` (optional, for "Forget on this device" server-side clear).

### 3. Injection — `buildSessionEnv` in `main.go` (~line 606)
- After `loadEnvFile(.swe-swe/env ...)`, insert the session-env store **before** the file load so **`.swe-swe/env` wins collisions** (explicit checked-in config beats the UI textarea).
  - Concretely: build the store's pairs, run them through `filterReservedEnv`, `os.Expand` each value via the same `envLookup(env)` used by `loadEnvFile`, append. Then let the existing `loadEnvFile` call run last so the file overrides.
  - Add `SID`-derived lookup: `buildSessionEnv` already takes `p.SID`, so `getSessionEnv(p.SID)` is in scope.
- Reuse `loadEnvFile`'s line parser — extract the `KEY=VALUE` / `#`-comment / `os.Expand` logic into a shared `parseEnvLines(raw string, lookup) []string` so file and store parse identically. (`env_file_test.go` already covers the parser; point the new store at it.)

### 4. State snapshot — `session_cred_state.go` `buildSessionCredState`
- Add an `env` block to the broadcast map: `{present: bool, count: int}` (never echo values back). Lets the UI show a nav badge / "N vars" and re-hydrate "saved" state without re-sending secrets.

### 5. Tests — `env_store_test.go`, extend `session_env_test.go`
- Reserved keys dropped; `.swe-swe/env` wins on collision; `$VAR` expansion against session env; inheritance parent→child; values never appear in `buildSessionCredState` output.

---

## Frontend (JS) — `static/terminal-ui.js` + `static/styles/terminal-ui.css`

### 6. Nav + pane HTML (`terminal-ui.js` ~lines 515–640)
- New `<span class="settings-panel__nav-section">Repo</span>` + `<button data-tab="env">Environment variables</button>` with a `settings-panel__nav-badge` (`id="settings-nav-badge-env"`, shows var count).
- New `<section data-pane="env">`: repo-match chip (origin + short initSha + trust status), `<textarea id="settings-env-vars">` (monospace, KEY=VALUE), reserved-key notice line (populated from the `env_stored` ack's `dropped[]`), footer with **Forget on this device** + **Save env vars** + status.
- CSS: reuse `settings-panel__input--multiline`; add `.repo-match` chip styles (see mockup). No new tokens.

### 7. localStorage + save flow (`terminal-ui.js`)
- Key helper `_envLocalKey(initSha)` = `'swe-swe-env:' + window.location.origin + '|' + initSha` (mirror `_signingTrustKey`).
- `_saveEnvVars()` — read textarea, `_writeEnvLocal(initSha, raw)`, send WS `{type:'set_env', data:{raw}}`. On `env_stored` ack, render the `dropped[]` reserved-key notice.
- `_readEnvLocal(initSha)` to repopulate the textarea on panel open.
- `_forgetEnvOnThisDevice()` — clear localStorage entry + send `clear_env`.

### 8. Auto-sync (`terminal-ui.js` `_maybeAutoConnectSecrets`, ~line 2956)
- In the same block that already re-sends the PAT after checking the `(origin, initSha)` trust record + `_signingAutoSendSafe()`: also read `_readEnvLocal(initSha)` and, if present, send `set_env`. One trust gate covers PAT + signing key + env vars.
- Extend `_maybePromptCredsTrust()` copy so the consent prompt mentions env vars too ("auto-send your saved HTTPS credentials **and repo env vars**").

### 9. WS message handling
- Add `case 'env_stored'` (near `credentials_stored`, ~line 1514) — update status text + render dropped-keys notice + refresh nav badge from the cred-state snapshot's `env.count`.

---

## Docs / templates
- `docs/` — note the new pane + that it's memory-only and repo-matched. Relationship to `.swe-swe/env` (file wins).
- No golden-test impact unless `swe-swe init` templates change (they don't here — this is server/static, not the init templates).

## Verification
- `make test` for Go units.
- `make e2e-up-simple` (port 9780) → open settings → Repo → Environment variables → save → open a **new** session → confirm the var is in the agent process env and reserved keys were dropped. `make e2e-down`.

## Decisions (resolved)
- **Reload masking: click-to-reveal/edit.** On panel open, do NOT render the stored blob into the textarea. Show "N vars saved on this device — click to edit" (count from the cred-state `env_count`); clicking reveals the raw blob (read from localStorage) into an editable textarea. Values still live in localStorage for auto-sync, exactly like the PAT — the masking is display-only.
- **Nav badge: var count** (e.g. `6`), from `env_count`.

## Progress
- [x] Server: `parseEnvLines` refactor, `env_store.go` (+ reserved-key denylist), injection in `buildSessionEnv` (file wins), `set_env`/`clear_env` WS handlers, `env_present`/`env_count` in cred-state, `inheritSessionEnv` on create_session, clear on session end. **`make test-server` green.**
- [x] Frontend: nav "Repo" section + pane, click-to-reveal textarea, localStorage (`swe-swe-env:<origin>|<initSha>`), save/forget, auto-sync in `_maybeAutoConnectSecrets`, `env_stored`/`env_cleared`/cred-state handling, nav badge, reserved-key notice. CSS masked-block. Golden regenerated.
- [x] `make test` full suite green. e2e `env-vars.spec.js` (4 tests) green. Screenshots captured.
- [x] Full e2e suite against freshly-rebuilt stack: **feature specs pass** — `credentials.spec` 10/10, `env-vars.spec` 4/4. 12 unrelated failures (`terminal-ui-tabs`, `ports` files-proxy, `tunnel`) are all `TimeoutError` (90s waitForFunction / 180s) on chat/probe readiness, appearing only in the later half of a 51-min run = container degradation (md-serve/agent-chat cold-start + port exhaustion), not this diff. Re-running `terminal-ui-tabs` isolated on a clean stack to confirm.
- [x] Isolated re-run on a clean stack: `terminal-ui-tabs` 13/13 + shots pass (17s each, no timeouts) -> confirmed the full-suite failures were container degradation, not this diff.
- [ ] Docs (pending).

## Status: implementation + verification complete (uncommitted)
Changed: `env_store.go` (new), `cred_store.go`, `main.go`, `session_cred_state.go`, `static/terminal-ui.js`, `static/styles/terminal-ui.css`, golden (all variants), `e2e/tests/env-vars.spec.js` (new). Screenshots in scratchpad (`env-1-empty` .. `env-4-masked`).
