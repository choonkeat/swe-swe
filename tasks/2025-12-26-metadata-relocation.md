# Metadata Relocation: Move .swe-swe from Project to $HOME/.swe-swe/projects/

## Objective
Move project metadata from `{path}/.swe-swe` (editable by rogue containers) to `$HOME/.swe-swe/projects/{sanitized-path}-{md5}/` (secure, outside container reach).

## Design Summary
- **Sanitization**: Replace special characters with `-`, append MD5 hash of absolute path
- **Security**: Metadata unreachable from container; only project code is mounted
- **UX**: Add `swe-swe list` command with auto-prune for missing paths
- **Errors**: `swe-swe up` on uninitialized path shows error mentioning `swe-swe list`
- **No migration**: Existing `.swe-swe/` directories are left as-is

---

## Implementation Steps

### Step 1: Create utility functions for path sanitization and metadata directory management
**Goal**: Build reusable functions for computing metadata paths
**Files to modify**: `cmd/swe-swe/main.go`

**Tasks**:
- Add `import "crypto/md5"` and `import "fmt"`
- Implement `sanitizePath(absPath string) string` function that:
  - Takes absolute path (e.g., `/Users/alice/projects/myapp`)
  - Replaces non-alphanumeric chars (except `/`) with `-`
  - Computes MD5 hash of full absolute path
  - Returns `{sanitized}-{md5-first-8-chars}` format
- Implement `getMetadataDir(absPath string) (string, error)` function that:
  - Computes metadata dir path: `$HOME/.swe-swe/projects/{sanitized-path}`
  - Returns full path or error if $HOME can't be determined

**Tests to write** (`cmd/swe-swe/main_test.go`):
- Test `sanitizePath` returns consistent hash for same path
- Test `sanitizePath` returns different hash for different paths
- Test `sanitizePath` correctly replaces special chars
- Test `getMetadataDir` returns valid path under $HOME
- Test `getMetadataDir` returns error if $HOME not set

**Status**: PENDING

---

### Step 2: Update `handleInit` to use new metadata location
**Goal**: Write project metadata to `$HOME/.swe-swe/projects/` instead of `.swe-swe/`

**Tasks**:
- Modify `handleInit()` to:
  - Compute metadata dir using `getMetadataDir(absPath)`
  - Create metadata dir structure: `bin/`, `home/`, `certs/`
  - Extract templates and certificates to metadata dir (not to project)
  - Keep binary extraction in metadata dir
  - Add message: `"View all projects: swe-swe list"`
- Verify `.swe-swe/` is NOT created in project directory
- Existing behavior is preserved (init creates all necessary files)

**Tests to write**:
- Test `handleInit` creates metadata dir at correct location
- Test metadata dir contains `bin/`, `home/`, `certs/`
- Test `docker-compose.yml` exists in metadata dir
- Test `.swe-swe/` is NOT created in project directory
- Test init message includes "View all projects: swe-swe list"

**Status**: PENDING

---

### Step 3: Update `handleUp` to use new metadata location and add list command hint
**Goal**: Load from new metadata location and provide clear error for uninitialized paths

**Tasks**:
- Modify `handleUp()` to:
  - Compute metadata dir using `getMetadataDir(absPath)`
  - Check metadata dir exists (not `.swe-swe/` in project)
  - If not found, error: `"Project not initialized at {path}. Run: swe-swe init --path {path}. View projects: swe-swe list"`
  - Use metadata dir for `docker-compose.yml`, binary extraction, etc.
- Remove old check for `.swe-swe/` in project
- Keep docker-compose execution logic unchanged

**Tests to write**:
- Test `handleUp` fails with clear error if metadata dir doesn't exist
- Test error message mentions `swe-swe list`
- Test `handleUp` succeeds when metadata dir exists
- Test docker-compose.yml is read from correct metadata location

**Status**: PENDING

---

### Step 4: Implement `handleList` command
**Goal**: Show all initialized projects and auto-prune missing ones

**Tasks**:
- Implement `handleList()` function that:
  - Scans `$HOME/.swe-swe/projects/` directory
  - For each entry, extract original path from metadata (need to store this!)
  - Check if original path still exists on disk
  - Remove metadata dir if original path doesn't exist (auto-prune)
  - Display remaining projects with: `{original-path} → metadata at {metadata-dir}`
  - Show count of pruned projects if any
- Modify `main()` switch to handle "list" command
- Update `printUsage()` to include `list` command

**Important**: Need mechanism to recover original path from metadata dir
- **Option A**: Store original path in metadata as file (e.g., `.path` file)
- **Option B**: Store in a registry file at `$HOME/.swe-swe/projects.json`
- **Recommendation**: Use simple `.path` file in each metadata dir (per CLAUDE.md: minimal dependencies)

**Additional task for this step**:
- Modify `handleInit()` to write original absolute path to `{metadata_dir}/.path`

**Tests to write**:
- Test `handleList` finds all initialized projects
- Test `handleList` auto-prunes metadata dirs for missing paths
- Test `handleList` displays remaining projects correctly
- Test `handleList` shows count of pruned projects
- Test `.path` file is created during init
- Test `.path` file is read correctly during list

**Status**: PENDING

---

### Step 5: Update `handleDown` to use new metadata location
**Goal**: Load from new metadata location with consistent error handling

**Tasks**:
- Modify `handleDown()` to:
  - Compute metadata dir using `getMetadataDir(absPath)`
  - Check metadata dir exists (not `.swe-swe/` in project)
  - If not found, error: `"Project not initialized at {path}. Run: swe-swe init --path {path}. View projects: swe-swe list"`
  - Use metadata dir for docker-compose operations

**Tests to write**:
- Test `handleDown` fails with clear error if metadata dir doesn't exist
- Test error message mentions `swe-swe list`
- Test `handleDown` succeeds when metadata dir exists

**Status**: PENDING

---

### Step 6: Update `handleBuild` to use new metadata location
**Goal**: Load from new metadata location with consistent error handling

**Tasks**:
- Modify `handleBuild()` to:
  - Compute metadata dir using `getMetadataDir(absPath)`
  - Check metadata dir exists (not `.swe-swe/` in project)
  - If not found, error: `"Project not initialized at {path}. Run: swe-swe init --path {path}. View projects: swe-swe list"`
  - Use metadata dir for docker-compose operations

**Tests to write**:
- Test `handleBuild` fails with clear error if metadata dir doesn't exist
- Test error message mentions `swe-swe list`
- Test `handleBuild` succeeds when metadata dir exists

**Status**: PENDING

---

### Step 7: Update `handleUpdate` to use new metadata location
**Goal**: Load from new metadata location with consistent error handling

**Tasks**:
- Modify `handleUpdate()` to:
  - Compute metadata dir using `getMetadataDir(absPath)`
  - Check metadata dir exists (not `.swe-swe/` in project)
  - If not found, error: `"Project not initialized at {path}. Run: swe-swe init --path {path}. View projects: swe-swe list"`
  - Use metadata dir for binary operations

**Tests to write**:
- Test `handleUpdate` fails with clear error if metadata dir doesn't exist
- Test error message mentions `swe-swe list`
- Test `handleUpdate` succeeds when metadata dir exists

**Status**: PENDING

---

## Testing Strategy

### Unit Tests
- Path sanitization logic
- Metadata directory computation
- `.path` file I/O

### Integration Tests
- Full `swe-swe init` → verify metadata created in correct location
- Full `swe-swe up` → verify container starts with metadata from new location
- Full `swe-swe list` → verify pruning and display work

### Regression Tests
- Verify existing `up`, `down`, `build`, `update` commands still work with new paths
- Verify no changes to container interface or docker-compose behavior
- Verify error messages are clear and actionable

---

## Progress Tracking

| Step | Status | Notes |
|------|--------|-------|
| 1    | COMPLETED | Utility functions - sanitizePath() and getMetadataDir() implemented with 7 unit tests |
| 2    | COMPLETED | handleInit updated - uses metadata dir, writes .path file, shows list hint |
| 3    | COMPLETED | handleUp updated - uses new metadata location with improved error message |
| 4    | COMPLETED | handleList implemented - scans projects, auto-prunes missing paths, displays active projects |
| 5    | COMPLETED | handleDown updated - uses metadata dir, error message includes list hint |
| 6    | COMPLETED | handleBuild updated - uses metadata dir, error message includes list hint |
| 7    | COMPLETED | handleUpdate updated - uses metadata dir, error message includes list hint |

---

## Deferred / Out of Scope
- Migration of existing `.swe-swe/` directories (not needed)
- UI for managing projects (use CLI list for now)
- Config file for customizing metadata location (not needed)
