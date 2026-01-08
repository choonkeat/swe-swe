# Copy Untracked Files to Worktree

## Goal

When creating a git worktree for a named session, automatically copy relevant untracked files (dotfiles, `CLAUDE.md`, `AGENTS.md`) into the worktree so the development environment "just works" without manual setup.

## Background

Git worktrees only include tracked files. Untracked files important for development are missing:
- `.env`, `.env.local` - environment variables
- `.claude/` - Claude Code settings (memory, MCP configs)
- `CLAUDE.md`, `AGENTS.md` - agent instructions (if gitignored)
- Other agent configs (`.aider.conf.yml`, `.codex`, etc.)

Reference: https://github.com/chuyeow/tiny-tools/blob/main/wt.sh

## Files to Modify

- `cmd/swe-swe/templates/host/swe-swe-server/main.go`

---

## Phase 1: Add `copyUntrackedFiles` helper function

### What will be achieved
A reusable Go function that copies untracked files/directories from a source directory to a destination worktree, preserving relative paths.

### Steps

1. **Define the exclusion list constant**
   ```go
   var excludeFromCopy = []string{".git", ".swe-swe"}
   ```

2. **Create `isTrackedInGit` helper function**
   - Takes a file path relative to repo root
   - Runs `git ls-files --error-unmatch <path>`
   - Returns `true` if exit code is 0 (tracked), `false` otherwise

3. **Create `copyUntrackedFiles` function**
   - Takes `srcDir` and `destDir`
   - Lists all entries in `srcDir` matching patterns: files starting with `.`, `CLAUDE.md`, `AGENTS.md`
   - For each entry:
     - Skip if in exclusion list
     - Skip if tracked in git
     - Copy recursively to `destDir` preserving relative path

4. **Create `copyFileOrDir` helper function**
   - Handles both files and directories
   - For directories: recursively copies contents
   - Preserves file permissions

### Verification

Unit tests with temp git repos:
- Test `isTrackedInGit` with tracked/untracked files
- Test `copyUntrackedFiles` with various file combinations
- Test exclusion list (`.git`, `.swe-swe` not copied)

---

## Phase 2: Integrate into `createWorktree`

### What will be achieved
The `createWorktree` function calls `copyUntrackedFiles` after successfully creating the worktree.

### Steps

1. **Add `getGitRoot` helper function**
   - Runs `git rev-parse --show-toplevel`
   - Returns the repo root path

2. **Modify `createWorktree` function**
   - After successful `git worktree add`, call `getGitRoot()` to get source
   - Call `copyUntrackedFiles(gitRoot, worktreePath)`
   - Log which files were copied
   - If copy fails, log warning but don't fail worktree creation (graceful degradation)

### Verification

Unit tests:
- Test `createWorktree` end-to-end with temp git repos
- Verify untracked files are copied
- Verify graceful failure if copy fails

---

## Phase 3: Comprehensive Table-Driven Tests

### What will be achieved
Comprehensive test coverage for all permutations.

### Test Helper

```go
func setupTestGitRepo(t *testing.T, files map[string]struct{tracked bool, content string}) string
```
- Creates temp dir, `git init`, configures user, creates files, commits tracked ones

### Table-driven tests for `isTrackedInGit`

| Test Case | File | Tracked in Git | Expected |
|-----------|------|----------------|----------|
| tracked file | `README.md` | yes | `true` |
| untracked file | `.env` | no | `false` |
| tracked dotfile | `.gitignore` | yes | `true` |
| untracked dotfile | `.claude/settings.json` | no | `false` |
| nonexistent file | `missing.txt` | no | `false` |
| tracked in subdir | `src/main.go` | yes | `true` |
| untracked in subdir | `src/.env.local` | no | `false` |

### Table-driven tests for `copyUntrackedFiles`

| Test Case | Source Files | Expected Copied | Expected NOT Copied |
|-----------|--------------|-----------------|---------------------|
| basic dotfiles | `.env` (untracked), `.gitignore` (tracked) | `.env` | `.gitignore` |
| CLAUDE.md untracked | `CLAUDE.md` (untracked) | `CLAUDE.md` | - |
| CLAUDE.md tracked | `CLAUDE.md` (tracked) | - | `CLAUDE.md` |
| AGENTS.md untracked | `AGENTS.md` (untracked) | `AGENTS.md` | - |
| nested dotdir | `.claude/settings.json` (untracked) | `.claude/settings.json` | - |
| excluded .git | `.git/config` (always exists) | - | `.git/` |
| excluded .swe-swe | `.swe-swe/recordings/` (untracked) | - | `.swe-swe/` |
| mixed scenario | `.env`, `.claude/`, `CLAUDE.md` (untracked), `.gitignore` (tracked) | `.env`, `.claude/`, `CLAUDE.md` | `.gitignore` |
| empty repo | no matching files | nothing | - |
| only tracked dotfiles | `.gitignore`, `.eslintrc` (tracked) | nothing | `.gitignore`, `.eslintrc` |
| deeply nested | `.claude/mcp/servers.json` (untracked) | `.claude/mcp/servers.json` | - |
| symlinks | `.env` → `../.env.shared` (untracked symlink) | `.env` (as symlink) | - |
| file permissions | `.env` mode 0600 (untracked) | `.env` with mode 0600 | - |

### Table-driven tests for `createWorktree` integration

| Test Case | Setup | Expected Outcome |
|-----------|-------|------------------|
| basic worktree | untracked `.env`, `CLAUDE.md` | worktree created, files copied |
| branch exists | branch `foo` exists | uses `foo-<suffix>` |
| path exists | worktree path exists | uses path with suffix |
| no untracked files | only tracked files | worktree created, no copy needed |
| copy failure graceful | dest is read-only | worktree created, warning logged |

### Final Verification

- `go test ./... -v` passes
- `make test` passes
- `make build golden-update` shows no changes
- Code coverage for new functions > 80%

---

## Implementation Notes

### Exclusion list rationale
- `.git` - worktree has its own git structure managed by git
- `.swe-swe` - contains worktrees themselves, recordings; would cause duplication/recursion

### Graceful degradation
If copying fails, the worktree is still usable (just without the convenience files). Log a warning but don't fail the worktree creation.

### File patterns to copy
- `.*` - all dotfiles/dotdirs (except exclusions)
- `CLAUDE.md` - Claude Code instructions
- `AGENTS.md` - multi-agent instructions
