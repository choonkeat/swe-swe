# Task: Ensure swe-swe files exist in all session working directories

## Goal

Ensure all session working directories (external repo clones, new projects, and their worktrees) get the same swe-swe template files that `swe-swe init` places into `/workspace`, using two tiers:

- **Tier 1 (Full init)**: Embed container template files in the server binary and write them into base repo directories (`/repos/*/workspace`) at prepare time. Also create an initial empty commit for new projects with git user fallback.
- **Tier 2 (Ensure from base)**: Replace `copyUntrackedFiles` + `copySweSweDocsDir` with a unified `ensureSweSweFiles` that symlinks all swe-swe files from the base repo into worktrees. Skip-if-exists. Source is always the worktree's own base repo, not `/workspace`.

## Context: 5 Session Scenarios

| # | Scenario | Current pwd | Target pwd | Treatment |
|---|----------|-------------|------------|-----------|
| 1 | Default workspace, default branch | `/workspace` | `/workspace` | Already init'd. No change. |
| 2 | Default workspace, branch B | `/worktrees/B` | `/worktrees/B` | Tier 2: ensureSweSweFiles from `/workspace` |
| 3 | Git repo URL, default branch | `/repos/{sanitized}/workspace` | `/repos/{sanitized}/workspace` | **Tier 1: setupSweSweFiles from templates** |
| 4 | Git repo URL, branch B | `/repos/{sanitized}/worktrees/B` | `/repos/{sanitized}/worktrees/B` | Tier 1 on base (if needed), then Tier 2 from `/repos/{sanitized}/workspace` |
| 5 | New project N | `/repos/N/worktrees/N` (bug) | `/repos/N/workspace` | **Tier 1 + empty commit + no worktree** |

## Key source files

- `cmd/swe-swe/init.go` — `swe-swe init`, places container template files into project dir
- `cmd/swe-swe/templates/host/swe-swe-server/main.go` — server, session creation, worktree logic
- `cmd/swe-swe/templates/host/swe-swe-server/static/homepage-main.js` — frontend new session dialog
- `cmd/swe-swe/templates/container/` — source template files (`.mcp.json`, `.swe-swe/docs/*`, `swe-swe/setup`)

---

## Phase 1: Embed container templates in the server & implement Tier 1 setup

### What will be achieved

When a user opens an external repo clone or creates a new project, the base directory (`/repos/*/workspace`) gets the same swe-swe template files that `swe-swe init` places into `/workspace`. New projects also get an initial empty commit (with git user fallback). New projects no longer trigger worktree creation.

### Steps

#### 1a. Copy container templates into server source during `swe-swe init` ✅

In `init.go`, after extracting host files to the metadata directory, also copy the container template files into `{metadataDir}/swe-swe-server/container-templates/`. This avoids duplicating files in the source tree — the container templates exist once in `cmd/swe-swe/templates/container/`, and `init.go` places them where the Docker build can see them.

Files to copy:
- `.mcp.json`
- `.swe-swe/docs/AGENTS.md`
- `.swe-swe/docs/browser-automation.md`
- `.swe-swe/docs/app-preview.md`
- `.swe-swe/docs/docker.md`
- `swe-swe/setup`

Source: `cmd/swe-swe/templates/container/` (embedded in swe-swe CLI binary)
Destination: `{metadataDir}/swe-swe-server/container-templates/` (available at Docker build time)

#### 1b. Embed container templates in server binary ✅

In `main.go` (the server), add:
```go
//go:embed container-templates/*
var containerTemplatesFS embed.FS
```

Add a function `setupSweSweFiles(destDir string, agents []AssistantConfig)` that:
- Walks the embedded FS
- Writes each file to `destDir` at its relative path (strip `container-templates/` prefix)
- Skips if destination file/dir already exists (idempotent)
- Conditionally includes `swe-swe/setup` only if non-slash-command agents are configured
- Include `.swe-swe/docs/docker.md` unconditionally (simpler; it's documentation)

#### 1c. Call `setupSweSweFiles` in prepare handlers ✅

- `handleRepoPrepareClone` (main.go:3019): after successful clone or fetch, call `setupSweSweFiles(repoPath, availableAssistants)`
- `handleRepoPrepareCreate` (main.go:3080): after successful git init + commit, call `setupSweSweFiles(repoPath, availableAssistants)`

#### 1d. Empty commit with git user fallback for new projects ✅ (prior commit)

In `handleRepoPrepareCreate`, after `git init`:
1. Attempt `git -C {repoPath} commit --allow-empty -m "init"`
2. If it fails (likely missing user.name/user.email):
   - Get system username via `os/user.Current()`
   - Get system hostname via `os.Hostname()`
   - Run `git -C {repoPath} config user.email "{username}@{hostname}"`
   - Run `git -C {repoPath} config user.name "{username}"`
   - Retry the empty commit
3. This ensures the repo has a commit on its default branch, needed for future `git worktree add`.

#### 1e. Skip worktree for new projects ✅ (prior commit)

**Frontend** (`homepage-main.js:528-535`): For new projects (`dialogState.isNewProject`), pass the project name via a separate `sessionName` query param instead of `name`. The `name` param currently drives both display name and worktree creation.

```js
// Before (current):
var sessionName = dialogState.isNewProject ? dialogState.projectName : dialogState.selectedBranch;
if (sessionName) {
    url += '&name=' + encodeURIComponent(sessionName);
}

// After:
if (dialogState.isNewProject) {
    url += '&sessionName=' + encodeURIComponent(dialogState.projectName);
} else if (dialogState.selectedBranch) {
    url += '&name=' + encodeURIComponent(dialogState.selectedBranch);
}
```

**Server** (`handleWebSocket`, main.go:3648): Read `sessionName` query param. If present, use it for display name but don't pass it as `name` to `getOrCreateSession` (so no worktree is created).

**Server** (`terminal-ui.js:714`): Forward `sessionName` param to WebSocket URL alongside `name` and `pwd`.

#### 1f. Golden file update ✅

Run `make build golden-update` and verify diffs show only the new `container-templates/` directory added to the server source extraction.

### Verification

- **Unit test for `setupSweSweFiles`**: temp dir, call function, assert all expected files exist with correct content. Call again, assert no overwrites.
- **Unit test for empty commit + fallback**: temp dir with `git init`, call the commit logic, verify `git log` shows "init".
- **Regression**: `make test` — existing golden tests and worktree tests pass. Golden diff shows only `container-templates/`.

---

## Phase 2: Unify file propagation into `ensureSweSweFiles` (Tier 2)

### What will be achieved

Worktree creation uses a single `ensureSweSweFiles` function that symlinks all swe-swe files (both files and dirs) from the base repo into the worktree. Source is always the worktree's own base repo (`repoPath`). `copyUntrackedFiles` and `copySweSweDocsDir` are removed.

### Steps

#### 2a. Write `ensureSweSweFiles(srcDir, destDir string)` ✅

```go
func ensureSweSweFiles(srcDir, destDir string) error
```

Logic:
1. Read top-level entries in `srcDir`
2. For each entry matching: starts with `.` (except `.git`), or is `CLAUDE.md`, `AGENTS.md`, or `swe-swe/`:
   - If tracked in git in `srcDir` → skip (git worktree already has it)
   - If destination already exists → skip
   - Create an absolute symlink: `destDir/{name}` → `srcDir/{name}`
3. Log what was symlinked

Key differences from old `copyUntrackedFiles`:
- Symlinks **everything** (files and dirs), not just dirs
- `.swe-swe` is **no longer excluded** — it gets symlinked, so `.swe-swe/docs/*` comes along automatically (absorbs `copySweSweDocsDir`)
- Skip-if-exists check at destination (current code overwrites files)
- Also processes `swe-swe/` directory (non-dot, non-CLAUDE/AGENTS entry)

#### 2b. Update `excludeFromCopy` ✅

Rename to `excludeFromSymlink` (or inline). Remove `.swe-swe` from the list — only `.git` remains:

```go
var excludeFromSymlink = []string{".git"}
```

#### 2c. Replace calls in `createWorktreeInRepo` ✅

At main.go:3280-3286, replace:
```go
copyUntrackedFiles(repoPath, worktreePath)
copySweSweDocsDir(repoPath, worktreePath)
```
with:
```go
ensureSweSweFiles(repoPath, worktreePath)
```

#### 2d. Remove dead code ✅

- Delete `copyUntrackedFiles` (main.go:2527-2593)
- Delete `copySweSweDocsDir` (main.go:2488-2521)
- Delete `copyFileOrDir` (main.go:2435-2484) if no other callers
- Keep `isTrackedInGit` (used by `ensureSweSweFiles`)

### Verification

- **Unit test for `ensureSweSweFiles`**: temp source dir with dotfiles, dotdirs, `CLAUDE.md`, `.swe-swe/docs/`, `.git/`. Assert:
  - All expected entries are symlinks pointing to source
  - `.git` was not symlinked
  - Symlinks resolve to correct targets
  - Call again — no errors, no duplicate symlinks
  - Pre-create a file in dest, call again — that file is not overwritten
- **Existing worktree tests**: `make test` passes
- **Regression**: golden files should not change in this phase

---

## Phase 3: Integration testing & cleanup

### What will be achieved

All 5 scenarios verified end-to-end via dev server + MCP browser. Dead code removed, comments updated.

### Steps

#### 3a. End-to-end verification

Use dev server workflow (`make run` + MCP browser at `http://swe-swe:3000`).

For each scenario, verify inside the session terminal:

| # | Verify pwd | Verify worktree | Verify swe-swe files |
|---|-----------|-----------------|---------------------|
| 1 | `pwd` = `/workspace` | `git worktree list` shows only main | `.mcp.json`, `.swe-swe/docs/` exist |
| 2 | `pwd` = `/worktrees/{branch}` | Is a worktree | `.mcp.json` etc. are symlinks → `/workspace` |
| 3 | `pwd` = `/repos/{sanitized}/workspace` | Not a worktree | `.mcp.json`, `.swe-swe/docs/` exist (real files from templates) |
| 4 | `pwd` = `/repos/{sanitized}/worktrees/{branch}` | Is a worktree | Symlinks → `/repos/{sanitized}/workspace` |
| 5 | `pwd` = `/repos/{name}/workspace` | Not a worktree | `.mcp.json`, `.swe-swe/docs/` exist; `git log` shows "init" commit |

#### 3b. Verify idempotency

- Open scenario 3 twice with same repo URL — second time fetches, `setupSweSweFiles` skips existing files
- Open scenario 5, try creating same project name — gets "already exists" error
- Reconnect to existing worktree — `ensureSweSweFiles` is no-op

#### 3c. Verify tracked-file edge case

- In `/workspace`, track `.mcp.json` in git
- Create worktree (scenario 2) — `.mcp.json` present via git, not as a symlink

#### 3d. Clean up comments

- Update comments on `createWorktreeInRepo` to reference `ensureSweSweFiles`
- Update/remove stale variable comments
- Remove any orphaned helper functions

#### 3e. Final test run

- `make test` — all existing + new tests pass
- `make build golden-update` — review and commit golden diffs
