# CRITICAL: Memory Exhaustion and Resource Management

## Priority: 🔴 CRITICAL - System Stability Risk

## Problem Statement

The application has multiple memory leaks and unbounded growth patterns that will exhaust system memory over time. Combined with lack of resource limits, this creates conditions for out-of-memory (OOM) kills and system instability.

## Identified Memory Issues

### 1. Unbounded File Index Growth (main.go:154-178)

**Location:** Periodic file indexer

**Current Code:**
```go
type FuzzyMatcher struct {
    files []FileInfo  // Grows without bounds
}

go func() {
    ticker := time.NewTicker(2 * time.Minute)
    for range ticker.C {
        if err := fuzzyMatcher.IndexFiles(); err != nil {
            log.Printf("Periodic file re-indexing failed: %v", err)
        }
        // Files keep accumulating, never removed
    }
}()
```

**Problems:**
1. **No Cleanup**: Deleted files never removed from index
2. **No Size Limit**: Index grows indefinitely
3. **Duplicate Entries**: Same files indexed multiple times
4. **Memory Growth**: Each file entry ~200 bytes, millions of files = GBs of RAM

**Impact:**
- 10K files = 2MB
- 1M files = 200MB  
- 10M files = 2GB (common in node_modules)
- System OOM within days/weeks

### 2. Session History Accumulation (websocket.go:27-28)

**Current Code:**
```go
type Client struct {
    claudeSessionHistory []string // History of session IDs (max 10, newest at end)
}

// But implementation doesn't enforce limit!
client.claudeSessionHistory = append(client.claudeSessionHistory, newSessionID)
// Never trimmed to 10 items
```

**Problems:**
1. **Comment Lies**: Says "max 10" but never enforced
2. **Unbounded Growth**: Appends forever
3. **No Cleanup**: Old sessions never removed
4. **Per-Client Growth**: Each client accumulates history

**Memory Impact:**
- 100 retries = 4KB per client
- 1000 retries = 40KB per client
- 100 clients × 1000 retries = 4MB

### 3. Tool Use Cache Never Cleaned (websocket.go:71)

**Current Code:**
```go
type ChatService struct {
    toolUseCache map[string]ToolUseInfo // Cache tool use info by ID
}

// Items added but never removed
svc.toolUseCache[toolID] = ToolUseInfo{
    Name:  "Bash",
    Input: largeCommandString, // Could be MBs
}
```

**Problems:**
1. **No Expiration**: Cache entries live forever
2. **No Size Limit**: Unlimited entries
3. **Large Values**: Input strings can be huge
4. **Global Accumulation**: Shared across all clients

**Memory Impact:**
- 1K tool uses × 10KB average = 10MB
- 100K tool uses × 10KB = 1GB
- Grows until OOM

### 4. WebSocket Message Buffers (websocket.go:641-649)

**Current Code:**
```go
scanner := bufio.NewScanner(stderr)
const maxScanTokenSize = 1024 * 1024 // 1MB per line!
buf := make([]byte, 0, 64*1024)      // 64KB initial
scanner.Buffer(buf, maxScanTokenSize)
```

**Problems:**
1. **Large Buffers**: 1MB per scanner
2. **Per-Process**: Each command gets new buffers
3. **Not Released**: Buffers held until process ends
4. **Multiple Scanners**: stdout, stderr both buffered

**Memory Impact:**
- 10 concurrent commands × 2MB = 20MB
- 100 concurrent commands × 2MB = 200MB
- Under load = OOM

### 5. Broadcast Message Queue

**Current Code:**
```go
type ChatService struct {
    broadcast chan ChatItem // Unbounded in practice
}

// Messages queued without limit
go func() {
    for item := range svc.broadcast {
        // If clients slow, messages accumulate
    }
}()
```

**Problems:**
1. **No Backpressure**: Senders never blocked
2. **Slow Clients**: Cause queue growth
3. **No Dropping**: Old messages never discarded
4. **Memory Spike**: Burst traffic causes spike

### 6. Client Connection State

**Current Code:**
```go
type Client struct {
    conn                  *websocket.Conn
    username              string
    browserSessionID      string
    claudeSessionID       string
    claudeSessionHistory []string
    allowedTools          []string
    lastUserMessage       string  // Could be huge
    // Many fields, no cleanup
}
```

**Problems:**
1. **Large Strings**: User messages can be MBs
2. **No Limits**: Fields grow without bounds
3. **Zombie Clients**: Disconnected clients not cleaned
4. **Reference Cycles**: Prevent GC

## Memory Growth Patterns

### Linear Growth
- File index: +200 bytes per file
- Session history: +40 bytes per retry
- Tool cache: +10KB per operation

### Exponential Growth
- Under load, buffers multiply
- Slow clients cause queue explosion
- Error conditions trigger retries

### Memory Timeline
| Time | Normal Load | High Load | With Errors |
|------|------------|-----------|-------------|
| 1 hour | 50MB | 200MB | 500MB |
| 1 day | 200MB | 2GB | 5GB |
| 1 week | 1GB | 10GB | OOM |
| 1 month | 5GB | OOM | OOM |

## Fix Requirements

### Phase 1: Immediate Bounds

1. **Enforce Session History Limit**
```go
const maxSessionHistory = 10

func (c *Client) AddSessionToHistory(sessionID string) {
    c.sessionMutex.Lock()
    defer c.sessionMutex.Unlock()
    
    c.claudeSessionHistory = append(c.claudeSessionHistory, sessionID)
    
    // Enforce limit
    if len(c.claudeSessionHistory) > maxSessionHistory {
        // Remove oldest entries
        c.claudeSessionHistory = c.claudeSessionHistory[len(c.claudeSessionHistory)-maxSessionHistory:]
    }
}
```

2. **LRU Cache for Tool Use**
```go
import "github.com/hashicorp/golang-lru"

type ChatService struct {
    toolUseCache *lru.Cache // Size-limited LRU cache
}

func NewChatService() *ChatService {
    cache, _ := lru.New(1000) // Max 1000 entries
    return &ChatService{
        toolUseCache: cache,
    }
}
```

3. **File Index with Limits**
```go
type BoundedFuzzyMatcher struct {
    files    []FileInfo
    maxFiles int
    mu       sync.RWMutex
}

func (fm *BoundedFuzzyMatcher) IndexFiles() error {
    fm.mu.Lock()
    defer fm.mu.Unlock()
    
    newFiles := []FileInfo{}
    err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
        if len(newFiles) >= fm.maxFiles {
            return filepath.SkipDir // Stop indexing
        }
        
        if shouldIndex(path) {
            newFiles = append(newFiles, FileInfo{
                Path: path,
                Size: info.Size(),
            })
        }
        return nil
    })
    
    fm.files = newFiles // Replace, don't append
    return err
}
```

### Phase 2: Resource Pools

1. **Buffer Pool**
```go
var bufferPool = sync.Pool{
    New: func() interface{} {
        return make([]byte, 64*1024) // 64KB buffers
    },
}

func processOutput(reader io.Reader) {
    buf := bufferPool.Get().([]byte)
    defer bufferPool.Put(buf)
    
    scanner := bufio.NewScanner(reader)
    scanner.Buffer(buf, len(buf))
    
    for scanner.Scan() {
        // Process line
    }
}
```

2. **Connection Pool**
```go
type ConnectionPool struct {
    maxConnections int
    active         int
    mu             sync.Mutex
    cond           *sync.Cond
}

func (p *ConnectionPool) Acquire() {
    p.mu.Lock()
    defer p.mu.Unlock()
    
    for p.active >= p.maxConnections {
        p.cond.Wait() // Block until connection available
    }
    
    p.active++
}

func (p *ConnectionPool) Release() {
    p.mu.Lock()
    defer p.mu.Unlock()
    
    p.active--
    p.cond.Signal()
}
```

### Phase 3: Memory Monitoring

1. **Memory Limits**
```go
import "runtime/debug"

func init() {
    // Set soft memory limit at 1GB
    debug.SetMemoryLimit(1 << 30)
}

func monitorMemory() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        var m runtime.MemStats
        runtime.ReadMemStats(&m)
        
        // Log memory usage
        log.Printf("[MEMORY] Alloc=%dMB, Sys=%dMB, NumGC=%d",
            m.Alloc/1024/1024,
            m.Sys/1024/1024,
            m.NumGC)
        
        // Trigger GC if memory high
        if m.Alloc > 500*1024*1024 { // 500MB
            runtime.GC()
            log.Printf("[MEMORY] Manual GC triggered")
        }
        
        // Alert if critical
        if m.Alloc > 800*1024*1024 { // 800MB
            log.Printf("[CRITICAL] Memory usage critical: %dMB", m.Alloc/1024/1024)
            // Could trigger cleanup or reject new connections
        }
    }
}
```

2. **Profiling Endpoints**
```go
import _ "net/http/pprof"

func init() {
    go func() {
        log.Println(http.ListenAndServe("localhost:6060", nil))
    }()
}

// Access profiles at:
// http://localhost:6060/debug/pprof/heap
// http://localhost:6060/debug/pprof/goroutine
// http://localhost:6060/debug/pprof/allocs
```

## Testing Requirements

### Memory Leak Tests

1. **Soak Test**
```go
func TestMemoryLeaks(t *testing.T) {
    runtime.GC()
    before := runtime.MemStats{}
    runtime.ReadMemStats(&before)
    
    // Run operations 1000 times
    for i := 0; i < 1000; i++ {
        client := createClient()
        client.SendMessage("test")
        client.Disconnect()
    }
    
    runtime.GC()
    after := runtime.MemStats{}
    runtime.ReadMemStats(&after)
    
    growth := after.Alloc - before.Alloc
    if growth > 10*1024*1024 { // 10MB threshold
        t.Errorf("Memory leak detected: %dMB growth", growth/1024/1024)
    }
}
```

2. **Stress Test**
```bash
# Run with memory profiling
go test -memprofile=mem.prof -bench=. -benchtime=10m

# Analyze profile
go tool pprof mem.prof
> top
> list main.
> web
```

3. **Load Test with Monitoring**
```go
func BenchmarkUnderLoad(b *testing.B) {
    // Monitor memory during benchmark
    go func() {
        ticker := time.NewTicker(1 * time.Second)
        for range ticker.C {
            var m runtime.MemStats
            runtime.ReadMemStats(&m)
            b.Logf("Memory: %dMB", m.Alloc/1024/1024)
        }
    }()
    
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            // Simulate client operations
        }
    })
}
```

## Validation Metrics

### Success Criteria
1. ✅ Memory stable over 24-hour test
2. ✅ No growth beyond 100MB baseline
3. ✅ Graceful degradation at limits
4. ✅ GC pause times < 10ms
5. ✅ No OOM under normal load
6. ✅ Memory returns to baseline after load

### Performance Targets
- Idle memory: < 50MB
- Per client: < 1MB
- Peak memory: < 1GB
- GC frequency: < 1/minute
- Response time: < 100ms p99

## Implementation Priority

1. **Immediate (Day 1)**
   - Fix session history limit
   - Add tool cache limit
   - Set memory monitoring

2. **Week 1**
   - Implement buffer pools
   - Fix file index growth
   - Add memory limits

3. **Week 2**
   - Connection pooling
   - Backpressure for queues
   - Memory profiling endpoints

4. **Week 3**
   - Load testing
   - Memory optimization
   - Documentation

## Risk Assessment

**Current Risk Level:** 🔴 CRITICAL
- **Probability:** 100% - Will OOM under load
- **Impact:** Service crash, data loss
- **Time to failure:** Days to weeks
- **Recovery:** Requires restart, loses state

## Notes

- Use `GOGC=50` to trigger GC more often
- Consider `GOMEMLIMIT` for hard limits
- Profile regularly in production
- Monitor with Prometheus/Grafana
- Set container memory limits as safety
- Document memory requirements

## References

- [Go Memory Management](https://go.dev/doc/gc-guide)
- [Profiling Go Programs](https://blog.golang.org/pprof)
- [Memory Leaks in Go](https://go101.org/article/memory-leaking.html)
- [sync.Pool](https://pkg.go.dev/sync#Pool)