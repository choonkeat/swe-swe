# Custom Session Environment Variables via `swe-swe/env`

## Goal

Allow users to set custom environment variables for their agent sessions via a `swe-swe/env` file (relative to the session's working directory), with three discoverability paths:

1. **Go-side loading** — vars loaded into every session automatically
2. **New Session dialog** — hint/status line about the mechanism
3. **Setup wizard** — interactive step during onboarding

## Phase 1: Go-side env loading ✅

**What will be achieved:** `buildSessionEnv()` in `swe-swe-server/main.go` loads `swe-swe/env` from the session's working directory and appends those vars to the session environment.

### Steps

1. Add `loadEnvFile(path string) []string` near `buildSessionEnv` (~line 368):
   - Read file with `os.ReadFile`
   - Split by newline
   - Skip empty lines and `#` comments
   - Validate `KEY=value` format via `strings.Cut`
   - Return slice of valid entries; return `nil` on read error (file missing = silent no-op)

2. Change `buildSessionEnv` signature to accept `workDir string`.

3. Append `loadEnvFile(filepath.Join(workDir, "swe-swe", "env"))` **after** the swe-swe vars so user vars take precedence on conflict.

4. Update both call sites to pass the working directory:
   - `restartProcess` (~line 907): pass `s.WorkDir`
   - `getOrCreateSession` (~line 4592): pass `workDir`

### Verification

- `make test` — no regression
- Dev server workflow (`make run` per `docs/dev/swe-swe-server-workflow.md`):
  - Create `swe-swe/env` with `FOO=bar`
  - Start a session via MCP Playwright
  - Confirm `FOO=bar` is in the session environment
  - Remove the file, confirm no error on session start

## Phase 2: New Session dialog hint ✅

**What will be achieved:** The New Session dialog shows a contextual hint about `swe-swe/env`. When the file exists: "Loading environment from `swe-swe/env`". When missing: "Tip: set custom env vars in `swe-swe/env`".

### Steps

1. In the `/api/repo/prepare` response (Go side), add a boolean field `hasEnvFile`:
   - Check `os.Stat(filepath.Join(repoPath, "swe-swe", "env"))`
   - Return the result in the JSON response

2. In `new-session-dialog.js`, after `prepareRepo()` succeeds, read the `hasEnvFile` flag from the response.

3. In `index.html`, add a muted hint line in the `post-prepare-fields` section (near agent selector or footer):
   - File present: "Loading environment from `swe-swe/env`"
   - File missing: "Tip: set custom env vars in `swe-swe/env`"

4. Style as dim/secondary text — consistent with existing dialog styling.

### Verification

- `make test` — no regression
- Dev server (`make run`) + MCP Playwright:
  - Open New Session dialog, select workspace, confirm hint appears
  - Create `swe-swe/env`, reopen dialog, confirm hint changes to active message
  - Remove file, confirm it reverts to tip

## Phase 3: Setup wizard integration ✅

**What will be achieved:** The `swe-swe/setup` script includes a new step that asks users if they want to set custom environment variables and writes them to `swe-swe/env`.

### Steps

1. In `cmd/swe-swe/templates/container/swe-swe/setup`, add new task 5 (renumber existing 5 → 6) for "Custom Environment Variables":
   - Check if `swe-swe/env` already exists
   - If yes: show current contents, ask if user wants to modify
   - If no: ask "Do you need any custom environment variables for your sessions?" with example (e.g., `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`)
   - If yes: collect key=value pairs, write to `swe-swe/env`
   - If no: skip

2. Mirror the same change in:
   - `cmd/swe-swe/slash-commands/swe-swe/setup.md`
   - `cmd/swe-swe/slash-commands/swe-swe/setup.toml`

3. Run `make build golden-update` and verify golden diff only shows the new setup step.

### Verification

- `make build golden-update` — inspect golden diff, confirm only the new section appears
- `make test` — no regression
- Dev server (`make run`) + MCP Playwright: start a session, run `/setup`, confirm the new env vars step appears in the conversational flow at the right position
