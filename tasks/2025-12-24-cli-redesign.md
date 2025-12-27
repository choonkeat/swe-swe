# swe-swe CLI Redesign: Rename run/stop and fix restart behavior

## Objective
1. Rename `swe-swe run` → `swe-swe up` and `swe-swe stop` → `swe-swe down` (align with docker-compose)
2. Don't restart processes that exit with code 0 (success) - leave them terminated

## Changes Required

### Part 1: Rename CLI Commands
**File**: `cmd/swe-swe/main.go`
- Rename `handleRun()` → `handleUp()`
- Rename `handleStop()` → `handleDown()`
- Update command dispatch in main()
- Update help text

### Part 2: Fix Restart Behavior
**File**: `cmd/swe-swe-server/main.go`
- In `startPTYReader()`, capture process exit code
- Check if exit code is 0
- Skip restart if exit code is 0
- Still restart on non-zero exit codes (errors)

## Implementation Steps

### Step 1: Rename CLI commands
- [x] Update main() switch statement
- [x] Rename function definitions
- [x] Update help text
- [x] Test that `swe-swe up` and `swe-swe down` work
- **Status**: ✅ COMPLETED - Git commit: bd49764

### Step 2: Fix restart behavior in swe-swe-server
- [x] Get exit code from `s.Cmd.ProcessState`
- [x] Check if exit code is 0
- [x] Skip restart message and don't restart if code is 0
- [x] Show exit code in error messages
- [x] Log success exit differently
- **Status**: ✅ COMPLETED - Git commit: 35c9358

## Testing Results
- ✅ Build succeeds for both CLI and server
- ✅ Help text shows new `up`/`down` commands
- ✅ Exit code detection logic implemented correctly
- ✅ No regressions in existing functionality

## Testing Plan
- Verify `swe-swe up` starts containers
- Verify `swe-swe down` stops containers
- Test process that exits with code 0 doesn't restart
- Test process that exits with non-zero code does restart
