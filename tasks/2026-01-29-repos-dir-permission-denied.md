# Bug: External repo clone fails with "permission denied" when --repos-dir not specified

**Date:** 2026-01-29
**Status:** Fixed
**Severity:** High - Blocks external repository workflow
**Fixed in:** commit (pending)

## Summary

When `swe-swe init` is run WITHOUT the `--repos-dir` flag, creating a new session with an external git URL fails with:

```
Failed to create directory: mkdir /repos/github.com-choonkeat-tiny-form-fields: permission denied
```

## Root Cause

The `.swe-swe/repos` directory is NOT created on the host during `swe-swe init`. When Docker mounts this non-existent directory, it creates it with root ownership, causing permission denied errors for the container's `app` user.

### Technical Details

1. **Dockerfile** (`cmd/swe-swe/templates/host/Dockerfile:169`):
   ```dockerfile
   RUN mkdir -p /repos && chown {{UID}}:{{GID}} /repos
   ```
   Creates `/repos` inside the container with correct ownership (e.g., 1000:1000).

2. **docker-compose.yml** (`cmd/swe-swe/templates/host/docker-compose.yml:189`):
   ```yaml
   - {{REPOS_DIR}}:/repos
   ```
   When `--repos-dir` is NOT specified, defaults to:
   ```yaml
   - ${WORKSPACE_DIR:-.}/.swe-swe/repos:/repos
   ```

3. **init.go** - Missing directory creation:
   - Lines 547-552 create `binDir`, `homeDir`, `certsDir`
   - **BUT NOT** `.swe-swe/repos` directory

4. **What happens on `docker-compose up`:**
   - Host directory `.swe-swe/repos` doesn't exist
   - Docker auto-creates it as `root:root`
   - Volume mount overlays container's `/repos` with host directory
   - Container's `/repos` now has `root:root` ownership
   - `app` user (UID 1000) tries to `mkdir /repos/github.com-...` → **permission denied**

### Comparison: `--repos-dir` specified vs not specified

| Scenario | Host Path | Docker Behavior | Result |
|----------|-----------|-----------------|--------|
| `--repos-dir /data/repos` | `/data/repos` | User must pre-create | Works if user creates dir |
| No flag (default) | `.swe-swe/repos` | Docker auto-creates as root | **FAILS** - permission denied |

## Reproduction Steps

1. Run `swe-swe init` WITHOUT `--repos-dir` flag
2. Run `swe-swe up`
3. Open web UI, click "New Session"
4. Select "Clone external repository"
5. Enter any git URL (e.g., `https://github.com/choonkeat/tiny-form-fields.git`)
6. Click "Next", select agent, click "Start Session"
7. **Error:** `Failed to create directory: mkdir /repos/github.com-...: permission denied`

## Proposed Fix

Add `.swe-swe/repos` directory creation in `cmd/swe-swe/init.go` during initialization, similar to how other directories are created.

### Code Change Location

In `cmd/swe-swe/init.go`, around lines 547-552, add:

```go
homeDir := filepath.Join(sweDir, "home")
certsDir := filepath.Join(sweDir, "certs")
reposDirPath := filepath.Join(sweDir, "repos")  // ADD THIS
for _, dir := range []string{binDir, homeDir, certsDir, reposDirPath} {  // ADD reposDirPath
    if err := os.MkdirAll(dir, 0755); err != nil {
        log.Fatalf("Failed to create directory %q: %v", dir, err)
    }
}
```

**Note:** This should only create the default `.swe-swe/repos` directory when `--repos-dir` is NOT specified. If `--repos-dir` is specified, the user is responsible for ensuring that directory exists.

### Alternative Fix

Validate and create the repos directory in the template processing stage (`templates.go:192-198`), but host-side operations should ideally be in `init.go`.

## Related Issues

- Same issue may affect `.swe-swe/worktrees` directory (also mounted without pre-creation)
- See `tasks/2026-01-29-worktree-missing-file-copy.md` for related worktree issues

## Files Involved

| File | Lines | Role |
|------|-------|------|
| `cmd/swe-swe/init.go` | 340 | Flag definition |
| `cmd/swe-swe/init.go` | 547-552 | Directory creation (missing repos) |
| `cmd/swe-swe/templates.go` | 192-198 | Default value substitution |
| `cmd/swe-swe/templates/host/Dockerfile` | 169 | Container-side `/repos` creation |
| `cmd/swe-swe/templates/host/docker-compose.yml` | 189 | Volume mount |
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | 2846, 3084-3143 | `reposDir` usage and clone logic |
