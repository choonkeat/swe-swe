# Security Assessment: swe-swe Codebase

**Date:** 2026-01-29
**Auditor:** Claude Code (Opus 4.5)
**Scope:** Authentication, authorization, network security, input validation, credential handling, concurrency, resource management, error handling

---

## Executive Summary

This security assessment was conducted in response to concerns raised by the "ClawdBot Security Crisis" article (vertu.com). The audit examined whether swe-swe exhibits similar vulnerabilities to those described: exposed gateways with zero authentication, shell access without protection, and lack of basic security controls.

**Key Finding:** swe-swe's architecture is fundamentally sound with proper authentication via Traefik ForwardAuth middleware. However, **4 critical issues**, **5 high-severity issues**, and **15 medium-severity issues** were identified that require attention.

**Overall Risk Assessment:**
- **Local development use:** MEDIUM risk
- **Exposed to untrusted networks:** HIGH risk (without addressing critical issues)

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                      HOST SYSTEM                             │
│  ┌────────────────────────────────────────────────────────┐ │
│  │              Docker Compose Network                     │ │
│  │              (swe-network, internal)                    │ │
│  │                                                         │ │
│  │  ┌──────────────┐                                       │ │
│  │  │   Traefik    │◄── Port 7000/7443/17443 (external)   │ │
│  │  │ (ForwardAuth)│◄── Docker socket (ro)                │ │
│  │  └──────┬───────┘                                       │ │
│  │         │                                               │ │
│  │  ┌──────┴──────────────────────────────────────┐       │ │
│  │  │                                              │       │ │
│  │  │  ┌─────────┐  ┌─────────┐  ┌─────────────┐ │       │ │
│  │  │  │  Auth   │  │ Chrome  │  │ code-server │ │       │ │
│  │  │  │ (4180)  │  │ (6080)  │  │   (8080)    │ │       │ │
│  │  │  └─────────┘  └─────────┘  └─────────────┘ │       │ │
│  │  │                                              │       │ │
│  │  │  ┌────────────────────────────────────────┐ │       │ │
│  │  │  │           swe-swe-server               │ │       │ │
│  │  │  │  - WebSocket terminal (9898)           │ │       │ │
│  │  │  │  - Preview proxy (9899)                │ │       │ │
│  │  │  │  - File uploads                        │ │       │ │
│  │  │  │  - Recording API                       │ │       │ │
│  │  │  └────────────────────────────────────────┘ │       │ │
│  │  └──────────────────────────────────────────────┘       │ │
│  └────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

**Authentication Flow:**
1. All HTTP requests go through Traefik reverse proxy
2. Traefik invokes ForwardAuth middleware → Auth service at `/swe-swe-auth/verify`
3. Auth service validates HMAC-SHA256 signed session cookie
4. Invalid/missing cookie → redirect to `/swe-swe-auth/login`
5. Valid cookie → request forwarded to backend service

---

## Issues by Severity

### CRITICAL (4 issues)

#### C1: Default Password "changeme"

**Location:** `cmd/swe-swe/docker.go:114-116`, `cmd/swe-swe/templates/host/docker-compose.yml:82`

**Description:**
When `SWE_SWE_PASSWORD` environment variable is not set, the system defaults to password `"changeme"`. This is a well-known weak password that attackers routinely try.

**Code:**
```go
// docker.go:114-116
if os.Getenv("SWE_SWE_PASSWORD") == "" {
    env = append(env, "SWE_SWE_PASSWORD=changeme")
}
```

```yaml
# docker-compose.yml:82
- SWE_SWE_PASSWORD=${SWE_SWE_PASSWORD:-changeme}
```

**Risk:**
Any exposed instance (accidental port forwarding, misconfigured firewall, cloud security group) is immediately compromised with full terminal access.

**Recommendation:**
```go
// docker.go - require explicit password
if os.Getenv("SWE_SWE_PASSWORD") == "" {
    fmt.Fprintf(os.Stderr, "Error: SWE_SWE_PASSWORD environment variable is required\n")
    fmt.Fprintf(os.Stderr, "Generate one with: export SWE_SWE_PASSWORD=$(openssl rand -base64 32)\n")
    os.Exit(1)
}
```

---

#### C2: WebSocket CheckOrigin Allows All Origins

**Location:** `cmd/swe-swe/templates/host/swe-swe-server/main.go:53-59`

**Description:**
The WebSocket upgrader accepts connections from any origin, enabling cross-origin WebSocket hijacking attacks.

**Code:**
```go
var upgrader = websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
    CheckOrigin: func(r *http.Request) bool {
        return true // Allow all origins for development
    },
}
```

**Risk:**
A malicious website can open a WebSocket connection to a user's swe-swe instance if:
- User is authenticated (has valid session cookie)
- Attacker knows or guesses the URL
- Combined with any auth bypass, grants full terminal access

**Recommendation:**
```go
CheckOrigin: func(r *http.Request) bool {
    origin := r.Header.Get("Origin")
    if origin == "" {
        return true // Allow non-browser clients
    }
    // Parse and validate origin matches request host
    originURL, err := url.Parse(origin)
    if err != nil {
        return false
    }
    return originURL.Host == r.Host
},
```

---

#### C3: API Keys in Environment Variables

**Location:** `cmd/swe-swe/templates/host/docker-compose.yml:211-224`

**Description:**
Sensitive API keys (ANTHROPIC_API_KEY, etc.) are passed to containers via environment variables, which are visible through `docker inspect`, process listings, and container logs.

**Code:**
```yaml
environment:
  - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
  # - GEMINI_API_KEY=${GEMINI_API_KEY}
  # - OPENAI_API_KEY=${OPENAI_API_KEY}
```

**Risk:**
- Anyone with Docker API access can read all API keys
- Container compromise exposes all credentials
- Keys may appear in debug logs or crash dumps
- No credential rotation mechanism

**Recommendation:**
- Use Docker secrets for credential injection
- Implement credential provider pattern (fetch from vault at runtime)
- Document credential rotation procedures
- Consider short-lived tokens where possible

---

#### C4: Docker Socket with Read-Write Access

**Location:** `cmd/swe-swe/templates/host/docker-compose.yml:200`

**Description:**
When `--with-docker` flag is used, the Docker socket is mounted with read-write access, enabling container escape.

**Code:**
```yaml
# Only included when --with-docker flag is used
- /var/run/docker.sock:/var/run/docker.sock
```

**Risk:**
If the swe-swe container is compromised, attacker can:
1. Create privileged containers
2. Mount host filesystem
3. Escape to host system completely
4. Access other containers' data and credentials

**Recommendation:**
- Add explicit warning when `--with-docker` is used
- Require confirmation for this flag
- Document that this should only be used in isolated development VMs
- Consider rootless Docker or restricted socket proxy

---

### HIGH (5 issues)

#### H1: Signal Handler Memory Leak (proxy.go)

**Location:** `cmd/swe-swe/proxy.go:307-346`

**Description:**
Signal handlers are registered with `signal.Notify()` but never unregistered with `signal.Stop()`. On repeated proxy invocations in the same process, handlers accumulate.

**Code:**
```go
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

go func() {
    sig := <-sigChan
    // ... handle signal ...
}()

// MISSING: defer signal.Stop(sigChan)
```

**Risk:**
- Memory leak from accumulated goroutines
- Multiple handlers fire on single signal
- Non-deterministic shutdown behavior

**Recommendation:**
```go
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
defer signal.Stop(sigChan)  // ADD THIS LINE
```

---

#### H2: Signal Handler Memory Leak (docker.go)

**Location:** `cmd/swe-swe/docker.go:142-150`

**Description:**
Identical issue to H1 in different file.

**Code:**
```go
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

go func() {
    sig := <-sigChan
    if cmd.Process != nil {
        cmd.Process.Signal(sig)
    }
}()

// MISSING: defer signal.Stop(sigChan)
```

**Recommendation:**
Add `defer signal.Stop(sigChan)` after the `signal.Notify()` call.

---

#### H3: os.Chown Errors Silently Ignored

**Location:** `cmd/swe-swe/init.go:577-582, 623-628`

**Description:**
During initialization, `os.Chown()` calls in `filepath.Walk()` silently ignore errors, leading to permission issues that manifest later.

**Code:**
```go
filepath.Walk(homeDir, func(path string, info os.FileInfo, err error) error {
    if err == nil {
        os.Chown(path, 1000, 1000)  // ERROR IGNORED
    }
    return nil  // Walk errors also ignored
})
```

**Risk:**
- Files owned by wrong user cause runtime permission errors
- Code-server user (UID 1000) may not access home directory
- Silent failures make debugging extremely difficult

**Recommendation:**
```go
filepath.Walk(homeDir, func(path string, info os.FileInfo, err error) error {
    if err != nil {
        log.Printf("Warning: cannot access %s: %v", path, err)
        return nil // Continue walking
    }
    if err := os.Chown(path, 1000, 1000); err != nil {
        log.Printf("Warning: cannot chown %s: %v", path, err)
    }
    return nil
})
```

---

#### H4: PTY File Descriptor Leak

**Location:** `cmd/swe-swe/templates/host/swe-swe-server/main.go` (Session struct)

**Description:**
The Session struct holds a PTY file descriptor (`PTY *os.File`) but there's no clear cleanup function that ensures it's closed when sessions terminate.

**Risk:**
- File descriptor exhaustion under high session churn
- "too many open files" errors in production
- Memory leak from associated buffers

**Recommendation:**
- Implement explicit `Session.Close()` method
- Ensure PTY is closed in all termination paths
- Add defer in session creation
- Consider adding FD tracking/monitoring

---

#### H5: Traefik Docker Socket Access

**Location:** `cmd/swe-swe/templates/host/docker-compose.yml:60`

**Description:**
Traefik mounts the Docker socket read-only for service discovery, exposing container metadata.

**Code:**
```yaml
- "/var/run/docker.sock:/var/run/docker.sock:ro"
```

**Risk:**
- Traefik can enumerate all containers and images
- Metadata exposure (environment variables visible)
- If Traefik is compromised, provides reconnaissance capability

**Recommendation:**
- Document this requirement and risk
- Consider using explicit service registration instead of discovery
- Evaluate Traefik socket proxy for restricted access

---

### MEDIUM (15 issues)

#### M1: .env File World-Readable Permissions

**Location:** `cmd/swe-swe/certs.go:156`

**Description:**
The `.env` file is created with 0644 permissions (world-readable).

**Code:**
```go
if err := os.WriteFile(envFilePath, []byte(envFileContent), 0644); err != nil {
```

**Recommendation:**
```go
if err := os.WriteFile(envFilePath, []byte(envFileContent), 0600); err != nil {
```

---

#### M2: No File Upload Size Limits

**Location:** `cmd/swe-swe/templates/host/swe-swe-server/main.go:3895-3943`

**Description:**
File uploads have no size validation, enabling disk exhaustion attacks.

**Recommendation:**
- Add configurable maximum file size (e.g., 100MB default)
- Reject oversized uploads with clear error message
- Add rate limiting per session

---

#### M3: os.Remove Errors Ignored in Cleanup

**Location:** `cmd/swe-swe/proxy.go:112, 115-117`

**Description:**
In `rejectRequest()`, file removal errors are ignored, potentially leaving orphan files.

**Code:**
```go
func rejectRequest(uuid string) {
    os.Remove(reqFile)  // Error ignored
    os.WriteFile(stdoutFile, []byte{}, 0644)  // Error ignored
    os.WriteFile(stderrFile, []byte{...}, 0644)  // Error ignored
}
```

**Recommendation:**
```go
func rejectRequest(uuid string) {
    if err := os.Remove(reqFile); err != nil && !os.IsNotExist(err) {
        log.Printf("Warning: failed to remove request file %s: %v", reqFile, err)
    }
    // Similar for other operations
}
```

---

#### M4: os.WriteFile Errors Ignored in Error Path

**Location:** `cmd/swe-swe/proxy.go:530-532`

**Description:**
When request processing fails, response file creation errors are ignored, potentially leaving containers waiting.

**Recommendation:**
Check and log WriteFile errors in error handling paths.

---

#### M5: TOCTOU Race in filepath.Walk

**Location:** `cmd/swe-swe/init.go:577-628`

**Description:**
Two separate `filepath.Walk()` operations on the same directory create a time-of-check-time-of-use race condition.

**Risk:**
Files created/deleted between walks may cause inconsistent state.

**Recommendation:**
Accept the race (initialization is single-user) or use atomic directory operations.

---

#### M6: Watcher Close Error Ignored

**Location:** `cmd/swe-swe/proxy.go:398`

**Description:**
`defer watcher.Close()` ignores the return error.

**Recommendation:**
```go
defer func() {
    if err := watcher.Close(); err != nil {
        log.Printf("Warning: failed to close watcher: %v", err)
    }
}()
```

---

#### M7: Goroutine Leak in AddClient/RemoveClient

**Location:** `cmd/swe-swe/templates/host/swe-swe-server/main.go:343, 373`

**Description:**
`BroadcastStatus()` is called in a new goroutine without tracking, potentially accumulating under rapid connect/disconnect.

**Code:**
```go
go s.BroadcastStatus()  // No tracking
```

**Recommendation:**
- Use sync.WaitGroup to track goroutines
- Or use buffered channel with select to prevent blocking

---

#### M8: Channel Close Race in debug-listen

**Location:** `cmd/swe-swe/templates/host/swe-swe-server/main.go:1816-1849`

**Description:**
Connection may be closed by goroutine while main thread calls `WriteMessage()`.

**Recommendation:**
Protect all connection operations with SafeConn mutex, or use dedicated writer goroutine.

---

#### M9: Windows Signal Handling

**Location:** `cmd/swe-swe/docker.go:134-155`

**Description:**
Code uses `syscall.SIGTERM` which doesn't exist on Windows. `Process.Signal()` may fail silently.

**Recommendation:**
```go
// Use build tags or runtime check
if runtime.GOOS == "windows" {
    signal.Notify(sigChan, os.Interrupt)
} else {
    signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
}
```

---

#### M10: No CSRF Token on Login Form

**Location:** `cmd/swe-swe/templates/host/auth/main.go:242-276`

**Description:**
Login form doesn't include CSRF protection token.

**Risk:**
Low practical risk (attacker would need to know password), but violates defense-in-depth.

**Recommendation:**
Add CSRF token generation and validation.

---

#### M11: HMAC Secret from Environment Variable

**Location:** `cmd/swe-swe/templates/host/auth/main.go:279`

**Description:**
Session signing secret is passed via environment variable.

**Recommendation:**
Document secret rotation procedure and consider alternative secret storage.

---

#### M12: Long Session Expiration (7 days)

**Location:** `cmd/swe-swe/templates/host/auth/main.go:265`

**Description:**
Session cookies are valid for 7 days, extending attack window if stolen.

**Recommendation:**
- Reduce to 24-48 hours for active sessions
- Implement idle timeout
- Add session revocation capability

---

#### M13: Self-Signed Cert Marked as CA

**Location:** `cmd/swe-swe/certs.go:61`

**Description:**
Self-signed certificate has `IsCA: true` flag for iOS Safari compatibility.

**Risk:**
If private key is compromised, attacker can sign arbitrary certificates.

**Recommendation:**
Document this limitation clearly; key should be protected accordingly.

---

#### M14: os.Getwd Error Ignored

**Location:** `cmd/swe-swe/templates/host/swe-swe-server/main.go:533`

**Description:**
`os.Getwd()` error is ignored with `_`.

**Code:**
```go
workDir, _ = os.Getwd()  // Error ignored
```

**Recommendation:**
```go
if wd, err := os.Getwd(); err == nil {
    workDir = wd
} else {
    workDir = "/unknown"
    log.Printf("Warning: cannot get working directory: %v", err)
}
```

---

#### M15: Potential Nil Pointer on Metadata Access

**Location:** `cmd/swe-swe/templates/host/swe-swe-server/main.go:432-439`

**Description:**
`Metadata` field is checked for nil in some places but may not be in all code paths.

**Recommendation:**
Audit all `Metadata` access paths; consider initializing non-nil in constructor.

---

### LOW (4 issues)

#### L1: No File MIME Type Validation

**Location:** `cmd/swe-swe/templates/host/swe-swe-server/main.go:3924`

**Description:**
Uploaded files are not validated for MIME type.

**Recommendation:**
Add whitelist if specific file types should be restricted.

---

#### L2: os.Stat Error Loses Diagnostic Info

**Location:** `cmd/swe-swe/proxy.go:372`

**Description:**
`os.Stat()` error is discarded; both "file missing" and "permission denied" show same message.

**Recommendation:**
Log actual error for debugging purposes.

---

#### L3: Inconsistent os.Remove Error Handling

**Location:** `cmd/swe-swe/proxy.go:350, 351, 570, 676, 690`

**Description:**
Multiple bare `os.Remove()` calls with inconsistent error handling patterns.

**Recommendation:**
Create helper function:
```go
func safeRemove(path string) {
    if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
        log.Printf("Warning: failed to remove %s: %v", path, err)
    }
}
```

---

#### L4: Unencrypted Inter-Container Traffic

**Location:** Docker compose network configuration

**Description:**
Traffic between containers on Docker bridge network is unencrypted.

**Risk:**
Only exploitable if Docker host is compromised; low practical risk.

**Recommendation:**
Document network isolation assumptions in security documentation.

---

## Comparison: swe-swe vs "ClawdBot" Article Claims

| Claim from Article | swe-swe Status | Notes |
|--------------------|----------------|-------|
| "923 gateways exposed with zero authentication" | **NOT VULNERABLE** | ForwardAuth middleware protects all endpoints |
| "Full shell access available to public" | **NOT VULNERABLE** | Authentication required; internal network only |
| "No authentication whatsoever" | **NOT VULNERABLE** | HMAC-SHA256 session auth implemented |
| "Exposed ports directly to internet" | **NOT VULNERABLE** | Only Traefik ingress exposed; services internal |
| "Default settings open to attack" | **PARTIALLY VULNERABLE** | Default password "changeme" is critical issue |
| "Prompt injection can delete data" | **N/A** | AI agent behavior, not swe-swe infrastructure |
| "API keys exposed" | **PARTIALLY VULNERABLE** | Env vars visible in docker inspect |

---

## Remediation Priority

### Immediate (This Week)

1. **C1:** Remove default password - require explicit `SWE_SWE_PASSWORD`
2. **C2:** Fix WebSocket origin validation
3. **H1, H2:** Add `signal.Stop()` calls

### Short-term (This Month)

4. **M1:** Fix .env file permissions to 0600
5. **H3:** Handle os.Chown errors properly
6. **H4:** Implement PTY cleanup on session termination
7. **M2:** Add file upload size limits

### Medium-term (This Quarter)

8. **C3:** Migrate to Docker secrets for credentials
9. **C4:** Add warnings/confirmation for `--with-docker`
10. **M7:** Fix goroutine tracking in BroadcastStatus
11. **M10:** Add CSRF protection
12. **M12:** Reduce session expiration

### Long-term

13. Create SECURITY.md documenting threat model
14. Add race condition detector to CI
15. Implement comprehensive audit logging
16. Consider credential rotation automation

---

## Deployment Guidelines

### For Local Development (Recommended)

```bash
# Generate strong password
export SWE_SWE_PASSWORD=$(openssl rand -base64 32)

# Initialize with self-signed SSL
swe-swe init --ssl=selfsign --project-directory ~/project

# Start (binds to localhost only by default)
swe-swe up
```

### For Team/Remote Development

```bash
# Strong password required
export SWE_SWE_PASSWORD=$(openssl rand -base64 32)

# Use Let's Encrypt for valid TLS
swe-swe init --ssl=letsencrypt@myserver.example.com

# Implement firewall rules
ufw allow from trusted_ip to any port 7443

# Share password via secure channel (not in code/chat)
```

### NOT Recommended for Production

- Do not expose to public internet without VPN
- Do not use `--with-docker` flag in shared environments
- Do not store API keys in environment variables for production use

---

## Conclusion

The swe-swe codebase implements a reasonable security architecture with defense-in-depth through Traefik reverse proxy and ForwardAuth middleware. It is **not vulnerable** to the catastrophic "zero authentication" issues described in the ClawdBot article.

However, the **default password issue (C1)** and **WebSocket origin validation (C2)** are critical vulnerabilities that should be addressed immediately. The remaining issues are standard code quality improvements that will improve reliability and debuggability.

**Risk Assessment:**
- With critical issues fixed: **LOW** risk for local/trusted network use
- Current state: **MEDIUM** risk (default password is serious)
- If exposed to internet without fixes: **HIGH** risk
