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
- [ ] Full E2E verification (MCP browser + pwd) deferred to Phase 3

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

- [ ] Run `make build golden-update`
- [ ] Stage golden files: `git add -A cmd/swe-swe/testdata/golden`
- [ ] Review diff: `git diff --cached -- cmd/swe-swe/testdata/golden`
  - Expect changes only in docker-compose.yml (new mount) and swe-swe-server/main.go (path constant)

### Verification

- [ ] `make test` passes (green)
- [ ] Spin up test container per `/workspace/.swe-swe/test-container-workflow.md`
- [ ] `docker inspect <container>` - verify `/worktrees` mount exists
- [ ] Use MCP browser to create/enter a worktree session
- [ ] Verify `pwd` shows `/worktrees/<branch-name>` not `/workspace/.swe-swe/worktrees/<branch-name>`
- [ ] Shut down test container

---

## Phase 4: Documentation

### What will be achieved
Update any existing documentation that references the old worktree path, write ADR-021, and commit all changes.

### Steps

- [ ] Search for existing docs referencing `/workspace/.swe-swe/worktrees`:
  - [ ] Check `docs/adr/0020-git-worktree-integration.md`
  - [ ] Check any README or other docs

- [ ] Update found references to `/worktrees`

- [ ] Write `docs/adr/0021-worktree-mount-path.md`:
  - Context: agents working in nested paths can confuse main repo with worktree
  - Decision: mount `.swe-swe/worktrees` to `/worktrees` in containers
  - Consequences: cleaner path separation, minor docker-compose change

- [ ] Commit all changes with descriptive message

### Verification

- [ ] Review ADR follows existing ADR format in `docs/adr/`
- [ ] Grep for stale `/workspace/.swe-swe/worktrees` references - should find none
- [ ] `make test` still passes
- [ ] `git status` shows clean working tree

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
