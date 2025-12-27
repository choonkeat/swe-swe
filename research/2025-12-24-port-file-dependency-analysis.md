# Research: `.swe-swe/.port` File Dependency Analysis

**Date**: 2025-12-24
**Context**: Comprehensive analysis of `.port` file usage and dependencies (similar analysis to `.domain` file)

## Current Status

The `.port` file has **already been eliminated** from the active codebase. All references to `.port` file reading/writing and the `readPort()` helper function have been removed.

## Historical Usage

Previously, the `.port` file served similar purposes to `.domain`:

1. **Creation** - Stored during `swe-swe init` with default value 9899
2. **Reading** - Via `readPort()` helper function (now removed)
3. **Usage** - Passed as `SWE_PORT` environment variable to docker-compose
4. **Purpose** - Support flexible per-project port configuration

## Current Implementation

### Port Configuration (Hardcoded)

**File**: `/Users/choonkeatchew/git/choonkeat/swe-swe/cmd/swe-swe/main.go:202`

```go
// Default port for Traefik service
port := 9899
```

Port is now hardcoded to 9899 in the `handleUp()` function with no file-based customization.

### CLI Output

**File**: `/Users/choonkeatchew/git/choonkeat/swe-swe/cmd/swe-swe/main.go:232-233`

```go
fmt.Printf("Starting swe-swe environment at %s\n", absPath)
fmt.Printf("Access at: http://0.0.0.0:%d\n", port)
```

Shows simple `0.0.0.0:9899` instead of subdomain-based URLs.

### Docker Template

**File**: `/Users/choonkeatchew/git/choonkeat/swe-swe/cmd/swe-swe/templates/docker-compose.yml:12-13`

```yaml
ports:
  - "${SWE_PORT:-9899}:7000"
  - "${SWE_DASHBOARD_PORT:-9900}:8080"
```

Still contains environment variable placeholders for backwards compatibility, but:
- CLI does **not** pass `SWE_PORT` environment variable
- Docker-compose uses default value (9899) from placeholder
- The placeholders are passive and do not require env var to be set

## Removed Components

### 1. CLI Flag
**Previously**: `--port 9899` flag in `swe-swe init` command
**Now**: Removed entirely
**Status**: No longer accepts port customization from CLI

### 2. Helper Function
**Previously**: `readPort(sweDir string) int` function
**Location**: Was in `cmd/swe-swe/main.go` (lines 509-532)
**Status**: Removed in commit `5cbb732`

### 3. File Storage
**Previously**: `.swe-swe/.port` file created during init
**Status**: No longer created or read

### 4. Environment Variable Passing
**Previously**: Passed `SWE_PORT` to docker-compose in `handleUp()`
**Lines**: Was at `cmd/swe-swe/main.go:267`
**Status**: Removed in commit `a5f2a16`

### 5. Server-Side Port Functions
**Previously**: `readPortForVSCode()` function in swe-swe-server
**Status**: Removed in commit `8a2711c` (when `/vscode` handler was removed)

## Dependencies

Unlike the `.domain` file which had subdomain-dependent features, the `.port` file had simpler dependencies:

1. **Traefik Port Binding** - Needs to know external port for mapping
   - Currently: Fixed at 9899 (no flexibility)
   - Previously: Customizable via `.port` file

2. **CLI Output** - Display correct port to user
   - Currently: Hardcoded 9899
   - Previously: Read from `.port` file

3. **Server Redirect Logic** - For `/vscode` handler (now removed)
   - Previously: Used `readPortForVSCode()` to construct URLs
   - Now: Not applicable (handler removed)

## Evolution Timeline

### Phase 1: Custom Port Support (Completed)
- Added `--port` flag to init command
- Created `readPort()` helper function
- Templated docker-compose.yml
- Passed `SWE_PORT` to docker-compose
- Task: `tasks/2025-12-24-custom-port-support.md`

### Phase 2: VSCode Handler Removal (Completed)
- Removed `readPortForVSCode()` function
- Removed VSCode port dependency
- Task: `tasks/2025-12-24-eliminate-vscode-handler.md`

### Phase 3: Domain & Port File Elimination (Completed)
- Removed `--port` CLI flag
- Removed `readPort()` function
- Hardcoded port to 9899
- Stopped passing `SWE_PORT` env var
- Task: `tasks/2025-12-24-eliminate-domain-file.md`

## Why Eliminate Port Configuration?

### Simplification
- Reduces initialization complexity
- No need for per-project port files
- Fewer environment variables to manage

### Fixed Infrastructure
- Port 9899 is reliable and predictable
- Docker port mapping is simple
- No port conflicts to manage in typical usage

### Cost of Removal
- Lost flexibility for multiple concurrent instances
- Can't run multiple swe-swe projects on same machine with different ports
- Requires code changes if port needs to be customizable

## Backwards Compatibility

**Current behavior**:
- Existing projects with `.port` files will continue to work
- The files will simply be ignored
- Default port 9899 is always used

**Future re-implementation**:
- Port configuration could be re-added relatively easily
- Docker template already has placeholder structure
- Would need to decide: CLI flag, config file, environment variable, or combination

## Docker Compose Template Notes

The docker-compose template maintains environment variable placeholders:
- `${SWE_PORT:-9899}` - Traefik service port binding
- `${SWE_DASHBOARD_PORT:-9900}` - Traefik dashboard port

These are **not actively used** by the CLI but serve as:
1. **Documentation** of what port is being used
2. **Escape hatch** if users need to override via environment
3. **Future-proofing** for re-implementation

## Conclusion

The `.port` file was a **simple configuration mechanism** for supporting flexible port numbers. It has been **successfully eliminated** as part of the simplification effort, with port now hardcoded to 9899. The removal was straightforward because:

1. Port configuration is less critical than domain configuration
2. A fixed port is acceptable for single-instance usage
3. Docker template placeholders provide backwards compatibility
4. Future re-implementation is relatively easy if needed

This decision trades flexibility for simplicity, which appears to be the correct tradeoff for the current use case.
