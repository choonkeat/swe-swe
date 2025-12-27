# Task: Eliminate `.domain` and `.port` Files

**Date**: 2025-12-24
**Time**: 22:08:53
**Objective**: Remove persistent domain and port configuration files (`.swe-swe/.domain`, `.swe-swe/.port`) and simplify initialization and CLI output.

## Context

Previously we removed the `/vscode` handler from swe-swe-server, which was the only place that needed to dynamically know the domain. This task completes the simplification by eliminating the need to persist and read domain/port configuration.

## Scope

This task involves removing:
1. Creation of `.domain` file in `handleInit()`
2. Creation of `.port` file in `handleInit()`
3. `readDomain()` helper function
4. `readPort()` helper function
5. CLI flags `--domain` and `--port` from `swe-swe init` command
6. CLI output showing subdomain URLs (change to `0.0.0.0:{port}`)
7. Passing `SWE_DOMAIN` and `SWE_PORT` env vars to docker-compose
8. `.domain` and `.port` file references in docker-compose template

**NOT changing** (still needed):
- Docker-compose service port bindings
- Traefik routing rules
- Code-server and swe-swe-server container configurations

## Implementation Plan

### Step 1: Remove `--domain` and `--port` CLI Flags from Init Command
**Files**: `cmd/swe-swe/main.go`

**Changes**:
- Remove `--domain` and `--port` flags from `handleInit()`
- Remove port validation code
- Remove domain/port file writing code
- Update usage message to remove `[--domain DOMAIN]` mention

**Test**:
- Build: `make build` ✅
- Verify no compilation errors ✅
- No remaining references to domain/port files ✅

**Progress**: ✅ Completed
**Commit**: `8afdd99` - fix: remove domain and port cli flags from init command

---

### Step 2: Remove Helper Functions `readDomain()` and `readPort()`
**Files**: `cmd/swe-swe/main.go`

**Changes**:
- Remove `readDomain()` function
- Remove `readPort()` function
- Remove strconv import (no longer needed)

**Test**:
- Build: `make build` ✅
- Verify no compilation errors ✅
- No remaining references to these functions ✅

**Progress**: ✅ Completed
**Commit**: `5cbb732` - fix: remove readDomain/readPort functions and simplify cli output

---

### Step 3: Simplify CLI Output in `handleUp()`
**Files**: `cmd/swe-swe/main.go`

**Changes**:
- Remove calls to `readDomain()` and `readPort()` helper functions
- Replace subdomain URLs with simple `0.0.0.0:{port}` output
- Hardcode port as 9899

**Test**:
- Build: `make build` ✅
- Verify no compilation errors ✅
- Verified with Steps 2 integration ✅

**Progress**: ✅ Completed
**Commit**: `5cbb732` - fix: remove readDomain/readPort functions and simplify cli output

---

### Step 4: Remove Docker Environment Variables
**Files**: `cmd/swe-swe/main.go` and `cmd/swe-swe/templates/docker-compose.yml`

**Changes**:
- Remove `SWE_PORT` and `SWE_DOMAIN` env var appending from `handleUp()` in main.go
- Remove these env vars from swe-swe-server service in docker-compose.yml
- Keep port binding in Traefik service (still needed)

**Test**:
- Build: `make build` ✅
- Verify no compilation errors ✅
- No remaining references in code ✅

**Progress**: ✅ Completed
**Commit**: `a5f2a16` - fix: remove swe_domain and swe_port env vars from docker-compose

---

### Step 5: Verify Integration and No Regressions
**Test Plan**:
1. Full build: `make build` ✅
2. No remaining references to `.domain`, `.port` files ✅
3. No remaining references to `readDomain()`, `readPort()` functions ✅
4. No remaining `SWE_DOMAIN`, `SWE_PORT` env vars in docker-compose ✅
5. Git history shows 3 clean, focused commits ✅

**Progress**: ✅ Completed

**Result**: All domain/port file elimination complete. The system now:
- Uses hardcoded port (9899)
- Shows simple `0.0.0.0:{port}` in CLI startup output
- No longer creates or reads `.domain` and `.port` files
- No longer passes domain/port to server via environment variables
- Still allows subdomain-based access via Traefik routing (traefik.lvh.me, swe-swe.lvh.me, vscode.lvh.me)

---

## Commits Executed

**Commit 1** (Step 1):
`8afdd99` - fix: remove domain and port cli flags from init command

**Commit 2** (Steps 2 & 3):
`5cbb732` - fix: remove readDomain/readPort functions and simplify cli output

**Commit 3** (Step 4):
`a5f2a16` - fix: remove swe_domain and swe_port env vars from docker-compose

---

## Summary of Changes

| Component | File | Action | Lines |
|-----------|------|--------|-------|
| Init flags | cmd/swe-swe/main.go | Remove `--domain` flag | 68 |
| Init flags | cmd/swe-swe/main.go | Remove `--port` flag | 69 |
| Domain storage | cmd/swe-swe/main.go | Remove file write | 142-147 |
| Port storage | cmd/swe-swe/main.go | Remove file write | 150-154 |
| Helper function | cmd/swe-swe/main.go | Remove readDomain() | 493-507 |
| Helper function | cmd/swe-swe/main.go | Remove readPort() | 509-532 |
| CLI output | cmd/swe-swe/main.go | Change URL format | 258-260 |
| Docker env | cmd/swe-swe/main.go | Remove env append | 294-295 |
| Docker env | templates/docker-compose.yml | Remove env vars | 47-49 |

**Total Lines Removed/Changed**: ~80-100 lines
**Files Modified**: 2 (main.go, docker-compose.yml)

---

## Notes

- Users will no longer specify domain/port during `swe-swe init`
- CLI startup output will be simpler (just show `0.0.0.0:{port}`)
- Services will still be accessible via Traefik routing (traefik.lvh.me, swe-swe.lvh.me)
- The hardcoded port binding in Traefik will remain (7000 internal → 9899 external)
- Traefik routing rules remain unchanged and continue to work based on hostname patterns
