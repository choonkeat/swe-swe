# Add panic recovery to unprotected goroutines in swe-swe-server

## Background

The swe-swe container has been crashing intermittently (containerd shim disconnects at ~01:32 and ~06:19 UTC on March 2). Investigation found:

- Only the swe-swe container crashes; all other services (traefik, auth, chrome, etc.) run fine
- No OOM kills, no external signals — the process dies from within
- The Go binary has 7 goroutines, but only 1 has `recover()` (at line 5344, inside `registerOrchestrationTools`)
- An unrecovered panic in any goroutine crashes the entire process
- Go's `net/http` catches panics in HTTP handlers, but NOT in standalone goroutines

## Goal

Add `defer func() { if r := recover(); ... }()` to each unprotected goroutine so that:
1. A panic is **logged with full stack trace** (for diagnosis)
2. The server keeps running (other sessions unaffected)
3. The affected session may break, but it won't take down the whole server

## Source file

`/workspace/cmd/swe-swe/templates/host/swe-swe-server/main.go` (6233 lines)

## Changes needed

### Helper function (add near top of file, after imports)

```go
// recoverGoroutine logs panics from goroutines without crashing the server.
// Usage: defer recoverGoroutine("description")
func recoverGoroutine(where string) {
	if r := recover(); r != nil {
		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		log.Printf("PANIC recovered in %s: %v\n%s", where, r, buf[:n])
	}
}
```

Note: `runtime` is already imported.

### Goroutine 1: PTY reader (line ~1078)

In `func (s *Session) startPTYReader()`, immediately inside the `go func()`:

```go
go func() {
	defer recoverGoroutine(fmt.Sprintf("PTY reader for session %s", s.UUID))
	buf := make([]byte, 4096)
	// ... rest of function
}()
```

### Goroutine 2: WebSocket relay backend→client (line ~1483)

In `func handleWebSocketRelay(...)`, the first relay goroutine:

```go
go func() {
	defer recoverGoroutine("WebSocket relay backend→client")
	for {
		mt, msg, err := backendConn.ReadMessage()
		// ...
	}
}()
```

### Goroutine 3: WebSocket relay client→backend (line ~1497)

Same function, the second relay goroutine:

```go
go func() {
	defer recoverGoroutine("WebSocket relay client→backend")
	for {
		mt, msg, err := clientConn.ReadMessage()
		// ...
	}
}()
```

### Goroutine 4: Preview proxy server (line ~3776)

In the session creation code, the preview proxy goroutine:

```go
go func() {
	defer recoverGoroutine(fmt.Sprintf("preview proxy for session %s", sessionUUID))
	ln, err := net.Listen("tcp", previewSrv.Addr)
	// ...
}()
```

### Goroutine 5: Agent chat proxy server (line ~3794)

Same area, the agent chat proxy goroutine:

```go
go func() {
	defer recoverGoroutine(fmt.Sprintf("agent chat proxy for session %s", sessionUUID))
	ln, err := net.Listen("tcp", acSrv.Addr)
	// ...
}()
```

### Goroutine 6: Process wait (line ~5075)

In `func (s *Session) Close()`:

```go
go func() {
	defer recoverGoroutine(fmt.Sprintf("process wait for session %s", s.UUID))
	s.Cmd.Wait()
	close(done)
}()
```

### Goroutine 7: Shutdown handler (line ~2068)

Low risk but for completeness:

```go
go func() {
	defer recoverGoroutine("shutdown handler")
	<-serverCtx.Done()
	log.Println("Shutting down server...")
	// ...
}()
```

## Testing

After making changes:
1. `go build` should succeed with no errors
2. `go vet ./...` should pass
3. Manual test: start a session, verify normal operation
4. The PANIC log line will appear in container logs if/when a crash occurs, giving us the exact stack trace to fix the root cause

## Notes

- The existing `recover()` at line 5344 (inside `registerOrchestrationTools`) can stay as-is
- The `recoverGoroutine` helper uses `runtime.Stack(buf, false)` — the `false` means only the current goroutine's stack (not all goroutines), keeping the log manageable
- This is a **diagnostic measure** — it prevents crashes but doesn't fix the underlying bug. The logged panic will tell us what to fix next.
