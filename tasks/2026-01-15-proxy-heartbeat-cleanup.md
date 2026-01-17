# Proxy Heartbeat-Based Cleanup

## Goal

When the underlying program hangs and the container stops responding (dies, times out, or crashes), the host proxy should detect this via heartbeat staleness and kill the hanging process with proper signal escalation.

## Design Summary

- Container touches `.heartbeat` **before** sending request (prevents race)
- Container periodically touches `.heartbeat` while waiting for response
- Host tracks `activeRequests` count with atomic counter
- Host watches heartbeat staleness: if stale AND `activeRequests > 0` → kill all in-flight
- Signal escalation: SIGTERM → wait grace → SIGKILL
- Process group killing (not just direct child)
- Exit code format: `{code}` or `{code}:{signal}` when killed

## Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `PROXY_HEARTBEAT_STALE` | 5s | Heartbeat staleness threshold |
| `PROXY_KILL_GRACE` | 5s | Wait after SIGTERM before SIGKILL |
| `PROXY_SHUTDOWN_GRACE` | 30s | Wait for natural completion on shutdown |

---

## Phase 1: Host Infrastructure ✅ COMPLETE

### What will be achieved
- Track active request count with atomic counter
- Spawn child processes in their own process group
- Store process references so heartbeat watcher can kill them

### Steps

1. Add `activeRequests` atomic counter
   ```go
   var activeRequests atomic.Int32
   ```

2. Add process registry to track in-flight processes
   ```go
   type processEntry struct {
       cmd  *exec.Cmd
       pgid int
   }
   var inFlightProcesses sync.Map // uuid -> *processEntry
   ```

3. Set `Setpgid: true` before starting child process
   ```go
   cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
   ```

4. Update `processRequest()` to increment/decrement counter and register process
   ```go
   activeRequests.Add(1)
   defer activeRequests.Add(-1)
   inFlightProcesses.Store(uuid, &processEntry{cmd, pgid})
   defer inFlightProcesses.Delete(uuid)
   ```

### Tests

| Test | Description |
|------|-------------|
| `TestActiveRequestCounter` | Counter increments on start, decrements on finish |
| `TestProcessGroupSetup` | Child runs in own process group |
| `TestProcessRegistry` | Process registered during execution, removed after |

---

## Phase 2: Heartbeat Watcher ✅ COMPLETE

### What will be achieved
- Goroutine that periodically checks `.heartbeat` file freshness
- When stale AND `activeRequests > 0`, trigger process kills
- Configurable staleness threshold

### Steps

1. Add `heartbeatStale()` helper function
   ```go
   func heartbeatStale(path string, maxAge time.Duration) bool {
       info, err := os.Stat(path)
       if err != nil {
           return true  // Missing = stale
       }
       return time.Since(info.ModTime()) > maxAge
   }
   ```

2. Add heartbeat watcher goroutine in `runProxy()`
   ```go
   go func() {
       ticker := time.NewTicker(1 * time.Second)
       defer ticker.Stop()
       for {
           select {
           case <-ctx.Done():
               return
           case <-ticker.C:
               if activeRequests.Load() > 0 && heartbeatStale(heartbeatFile, staleThreshold) {
                   log.Printf("[proxy] Heartbeat stale, killing in-flight processes")
                   killAllInFlight()
               }
           }
       }
   }()
   ```

3. Read `PROXY_HEARTBEAT_STALE` from environment (default 5s)

4. Add logging when staleness detected
   ```go
   log.Printf("[proxy] Heartbeat stale (last: %v ago), killing %d in-flight processes",
       time.Since(info.ModTime()), activeRequests.Load())
   ```

### Tests

| Test | Description |
|------|-------------|
| `TestHeartbeatStale_Fresh` | Touch file, returns false |
| `TestHeartbeatStale_Stale` | Touch file, wait 6s, returns true |
| `TestHeartbeatStale_Missing` | No file, returns true |
| `TestHeartbeatWatcher_IgnoresWhenIdle` | No active + stale → no kill |
| `TestHeartbeatWatcher_KillsWhenActive` | Active + stale → triggers kill |

---

## Phase 3: Signal Escalation ✅ COMPLETE

### What will be achieved
- `killAllInFlight()` kills all registered process groups
- SIGTERM first, wait grace period, then SIGKILL if still alive
- Configurable grace period

### Steps

1. Add `killProcessGroup()` helper
   ```go
   func killProcessGroup(pgid int, grace time.Duration) error {
       syscall.Kill(-pgid, syscall.SIGTERM)

       done := make(chan struct{})
       go func() {
           for {
               if err := syscall.Kill(-pgid, 0); err != nil {
                   close(done)
                   return
               }
               time.Sleep(100 * time.Millisecond)
           }
       }()

       select {
       case <-done:
           return nil
       case <-time.After(grace):
           syscall.Kill(-pgid, syscall.SIGKILL)
           return fmt.Errorf("had to SIGKILL")
       }
   }
   ```

2. Add `killAllInFlight()` that iterates process registry
   ```go
   func killAllInFlight(grace time.Duration) {
       inFlightProcesses.Range(func(key, value any) bool {
           entry := value.(*processEntry)
           uuid := key.(string)
           log.Printf("[proxy] Killing process group %d for request %s", entry.pgid, uuid)
           killProcessGroup(entry.pgid, grace)
           return true
       })
   }
   ```

3. Read `PROXY_KILL_GRACE` from environment (default 5s)

4. Add detailed logging for each stage

### Tests

| Test | Description |
|------|-------------|
| `TestKillProcessGroup_CleanExit` | Handles SIGTERM, exits → no SIGKILL |
| `TestKillProcessGroup_ForceKill` | Ignores SIGTERM → SIGKILL after grace |
| `TestKillProcessGroup_KillsChildren` | Parent + child both killed via pgid |
| `TestKillAllInFlight_Multiple` | Multiple requests, all killed |

---

## Phase 4: Graceful Shutdown

### What will be achieved
- On SIGTERM/SIGINT, stop accepting new requests
- Wait up to 30s for natural completion
- Kill remaining with escalation, then exit

### Steps

1. Add shutdown deadline from `PROXY_SHUTDOWN_GRACE` (default 30s)

2. Replace simple `cancel()` with staged shutdown
   ```go
   go func() {
       sig := <-sigChan
       log.Printf("[proxy] Received %v, starting graceful shutdown...", sig)

       stopAccepting.Store(true)

       deadline := time.After(shutdownGrace)
       ticker := time.NewTicker(500 * time.Millisecond)
       defer ticker.Stop()

       for {
           select {
           case <-ticker.C:
               if activeRequests.Load() == 0 {
                   log.Printf("[proxy] All requests completed")
                   cancel()
                   return
               }
           case <-deadline:
               log.Printf("[proxy] Shutdown deadline exceeded, killing %d remaining",
                   activeRequests.Load())
               killAllInFlight(killGrace)
               cancel()
               return
           }
       }
   }()
   ```

3. Add `stopAccepting` flag to reject new requests during shutdown
   ```go
   if stopAccepting.Load() {
       log.Printf("[proxy] Rejecting request %s (shutting down)", uuid)
       writeExitFile(uuid, 125, "shutdown")
       continue
   }
   ```

4. Log progress during shutdown wait

### Tests

| Test | Description |
|------|-------------|
| `TestGracefulShutdown_WaitsForCompletion` | Request completes → clean exit |
| `TestGracefulShutdown_RejectsNewRequests` | New request during shutdown → rejected |
| `TestGracefulShutdown_KillsAfterDeadline` | Hanging request → killed after 30s |
| `TestGracefulShutdown_KillEscalation` | Ignores SIGTERM → SIGKILL after 5s |

---

## Phase 5: Container Script

### What will be achieved
- Touch `.heartbeat` before sending request
- Periodic touch while waiting
- Stop heartbeat on completion or timeout

### Steps

1. Add heartbeat file path
   ```bash
   heartbeat_file="$PROXY_DIR/.heartbeat"
   ```

2. Touch heartbeat BEFORE submitting request
   ```bash
   touch "$heartbeat_file"
   mv "$tmp_file" "$req_file"
   ```

3. Add background heartbeat loop
   ```bash
   (
       while [[ ! -f "$exit_file" ]]; do
           touch "$heartbeat_file"
           sleep 1
       done
   ) &
   heartbeat_pid=$!
   ```

4. Stop heartbeat when done
   ```bash
   kill "$heartbeat_pid" 2>/dev/null || true
   wait "$heartbeat_pid" 2>/dev/null || true
   ```

5. Ensure heartbeat stops on timeout too

### Tests

| Test | Description |
|------|-------------|
| `TestContainerHeartbeat_CreatedBeforeRequest` | `.heartbeat` mtime < `.req` mtime |
| `TestContainerHeartbeat_UpdatedPeriodically` | During command, heartbeat touched multiple times |
| `TestContainerHeartbeat_StopsOnCompletion` | After exit, heartbeat stops updating |
| `TestContainerHeartbeat_StopsOnTimeout` | Timeout → heartbeat stops → host detects |

---

## Phase 6: Exit Code Convention

### What will be achieved
- `.exit` contains `{code}` for normal exits
- `.exit` contains `{code}:{signal}` when killed
- Container parses new format

### Steps

1. Add `writeExitFile()` helper
   ```go
   func writeExitFile(exitFile string, code int, signal string) error {
       var content string
       if signal != "" {
           content = fmt.Sprintf("%d:%s", code, signal)
       } else {
           content = fmt.Sprintf("%d", code)
       }
       return os.WriteFile(exitFile, []byte(content), 0644)
   }
   ```

2. Update `processRequest()` to detect signal death
   ```go
   if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
       if status.Signaled() {
           signalName = status.Signal().String()
       }
   }
   ```

3. Update `killProcessGroup()` to mark killed processes

4. Update container script to parse format
   ```bash
   exit_content=$(cat "$exit_file")
   exit_code="${exit_content%%:*}"
   exit $exit_code
   ```

5. Special exit codes: `124:timeout`, `125:shutdown`

### Tests

| Test | Description |
|------|-------------|
| `TestExitFile_NormalExit` | Exits 0 → `0` |
| `TestExitFile_NonZeroExit` | Exits 1 → `1` |
| `TestExitFile_Signaled` | SIGKILL → `137:SIGKILL` |
| `TestExitFile_HostTimeout` | Stale kill → `124:timeout` |
| `TestContainerParsesExitCode` | Extracts code from `137:SIGKILL` |

---

## Phase 7: Integration Tests ✅ COMPLETE

### What will be achieved
- End-to-end tests for heartbeat-based cleanup
- All failure scenarios covered
- Short timeouts for fast feedback

### Steps

1. Add test helper for short timeouts
   ```go
   func setupFastTimeouts(t *testing.T) {
       t.Setenv("PROXY_HEARTBEAT_STALE", "2s")
       t.Setenv("PROXY_KILL_GRACE", "1s")
       t.Setenv("PROXY_SHUTDOWN_GRACE", "3s")
   }
   ```

2. Test: Container dies mid-request → host kills process

3. Test: Host shutdown with hanging process

4. Test: Normal operation with heartbeat overhead

5. Test: Multiple concurrent requests, one hangs

6. Test: Grandchild process killed via process group

### Tests

| Test | Verifies |
|------|----------|
| `TestIntegration_ContainerDies` | Heartbeat detection, kill trigger |
| `TestIntegration_ShutdownWithHang` | Graceful shutdown, escalation |
| `TestIntegration_NormalWithHeartbeat` | No regression, minimal overhead |
| `TestIntegration_PartialHang` | All processes killed on stale |
| `TestIntegration_GrandchildKilled` | Process group kill works |

---

## Regression Check

All existing tests in `proxy_test.go` and `proxy_integration_test.go` must pass after each phase.
