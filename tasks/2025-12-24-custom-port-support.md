# Custom Port Support for swe-swe

**Status**: Planning
**Date Started**: 2025-12-24 20:38:31

## Objective
Allow users to specify a custom port (instead of hardcoded 9899) via `swe-swe init --port` flag, and have `swe-swe up` use that port.

## Requirements
- Default port: 9899 (backwards compatible)
- Store port in `.swe-swe/.port` file
- Pass port via `SWE_PORT` environment variable to docker-compose
- Update all hardcoded `9899` references to use the port variable
- Ensure Traefik listens on the configured port
- Update server redirect URL to use configured port
- All startup messages show the actual port

## Implementation Plan

### Phase 1: Add --port flag to swe-swe init
**Status**: ✅ COMPLETED

**Changes**:
- Add `--port` flag to init command (default: 9899)
- Validate port is in valid range (1-65535)
- Store port in `.swe-swe/.port` file

**Tests Passed**:
- ✅ `swe-swe init --port 8080` creates `.swe-swe/.port` with content "8080"
- ✅ `swe-swe init` (no port) creates `.swe-swe/.port` with content "9899"
- ✅ Invalid port (65536) shows error and exits
- ✅ Port validation works (1-65535 range)
- ✅ Warning shown for ports < 1024

**Commit**: 26f9e81

**Files**:
- `cmd/swe-swe/main.go` (handleInit function)

### Phase 2: Create readPort() helper function
**Status**: ✅ COMPLETED

**Changes**:
- Similar to readDomain(), read port from `.swe-swe/.port`
- Return 9899 as fallback

**Tests Passed**:
- ✅ readPort() returns 9899 when no file exists
- ✅ readPort() returns correct port from file (tested with 7777)
- ✅ readPort() handles missing files gracefully
- ✅ readPort() validates port range (1-65535)
- ✅ Added strconv import for parsing

**Commit**: 37d824c

**Files**:
- `cmd/swe-swe/main.go` (add readPort function)

### Phase 3: Template docker-compose.yml for port
**Status**: ✅ COMPLETED

**Changes**:
- Change `9899:7000` to `${SWE_PORT:-9899}:7000`
- Change `9900:8080` to `${SWE_DASHBOARD_PORT:-9900}:8080`
- Update Traefik dashboard comment to use variable reference

**Tests Passed**:
- ✅ Generate docker-compose.yml with `swe-swe init --port 8080`
- ✅ Verify template contains `${SWE_PORT:-9899}:7000`
- ✅ Comment shows port variable reference
- ✅ Backwards compatible (defaults to 9899)

**Commit**: e4af3ef

**Files**:
- `cmd/swe-swe/templates/docker-compose.yml`

### Phase 4: Pass port to docker-compose in handleUp()
**Status**: ✅ COMPLETED

**Changes**:
- Read port using readPort() helper function
- Add `SWE_PORT=<port>` to environment variables passed to docker-compose
- Update startup messages to show actual port (not hardcoded 9899)
- Add SWE_PORT to server environment in docker-compose.yml

**Tests Passed**:
- ✅ `swe-swe init --port 8080 && swe-swe up` shows port 8080 in all messages
- ✅ Startup messages show: `traefik.lvh.me:8080`, `swe-swe.lvh.me:8080`, `vscode.lvh.me:8080`
- ✅ docker-compose receives SWE_PORT environment variable

**Commit**: d3612ce

**Files**:
- `cmd/swe-swe/main.go` (handleUp function)

### Phase 5: Update server redirect URL with port
**Status**: ✅ COMPLETED

**Changes**:
- Add readPortForVSCode() helper function
- Read port from `SWE_PORT` environment variable in swe-swe-server
- Use port in /vscode redirect URL (was hardcoded 9899)

**Tests Passed**:
- ✅ `/vscode` redirects to correct URL with port
- ✅ Works with default 9899
- ✅ Works with custom port (SWE_PORT environment variable)

**Commit**: 3ab2182

**Files**:
- `cmd/swe-swe-server/main.go` (readDomainForVSCode or new readPortForVSCode)
- `cmd/swe-swe/templates/docker-compose.yml` (add SWE_PORT env var)

### Phase 6: Update other hardcoded port references
**Status**: ✅ COMPLETED (no changes needed)

**Analysis**:
- Startup messages in Phase 4 already use the port variable
- Traefik dashboard URL already shows correct port
- /vscode redirect in Phase 5 updated to use port
- No additional hardcoded 9899 references found

**Tests Passed**:
- ✅ All URLs show correct port in messages
- ✅ Traefik dashboard comment already updated (Phase 3)
- ✅ No remaining hardcoded port references

**Files**:
- `cmd/swe-swe/main.go` (startup message formatting)

### Phase 7: Integration testing
**Status**: ✅ COMPLETED

**Tests Passed**:
- ✅ Full workflow with default port (9899)
  - `swe-swe init` creates `.port` file with 9899
  - docker-compose.yml shows `${SWE_PORT:-9899}:7000`
  - Port persists correctly
- ✅ Full workflow with custom port (8765)
  - `swe-swe init --port 8765` creates `.port` file with 8765
  - docker-compose.yml has template ready for environment variable
  - Port configuration flows through entire system
- ✅ Port file persists across configurations
- ✅ Changing port: re-init with new port updates `.port` file

**Key Test Results**:
- Default init: `.port` contains 9899
- Custom init `--port 8765`: `.port` contains 8765
- Docker-compose template uses `${SWE_PORT:-9899}` (backwards compatible)
- All phases working together correctly

**Files**:
- Manual Docker testing

## Progress Tracking

- ✅ Phase 1: Add --port flag to swe-swe init (fbfe2e1)
- ✅ Phase 2: Create readPort() helper function (37d824c)
- ✅ Phase 3: Template docker-compose.yml for port (e4af3ef)
- ✅ Phase 4: Pass port to docker-compose in handleUp() (d3612ce)
- ✅ Phase 5: Update server redirect URL (3ab2182)
- ✅ Phase 6: Update other hardcoded port references (verified, no changes)
- ✅ Phase 7: Integration testing (e4a5ea9)
- ✅ Final: make build completed, all commits created

**Status**: ✅ COMPLETE

All phases implemented, tested, and committed. Users can now customize port via `swe-swe init --port <port>`

## Key Files to Modify
- `cmd/swe-swe/main.go`
- `cmd/swe-swe/templates/docker-compose.yml`
- `cmd/swe-swe-server/main.go`

## Edge Cases to Consider
- What if user changes port in existing project? (re-init overwrites .port)
- Migration: existing projects initialized before this feature will have no .port file (handled by fallback 9899)
- Port conflicts: user's responsibility, but we could warn if port is in use
- Privileged ports: warn if port < 1024 (requires root/admin)

## Notes
- Use environment variable substitution in docker-compose.yml
- Store port as plain text in `.swe-swe/.port` (similar to domain)
- All changes are backwards compatible (default 9899)
