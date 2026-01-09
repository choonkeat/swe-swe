# Task: Mount Worktrees at /worktrees

## Goal

Mount `.swe-swe/worktrees` to `/worktrees` in containers, so agents work in `/worktrees/fix-issue-xyz` instead of `/workspace/.swe-swe/worktrees/fix-issue-xyz` - providing cleaner path separation between main repo and worktrees.

## Background

**Problem**: Agents in `/workspace/.swe-swe/worktrees/fix-issue-xyz` can navigate up and land in `/workspace/` - the main repo with "same files". This parent-child relationship creates accidental cross-contamination risk.

**Solution**: Mount worktrees at `/worktrees` so they're siblings to `/workspace`, not nested inside it. Going up from `/worktrees/fix-issue-xyz` leads to `/worktrees/` (a directory listing), not a repo.

---

## Phase 1: Docker Mount Configuration

### What will be achieved
Add a new volume mount in docker-compose.yml that maps the host's `.swe-swe/worktrees` directory to `/worktrees` inside both containers.

### Steps

- [x] Edit `cmd/swe-swe/templates/host/docker-compose.yml`:
  - [x] Add mount `${WORKSPACE_DIR:-.}/.swe-swe/worktrees:/worktrees` to `swe-swe` service volumes
  - [x] Add same mount to `code-server` service volumes

### Verification

- [x] `make build` succeeds
- [x] Spin up test container
- [x] `docker inspect <container>` - verify `/worktrees` mount exists in Mounts
- [x] Shut down test container
- [x] Full E2E verification completed in Phase 3

---

## Phase 2: Server Code Update

### What will be achieved
Change the server to use `/worktrees` as the base directory for git worktrees instead of `/workspace/.swe-swe/worktrees`.

### Steps

- [x] Edit `cmd/swe-swe/templates/host/swe-swe-server/main.go`:
  - [x] Line ~1237: Change `var worktreeDir = "/workspace/.swe-swe/worktrees"` to `var worktreeDir = "/worktrees"`

- [x] Edit `cmd/swe-swe/templates/host/swe-swe-server/worktree_test.go`:
  - [x] Update all hardcoded `/workspace/.swe-swe/worktrees` paths to `/worktrees`

### Verification

- [x] `make build` succeeds (compiles)
- [x] `make test` passes (server tests aligned with new path)
- [ ] Phase 3 will regenerate golden files to match

---

## Phase 3: Golden File Regeneration & E2E Verification

### What will be achieved
Regenerate golden test files to reflect the new `/worktrees` path, verify tests pass, and confirm end-to-end that worktree sessions use the new path.

### Steps

- [x] Run `make build golden-update`
- [x] Stage golden files: `git add -A cmd/swe-swe/testdata/golden`
- [x] Review diff: `git diff --cached -- cmd/swe-swe/testdata/golden`
  - Verified: changes only in docker-compose.yml (new mount) and swe-swe-server/main.go (path constant + GitCommit)

### Verification

- [x] `make test` passes (green)
- [x] Spin up test container (using docker-compose workflow per updated test-container-workflow.md)
- [x] `docker inspect <container>` - verify `/worktrees` mount exists
- [x] Verified `/worktrees` accessible inside container via `docker exec`
- [x] Shut down test container
- Note: Full worktree creation requires git repo in /workspace (test setup limitation)

---

## Phase 4: Documentation

### What will be achieved
Update any existing documentation that references the old worktree path, write ADR-021, and commit all changes.

### Steps

- [x] Search for existing docs referencing `/workspace/.swe-swe/worktrees`:
  - [x] Check `docs/adr/0020-git-worktree-integration.md` - found stale path on line 15
  - [x] Check any README or other docs - none found

- [x] Update found references to `/worktrees`

- [x] Write `docs/adr/0021-worktree-mount-path.md`:
  - Context: agents working in nested paths can confuse main repo with worktree
  - Decision: mount `.swe-swe/worktrees` to `/worktrees` in containers
  - Consequences: cleaner path separation, minor docker-compose change

- [x] Commit all changes with descriptive message

### Verification

- [x] Review ADR follows existing ADR format in `docs/adr/`
- [x] Grep for stale `/workspace/.swe-swe/worktrees` references - only task files (historical) remain
- [x] `make test` still passes
- [x] `git status` shows clean working tree

---

## Files to Modify

| File | Change |
|------|--------|
| `cmd/swe-swe/templates/host/docker-compose.yml` | Add `/worktrees` mount to both services |
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | Change `worktreeDir` constant |
| `cmd/swe-swe/templates/host/swe-swe-server/worktree_test.go` | Update hardcoded paths |
| `cmd/swe-swe/testdata/golden/*` | Regenerate via `make golden-update` |
| `docs/adr/0020-git-worktree-integration.md` | Update path references (if any) |
| `docs/adr/0021-worktree-mount-path.md` | New ADR documenting this decision |
