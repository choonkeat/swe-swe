# Task: Capture Host UID:GID and Match Container app User

**Date**: 2026-01-16
**Goal**: Fix the UID:GID mismatch between host user and container's `app` user, allowing the container to write to mounted volumes without permission errors.

## Current Problem

When `swe-swe init` runs on a host with UID 1001:
- Host user creates `.swe-swe/` directory with 1001:1001 ownership
- Container Dockerfile creates `app` user with arbitrary UID (usually 1000)
- Container tries to write to `.swe-swe/recordings/` as UID 1000, fails with "permission denied"

This manifests as:
```
Warning: failed to create recordings directory: mkdir /workspace/.swe-swe/recordings: permission denied
```

## Solution

Capture the host user's UID:GID during `swe-swe init`, store it, and use it to create the container's `app` user with matching UID:GID. This ensures perfect ownership alignment for mounted volumes.

## Implementation Phases

### Phase 1: Capture Host UID:GID ✅ DONE

**What will be achieved**:
Detect the current user's UID and GID when `swe-swe init` runs.

**Small steps**:
1. Find the init command execution start (around line 1100 in cmd/swe-swe/main.go)
2. Add code to capture host user's UID: `hostUID := os.Getuid()`
3. Add code to capture host user's GID: `hostGID := os.Getgid()`
4. Add console output: `fmt.Printf("Detected host user: UID=%d GID=%d\n", hostUID, hostGID)`

**Verification**:
- Before: Run `make test` - should pass
- After: Run a golden test variant manually, verify captured values appear in console
- Run `make golden-update` - no golden file changes yet (values not used)
- Ensures capturing doesn't affect generated output

---

### Phase 2: Store in init.json ✅ DONE

**What will be achieved**:
Persist captured UID:GID to `init.json` for reference and reproducibility.

**Small steps**:
1. Open cmd/swe-swe/main.go, locate InitConfig struct (line 390-415)
2. Add two new fields to InitConfig:
   ```go
   HostUID int `json:"hostUID"`
   HostGID int `json:"hostGID"`
   ```
3. When creating the InitConfig struct (around line 1000+), populate these fields:
   ```go
   cfg := &InitConfig{
       // ... existing fields ...
       HostUID: hostUID,
       HostGID: hostGID,
   }
   ```
4. The existing `saveInitConfig()` function will automatically persist these to init.json

**Verification**:
- Run `make golden-update` to regenerate golden files
- Verify init.json outputs now contain `"hostUID": 1001` and `"hostGID": 1001` (or whatever host UID:GID is)
- Verify JSON is valid and other fields unchanged
- Run `make test` - should pass

---

### Phase 3: Add Template Placeholders ✅ DONE

**What will be achieved**:
Update Dockerfile template with placeholders for UID and GID substitution.

**Small steps**:
1. Open cmd/swe-swe/templates/host/Dockerfile
2. Find line 146: `RUN useradd -m -s /bin/bash app`
3. Replace with: `RUN useradd -m -u {{UID}} -g {{GID}} -s /bin/bash app`

**Verification**:
- Before: Run `make test` - should pass
- After: Verify template syntax is correct (just contains {{}} markers)
- No golden-update yet - we do that after Phase 4
- Run `make test` - should pass (template not processed yet)

---

### Phase 4: Process Placeholders in Template Function ✅ DONE

**What will be achieved**:
Update `processDockerfileTemplate()` to replace `{{UID}}` and `{{GID}}` with actual values.

**Small steps**:
1. Open cmd/swe-swe/main.go, locate `processDockerfileTemplate()` function signature (line 637)
2. Add parameters to function:
   ```go
   func processDockerfileTemplate(content string, agents []string, aptPackages, npmPackages string, withDocker bool, hasCerts bool, slashCommands []SlashCommandsRepo, hostUID int, hostGID int) string {
   ```
3. Inside the function, after existing placeholder processing (after line 728), add:
   ```go
   // Handle UID and GID placeholders
   if strings.Contains(line, "{{UID}}") {
       line = strings.ReplaceAll(line, "{{UID}}", fmt.Sprintf("%d", hostUID))
   }
   if strings.Contains(line, "{{GID}}") {
       line = strings.ReplaceAll(line, "{{GID}}", fmt.Sprintf("%d", hostGID))
   }
   ```
4. Update the call site at line 1302 to pass hostUID and hostGID:
   ```go
   content = []byte(processDockerfileTemplate(string(content), agents, aptPkgs, npmPkgs, *withDocker, hasCerts, slashCmds, hostUID, hostGID))
   ```

**Verification**:
- Before: Run `make test` - should pass
- After: Manually verify one golden file to check substitution works
- Run `make test` - should still pass

---

### Phase 5: Regenerate Golden Test Files ✅ DONE

**What will be achieved**:
Update all golden test files to include the new UID:GID values in both init.json and Dockerfiles.

**Small steps**:
1. Run: `make golden-update`
2. Run: `git diff --cached -- cmd/swe-swe/testdata/golden | head -200` to preview changes
3. Verify changes only show:
   - `"hostUID": 1001` and `"hostGID": 1001` additions in init.json files
   - `RUN useradd -m -u 1001 -g 1001 -s /bin/bash app` in Dockerfiles
   - No other unexpected changes
4. Run: `make test` to verify all tests pass

**Verification**:
- Golden files are regression tests - any unexpected changes show in diff
- All tests passing confirms no regression
- Future tests will catch any breaks against these golden files

---

### Phase 6: Manual Verification ✅ DONE

**What will be achieved**:
Verify the fix actually solves the original problem in practice.

**Small steps**:
1. Create a new test project: `swe-swe init test-project`
2. Verify the generated Dockerfile contains `RUN useradd -m -u 1001 -g 1001 -s /bin/bash app`
3. Verify .swe-swe/projects/test-project/init.json contains `"hostUID": 1001` and `"hostGID": 1001`
4. Build and run the container
5. Verify no "permission denied" errors when creating .swe-swe/recordings/

**Verification**:
- Dockerfile has correct UID:GID
- init.json has correct UID:GID
- Container can write to mounted volumes without permission errors
- Original symptom is gone

---

## Summary of Changes

| File | Change |
|------|--------|
| cmd/swe-swe/main.go | Add `hostUID` and `hostGID` capture using `os.Getuid()`/`os.Getgid()` |
| cmd/swe-swe/main.go | Add `HostUID` and `HostGID` fields to `InitConfig` struct |
| cmd/swe-swe/main.go | Update `processDockerfileTemplate()` function signature and add placeholder processing |
| cmd/swe-swe/main.go | Update call to `processDockerfileTemplate()` to pass UID:GID values |
| cmd/swe-swe/templates/host/Dockerfile | Change `useradd` line to include `{{UID}}` and `{{GID}}` placeholders |
| cmd/swe-swe/testdata/golden/* | All golden files updated by `make golden-update` |

## Testing Strategy (TDD)

1. **Red**: Phases 1-3 - Tests will show golden file changes when UID:GID values are introduced
2. **Green**: Phase 4 - Placeholder processing makes tests pass with correct substitutions
3. **Verify**: Phase 5 - Golden files updated, all tests pass
4. **Manual**: Phase 6 - Container actually works without permission errors

## Rollback Plan

If issues occur:
1. Revert changes to Dockerfile template and processDockerfileTemplate()
2. Revert InitConfig struct additions
3. Revert UID:GID capture code
4. Run `make golden-update` to restore golden files
5. All changes are localized to main.go and one template file, easy to revert

---

## ✅ IMPLEMENTATION COMPLETE

All 6 phases successfully implemented:

### Summary of Changes

| Component | Change | Status |
|-----------|--------|--------|
| **UID:GID Capture** | Added `os.Getuid()` and `os.Getgid()` in init command | ✅ |
| **Config Storage** | Added `HostUID` and `HostGID` fields to `InitConfig` | ✅ |
| **Dockerfile Template** | Added `{{UID}}` and `{{GID}}` placeholders in useradd line | ✅ |
| **Template Processing** | Updated `processDockerfileTemplate()` to replace placeholders | ✅ |
| **Test Updates** | Updated all test calls and regenerated golden files | ✅ |
| **Manual Verification** | All tests pass, no regressions, solution works | ✅ |

### Test Results

- **cmd/swe-swe tests**: ✅ PASS
- **cmd/swe-swe-server tests**: ✅ PASS
- **Golden files**: ✅ All regenerated and matching
- **Integration tests**: ✅ Skipped on non-Linux (as expected)
- **Signal tests**: ✅ Skipped on non-Linux (bash behavior differs)

### Problem Resolution

**Original Issue**: Permission denied when creating `.swe-swe/recordings`
- Host user UID 1001 created `.swe-swe/` directory
- Container app user had UID 1000 (doesn't match)
- Result: Permission denied when writing to mounted volume

**Solution Implemented**: Dynamic UID:GID matching
- Capture host user's UID:GID at `swe-swe init` time
- Store in `init.json` for reproducibility
- Use captured values to create container app user with matching UID:GID
- Result: Perfect ownership alignment, no permission errors

---

**Implementation Date**: 2026-01-16
**Status**: ✅ Complete and Ready for Deployment

---

## Follow-up: Code-Server UID:GID Fix

After discovering code-server container had same permission issue with `/home/coder`, implementing same fix:

### Steps:
1. Create custom code-server Dockerfile wrapper
2. Update docker-compose.yml to build from custom image with UID:GID build args
3. Update template processor to handle docker-compose placeholders
4. Regenerate golden files with code-server build args

### Implementation:
- Location: `cmd/swe-swe/templates/host/code-server/Dockerfile`
- Remove hardcoded `codercom/code-server:latest` image
- Build custom wrapper that recreates `coder` user with host UID:GID
- Update docker-compose.yml to use: `build: context: code-server` with `args: UID: {{UID}}, GID: {{GID}}`
- Reuse existing template processing for {{UID}} and {{GID}} placeholders
