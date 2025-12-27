# Task: Eliminate VSCode Handler and Footer Link

**Date**: 2025-12-24
**Time**: 22:04:08
**Objective**: Remove the `/vscode` redirect handler from swe-swe-server and remove the VSCode link from the terminal UI footer.

## Scope

This task involves removing:
1. `/vscode` HTTP handler in swe-swe-server (lines 593-601 in main.go)
2. `readDomainForVSCode()` function (lines 964-972)
3. `readPortForVSCode()` function (lines 974-982)
4. VSCode link from HTML template (line 26 in index.html)
5. VSCode output message from startup (lines 258-260 in main.go)

**Not changing yet** (part of separate .domain file elimination task):
- Domain/port file storage (.swe-swe/.domain, .swe-swe/.port)
- Environment variable passing through docker-compose
- Domain/port CLI flags during init

## Implementation Plan

### Step 1: Remove `/vscode` Handler from swe-swe-server
**Files**: `cmd/swe-swe-server/main.go`
**Lines**: 593-601, 964-972, 974-982

**Changes**:
- Remove the `/vscode` redirect handler block (lines 593-601)
- Remove `readDomainForVSCode()` function (lines 964-972)
- Remove `readPortForVSCode()` function (lines 974-982)

**Test**:
- Build the server binary: `make build`
- Verify no compilation errors
- Start swe-swe and verify `/vscode` path no longer redirects
- Verify other handlers still work (POST /message, WebSocket, etc.)

**Progress**: ✅ Completed
**Commit**: `8a2711c` - fix: remove /vscode handler from swe-swe-server

---

### Step 2: Remove VSCode Link from HTML Template
**Files**: `cmd/swe-swe-server/static/index.html`
**Lines**: 26

**Changes**:
- Remove `links="[VS Code](/vscode)"` attribute from `<terminal-ui>` element
- Change from: `links="[VS Code](/vscode)"`
- Change to: `links=""` (or remove the attribute entirely)

**Test**:
- Build the server binary: `make build`
- Verify no compilation errors
- Start swe-swe and open terminal UI
- Verify VSCode link no longer appears in the footer status bar

**Progress**: ✅ Completed
**Commit**: `9ac6dca` - fix: remove vscode link from terminal ui footer

---

### Step 3: Remove VSCode Startup Message
**Files**: `cmd/swe-swe/main.go`
**Lines**: 258-260

**Changes**:
- Remove or comment out the VSCode URL output line
- Keep the other two messages (Traefik dashboard, swe-swe)

**Test**:
- Build the CLI: `make build`
- Verify no compilation errors
- Run `swe-swe up` and verify startup output shows only 2 service URLs (traefik, swe-swe)
- Verify the URLs still point to correct subdomains

**Progress**: ✅ Completed
**Commit**: `436c725` - fix: remove vscode service url from startup output

---

### Step 4: Verify Integration and No Regressions
**Test Plan**:
1. Full build: `make build` - should succeed ✅
2. Code review - no remaining references to removed functions ✅
3. Verify git history - three clean commits for each change ✅
4. All builds completed without errors ✅

**Progress**: ✅ Completed
**Result**: All three steps executed successfully. No compilation errors. Code changes are minimal and focused.

---

## Commits Plan

After each successful step with passing tests:

**Commit 1** (after Step 1):
```
fix: remove /vscode handler from swe-swe-server
```

**Commit 2** (after Step 2):
```
fix: remove vscode link from terminal ui footer
```

**Commit 3** (after Step 3):
```
fix: remove vscode service url from startup output
```

---

## Summary of Changes

| File | Change | Lines |
|------|--------|-------|
| cmd/swe-swe-server/main.go | Remove `/vscode` handler | 593-601 |
| cmd/swe-swe-server/main.go | Remove readDomainForVSCode() | 964-972 |
| cmd/swe-swe-server/main.go | Remove readPortForVSCode() | 974-982 |
| cmd/swe-swe-server/static/index.html | Remove links attribute | 26 |
| cmd/swe-swe/main.go | Remove VSCode output | 258-260 |

**Total Lines Removed**: ~45 lines
**Files Modified**: 3

---

## Notes

- The VSCode service will still be accessible via `vscode.lvh.me:9899` through Traefik routing (docker-compose still configures it)
- Users just won't get a convenient link in the terminal UI or the `/vscode` path redirect
- This is a preparatory step before removing the `.domain` file entirely in a future task
