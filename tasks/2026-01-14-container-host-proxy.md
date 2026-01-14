# Container-to-Host Command Proxy

## Overview

Implement a file-based command proxy that allows containers to execute commands on the host system and receive stdout/stderr/exit code results.

**Design doc**: `research/2026-01-14-container-host-proxy.design.md`

## Design Decisions

| Aspect | Decision |
|--------|----------|
| Arg format | NUL-delimited (transparent to user) |
| Env vars | Not supported (use CLI args) |
| File organization | Flat (`<uuid>.req`, `<uuid>.exit`, etc.) |
| Host file watching | Go + fsnotify |
| Container file watching | bash + inotifywait |
| Container dependency | `inotify-tools` package required |

## File Structure

```
.swe-swe/proxy/
├── <command>          # Generated container script
├── <command>.pid      # Host PID file
├── <uuid>.req         # Request: NUL-delimited args
├── <uuid>.stdout      # Response: captured stdout
├── <uuid>.stderr      # Response: captured stderr
└── <uuid>.exit        # Response: exit code (signals completion)
```

## Cleanup Responsibility

| File | Created by | Deleted by |
|------|-----------|------------|
| `<uuid>.req` | Container | Host (claims it) |
| `<uuid>.stdout` | Host | Container (after reading) |
| `<uuid>.stderr` | Host | Container (after reading) |
| `<uuid>.exit` | Host | Container (after reading) |

---

## Phase 1: Core Host Proxy

### Goal
A working `swe-swe proxy <command>` subcommand that watches for requests and executes commands.

### Steps

1. **Add `proxy` subcommand skeleton** ✅
   - Add `proxy.go` in `cmd/swe-swe/`
   - Register subcommand in main command dispatch
   - Parse `<command>` argument, validate it's provided

2. **Implement PID file management** ✅
   - Check for existing `<command>.pid` file
   - If exists and process alive → fail fast with error message
   - If exists but process dead → remove stale file
   - Write own PID to `<command>.pid`

3. **Set up fsnotify watcher** ✅
   - Create `.swe-swe/proxy/` directory if needed
   - Initialize fsnotify watcher on the directory
   - Filter for `*.req` file creation events

4. **Implement request processing** ✅
   - Read NUL-delimited args from `<uuid>.req`
   - Delete `.req` file (claim the request)
   - Execute command with `exec.Command()`
   - Capture stdout and stderr separately
   - Write `<uuid>.stdout`, `<uuid>.stderr`
   - Write `<uuid>.exit` last (signals completion)

5. **Add graceful shutdown** ✅
   - Handle SIGINT/SIGTERM
   - Clean up PID file on exit

### Tests

1. **Test PID file creation**
   - Start proxy, verify PID file exists with correct content

2. **Test duplicate proxy detection**
   - Start proxy, attempt second instance, verify error

3. **Test stale PID cleanup**
   - Create PID file with dead PID, start proxy, verify it starts

4. **Test request processing**
   - Create mock `.req` file with NUL-delimited args
   - Verify command executed with correct args
   - Verify `.stdout`, `.stderr`, `.exit` files created

5. **Test exit code propagation**
   - Request command that exits non-zero
   - Verify `.exit` contains correct code

---

## Phase 2: Container Script Generation

### Goal
Generate a bash script that containers use to submit requests naturally.

### Steps

1. **Define script template**
   - Embed template in Go code (using `embed` or string literal)
   - Template uses placeholder for proxy directory path
   - Script location: `.swe-swe/proxy/<command>`

2. **Implement script generation on proxy startup**
   - Write script to `.swe-swe/proxy/<command>`
   - Set executable permissions (`chmod +x`)
   - Delete script on proxy shutdown (cleanup handler)

3. **Script implementation details**
   - UUID generation: `cat /proc/sys/kernel/random/uuid`
   - Atomic request: `printf '%s\0' "$@" > <uuid>.req.tmp && mv <uuid>.req.tmp <uuid>.req`
   - Wait: `inotifywait` with timeout wrapper
   - Output: `cat <uuid>.stdout`, `cat <uuid>.stderr >&2`
   - Cleanup: `rm -f` response files after reading
   - Exit: `exit $(cat <uuid>.exit)`

4. **Handle edge case: no arguments**
   - Empty request file (zero bytes) = run command with no args

### Tests

1. **Test script is generated on startup**
   - Start proxy for `make`
   - Verify `.swe-swe/proxy/make` exists and is executable

2. **Test script is deleted on shutdown**
   - Start proxy, then stop it (SIGTERM)
   - Verify `.swe-swe/proxy/make` is removed

3. **Test script content correctness**
   - Generate script, read its content
   - Verify it contains expected components

4. **Test end-to-end request/response**
   - Start proxy for `echo`
   - Execute generated script with args
   - Verify output is correct

5. **Test special characters in args**
   - Args with spaces, quotes, equals signs
   - Verify all passed correctly via NUL-delimited format

---

## Phase 3: Error Handling & Cleanup

### Goal
Robust handling of edge cases and failure scenarios.

### Steps

1. **Stale PID detection (host)**
   - On startup, check if PID in file is actually running
   - Use `os.FindProcess()` + signal 0 to check liveness
   - If stale, log warning and remove PID file

2. **Graceful shutdown (host)**
   - Register signal handlers for SIGINT, SIGTERM
   - On shutdown:
     - Stop accepting new requests
     - Wait for in-flight request to complete (with timeout)
     - Delete generated container script
     - Delete PID file
   - Use `context.Context` for cancellation

3. **Timeout handling (container script)**
   - Default timeout: 300 seconds (configurable via `PROXY_TIMEOUT`)
   - If `inotifywait` times out:
     - Print error to stderr
     - Clean up `.req` file if still exists
     - Exit with code 124

4. **Orphan file cleanup (host)**
   - On startup, scan for orphan response files
   - Remove orphans older than 5 minutes
   - Handles: container crash, host crash after writing response

5. **Clear error messages**
   - Proxy already running: `"proxy for 'make' already running (PID 12345)"`
   - Command not found: `"command 'foo' not found in PATH"`
   - Request timeout: `"timeout waiting for host to execute command"`

### Tests

1. **Test stale PID cleanup**
   - Create PID file with non-existent PID
   - Start proxy, verify it starts and replaces PID file

2. **Test graceful shutdown cleans up files**
   - Start proxy, send SIGTERM
   - Verify PID file and container script removed

3. **Test in-flight request completes on shutdown**
   - Start proxy for `sleep 1`, submit request
   - Immediately send SIGTERM
   - Verify request completes

4. **Test container timeout**
   - Generate container script but don't start proxy
   - Run script with `PROXY_TIMEOUT=1`
   - Verify exits with code 124

5. **Test orphan cleanup**
   - Create orphan `.stdout` and `.exit` files
   - Start proxy, verify orphans removed

---

## Phase 4: Integration Testing

### Goal
End-to-end tests verifying complete container/host flow.

### Steps

1. **Set up test harness**
   - Test helper to start/stop proxy in background
   - Temp directory for `.swe-swe/proxy/`
   - Use `echo`, `cat`, `sh -c` as test commands

2. **Basic flow test**
   - Start proxy for `echo`
   - Run container script with args
   - Verify stdout captured, exit code 0

3. **Exit code propagation test**
   - Proxy for `sh`, run `exit 42`
   - Verify exit code is 42

4. **Stdout/stderr separation test**
   - Run command that writes to both
   - Verify streams separated correctly

5. **Concurrent requests test**
   - Submit 5 requests in parallel
   - Verify all get correct responses

6. **Special characters test**
   - Args with spaces, quotes, newlines, unicode
   - Verify passed through correctly

7. **Real container test (optional)**
   - Boot test container
   - Run proxy on host
   - Execute script from inside container

### Test Organization

```
cmd/swe-swe/
├── proxy.go                    # Implementation
├── proxy_test.go               # Unit tests (Phases 1-3)
└── proxy_integration_test.go   # Integration tests (Phase 4)
```

Run integration tests: `go test -v ./cmd/swe-swe/ -run Integration`

---

## Dependencies

**Go (host):**
- `github.com/fsnotify/fsnotify` - file watching

**Container:**
- `inotify-tools` package - must be installed in container image

---

## Usage

**Host (start proxy):**
```bash
swe-swe proxy make
# [proxy] Listening for 'make' commands...
# [proxy] Container script: .swe-swe/proxy/make
```

**Container (run command):**
```bash
.swe-swe/proxy/make build TARGET=hello
# Executes: make build TARGET=hello
# Shows stdout/stderr, exits with make's exit code
```

**Multiple proxies (separate terminals):**
```bash
# Terminal 1: swe-swe proxy make
# Terminal 2: swe-swe proxy docker
# Container has both: .swe-swe/proxy/make, .swe-swe/proxy/docker
```
