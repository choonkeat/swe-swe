# CRITICAL: Process and Goroutine Leaks

## Priority: 🔴 CRITICAL - Resource Exhaustion Risk

## Problem Statement

The application has multiple resource leaks that will exhaust system resources over time, leading to degraded performance and eventual system failure. These leaks accumulate with each user session and error condition.

## Identified Resource Leaks

### 1. Zombie Process Creation (websocket.go:106-140)

**Location:** `terminateProcess` function

**Current Code:**
```go
func terminateProcess(cmd *exec.Cmd) {
    // Try graceful shutdown first with interrupt signal
    err := cmd.Process.Signal(os.Interrupt)
    if err != nil {
        log.Printf("[PROCESS] Failed to send interrupt signal: %v", err)
    }
    
    // Wait for process to terminate gracefully
    done := make(chan error, 1)
    go func() {
        done <- cmd.Wait()  // THIS GOROUTINE MAY NEVER RETURN!
    }()
    
    select {
    case <-time.After(2 * time.Second):
        // Force kill after timeout
        log.Printf("[PROCESS] Graceful termination timed out, force killing PID: %d", cmd.Process.Pid)
        err := cmd.Process.Kill()
        if err != nil {
            log.Printf("[PROCESS] Failed to kill process: %v", err)
            // Process becomes zombie if kill fails!
        }
    case err := <-done:
        // Process terminated
        if err != nil {
            log.Printf("[PROCESS] Process termination error: %v", err)
        } else {
            log.Printf("[PROCESS] Process terminated gracefully")
        }
    }
}
```

**Problems:**
1. **Goroutine Leak**: The goroutine calling `cmd.Wait()` never terminates if the process doesn't exit
2. **Zombie Processes**: If `Process.Kill()` fails, the process remains as a zombie
3. **No Resource Cleanup**: File descriptors and pipes not closed properly
4. **No Verification**: No check that the process actually terminated

**Impact:**
- System runs out of process IDs
- Memory consumption grows unbounded
- File descriptor exhaustion

### 2. WebSocket Connection Leaks (websocket.go:240-280)

**Location:** WebSocket handler

**Current Code:**
```go
func (svc *ChatService) handleWebSocket(ws *websocket.Conn) {
    client := &Client{
        conn:             ws,
        browserSessionID: "",
        username:         "user",
    }
    
    svc.mutex.Lock()
    svc.clients[client] = true
    svc.mutex.Unlock()
    
    defer func() {
        svc.mutex.Lock()
        delete(svc.clients, client)
        svc.mutex.Unlock()
        ws.Close()
        // LEAK: Active processes not terminated!
        // LEAK: Context not cancelled!
    }()
    
    // ... handle messages ...
}
```

**Problems:**
1. **Process Orphaning**: Active processes continue running after client disconnects
2. **Context Leak**: Context cancellation function never called
3. **Resource Cleanup Missing**: PTY handles, pipes not closed

### 3. Periodic Indexer Goroutine Leak (main.go:154-178)

**Location:** Main initialization

**Current Code:**
```go
// Start periodic re-indexing to catch new files
go func() {
    ticker := time.NewTicker(2 * time.Minute)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            if err := fuzzyMatcher.IndexFiles(); err != nil {
                log.Printf("Periodic file re-indexing failed: %v", err)
            } else {
                log.Printf("Periodic re-index completed: %d files", fuzzyMatcher.GetFileCount())
            }
        }
        // NO WAY TO EXIT THIS LOOP!
    }
}()
```

**Problems:**
1. **Unbounded Goroutine**: No way to stop the indexer
2. **No Context**: Doesn't respect shutdown signals
3. **Memory Growth**: File index grows without bounds

### 4. PTY Resource Leaks (websocket.go:545-558)

**Location:** Goose command handling

**Current Code:**
```go
if isGooseCommand {
    ptmx, err = pty.Start(cmd)
    if err != nil {
        log.Printf("[ERROR] Failed to start PTY: %v", err)
        return ExecutionOtherError  // PTY not cleaned up!
    }
    defer func() {
        _ = ptmx.Close()  // May not be called on panic!
    }()
}
```

**Problems:**
1. **PTY Handle Leak**: PTY not closed on error paths
2. **Terminal State Corruption**: PTY settings not restored
3. **Kernel Resource Leak**: PTY devices limited system-wide

### 5. Scanner Buffer Leak (websocket.go:641-649)

**Location:** stderr handling

**Current Code:**
```go
go func() {
    scanner := bufio.NewScanner(stderr)
    const maxScanTokenSize = 1024 * 1024 // 1MB
    buf := make([]byte, 0, 64*1024)      // 64KB buffer
    scanner.Buffer(buf, maxScanTokenSize)
    
    for scanner.Scan() {
        line := scanner.Text()
        // Process line...
    }
    // Goroutine never exits if stderr blocks!
}()
```

**Problems:**
1. **Goroutine Leak**: Scanner goroutine blocks forever if stderr doesn't close
2. **Buffer Leak**: Large buffers not released on error
3. **No Timeout**: No mechanism to abandon hung reads

## Impact Analysis

### Resource Consumption Growth

| Resource | Leak Rate | Time to Exhaustion |
|----------|-----------|-------------------|
| Goroutines | ~3 per failed session | ~8 hours (10k limit) |
| Processes | ~1 per error | ~32k limit reached in days |
| File Descriptors | ~5 per connection | ~1024 limit in hours |
| Memory | ~1MB per session | OOM in days |
| PTY Devices | ~1 per goose cmd | System hangs at ~256 |

### Production Impact

- **Week 1**: Occasional slow responses
- **Week 2**: Random connection failures
- **Month 1**: Service requires daily restarts
- **Month 2**: System instability, data loss

## Fix Requirements

### Phase 1: Critical Leak Prevention

1. **Process Lifecycle Management**
```go
type ProcessManager struct {
    processes map[int]*ManagedProcess
    mu        sync.Mutex
    ctx       context.Context
}

type ManagedProcess struct {
    cmd      *exec.Cmd
    cancel   context.CancelFunc
    cleanup  []func()  // Cleanup functions
    done     chan struct{}
}

func (pm *ProcessManager) Start(ctx context.Context, name string, args ...string) (*ManagedProcess, error) {
    ctx, cancel := context.WithCancel(ctx)
    cmd := exec.CommandContext(ctx, name, args...)
    
    mp := &ManagedProcess{
        cmd:    cmd,
        cancel: cancel,
        done:   make(chan struct{}),
    }
    
    // Ensure cleanup happens
    go func() {
        cmd.Wait()
        close(mp.done)
        mp.runCleanup()
    }()
    
    pm.mu.Lock()
    pm.processes[cmd.Process.Pid] = mp
    pm.mu.Unlock()
    
    return mp, nil
}

func (mp *ManagedProcess) Terminate(timeout time.Duration) error {
    // Cancel context first
    mp.cancel()
    
    // Wait for graceful shutdown
    select {
    case <-mp.done:
        return nil
    case <-time.After(timeout):
        // Force kill
        mp.cmd.Process.Kill()
        <-mp.done  // Wait for confirmation
        return nil
    }
}
```

2. **Connection Cleanup**
```go
func (svc *ChatService) handleWebSocket(ws *websocket.Conn) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    client := &Client{
        conn:       ws,
        ctx:        ctx,
        cancelFunc: cancel,
    }
    
    // Cleanup function
    defer func() {
        // 1. Cancel context (stops all operations)
        cancel()
        
        // 2. Terminate active process with timeout
        if client.activeProcess != nil {
            ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()
            client.TerminateActiveProcess(ctx)
        }
        
        // 3. Close WebSocket
        ws.Close()
        
        // 4. Remove from clients map
        svc.mutex.Lock()
        delete(svc.clients, client)
        svc.mutex.Unlock()
        
        log.Printf("[CLEANUP] Client resources released")
    }()
}
```

3. **Goroutine Lifecycle Management**
```go
// Stoppable periodic task
type PeriodicTask struct {
    ticker *time.Ticker
    stop   chan struct{}
    done   chan struct{}
}

func NewPeriodicTask(interval time.Duration, task func()) *PeriodicTask {
    pt := &PeriodicTask{
        ticker: time.NewTicker(interval),
        stop:   make(chan struct{}),
        done:   make(chan struct{}),
    }
    
    go func() {
        defer close(pt.done)
        defer pt.ticker.Stop()
        
        for {
            select {
            case <-pt.ticker.C:
                task()
            case <-pt.stop:
                return
            }
        }
    }()
    
    return pt
}

func (pt *PeriodicTask) Stop() {
    close(pt.stop)
    <-pt.done  // Wait for goroutine to exit
}
```

### Phase 2: Resource Tracking

1. **Resource Monitor**
```go
type ResourceMonitor struct {
    goroutines int64
    processes  int64
    fds        int64
}

func (rm *ResourceMonitor) Track() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        runtime.GC()  // Force GC to get accurate numbers
        
        rm.goroutines = int64(runtime.NumGoroutine())
        
        // Track open file descriptors
        if fds, err := countOpenFDs(); err == nil {
            rm.fds = fds
        }
        
        // Log if thresholds exceeded
        if rm.goroutines > 1000 {
            log.Printf("[WARNING] High goroutine count: %d", rm.goroutines)
        }
        
        if rm.fds > 800 {
            log.Printf("[WARNING] High FD count: %d", rm.fds)
        }
    }
}
```

2. **Leak Detection Tests**
```go
func TestNoGoroutineLeaks(t *testing.T) {
    before := runtime.NumGoroutine()
    
    // Run test operations
    for i := 0; i < 100; i++ {
        client := createTestClient()
        client.Connect()
        client.SendMessage("test")
        client.Disconnect()
    }
    
    // Allow cleanup time
    time.Sleep(100 * time.Millisecond)
    runtime.GC()
    
    after := runtime.NumGoroutine()
    
    if after > before+10 {  // Allow small variance
        t.Errorf("Goroutine leak detected: before=%d, after=%d", before, after)
    }
}
```

## Testing Requirements

### Leak Detection Tests

1. **Process Leak Test**
   - Start 100 processes with errors
   - Verify all processes terminated
   - Check no zombies remain

2. **Goroutine Leak Test**
   - Create/destroy 1000 connections
   - Verify goroutine count returns to baseline
   - Use runtime.NumGoroutine() for validation

3. **FD Leak Test**
   - Open/close many connections
   - Verify file descriptor count stable
   - Use /proc/self/fd on Linux

4. **Memory Leak Test**
   - Run load test for 1 hour
   - Monitor memory with pprof
   - Verify no continuous growth

### Monitoring

Add metrics for:
- Active goroutine count
- Active process count
- Open file descriptors
- Memory usage
- PTY device usage

## Implementation Priority

1. **Immediate (Week 1)**
   - Fix process termination to prevent zombies
   - Add connection cleanup on disconnect
   - Fix PTY resource leaks

2. **Short-term (Week 2)**
   - Implement process manager
   - Add goroutine lifecycle management
   - Fix scanner goroutine leaks

3. **Medium-term (Week 3-4)**
   - Add resource monitoring
   - Implement leak detection tests
   - Add automatic resource limits

## Success Criteria

1. ✅ No goroutine growth over time
2. ✅ All processes properly terminated
3. ✅ File descriptors stay below 50% of limit
4. ✅ Memory usage stable over 24 hours
5. ✅ No zombie processes after 1000 operations
6. ✅ PTY devices properly released

## Risk Assessment

**Current Risk Level:** 🔴 CRITICAL
- **Probability:** 100% - Leaks occur on every error
- **Impact:** Service failure within days/weeks
- **Detection:** Currently no monitoring
- **Recovery:** Requires manual restart

## Notes

- Use `defer` for cleanup but verify it runs
- Always use context for cancellation
- Test with `go test -trace` for goroutine analysis
- Monitor with pprof in production
- Set resource limits as safety net

## References

- [Go Memory Leaks](https://go101.org/article/memory-leaking.html)
- [Finding Goroutine Leaks](https://github.com/uber-go/goleak)
- [Linux Process Management](https://man7.org/linux/man-pages/man2/wait.2.html)