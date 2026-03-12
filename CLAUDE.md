# Claude Code Instructions

## Testing

Always use `make test` (not `go test` directly) to run tests. The Makefile ensures consistent test execution.

## `swe-swe init` Changes

Templates live in `cmd/swe-swe/templates/host/` and are embedded in the binary at build time. See `docs/dev/template-editing-guide.md` for the full workflow.

After modifying templates, always run `make build golden-update` then verify:
```bash
git add -A cmd/swe-swe/testdata/golden
git diff --cached -- cmd/swe-swe/testdata/golden
```

**Note**: Always use `make golden-update` (not individual `_golden-variant` targets). The Makefile manages a temporary symlink that only exists during the full run.

### Adding new flags

Use a two-commit TDD approach:

1. **Baseline**: Add flag parsing (no effect yet) + golden test variants
   - Add flag to command parsing and `InitConfig` struct
   - Add test variants in `cmd/swe-swe/main_test.go`
   - Run `make build golden-update`, commit (shows flag in init.json only)

2. **Implementation**: Make flag take effect
   - Implement functionality (template changes, etc.)
   - Run `make build golden-update`, verify diff shows only functional changes, commit

## swe-swe Directory Convention

Inside the container's `/workspace/`:

- **`swe-swe/`** — Agent commands only. All files here are `@`-mentionable.
- **`.swe-swe/`** — Internal. Only subdirectories (no loose files): `docs/`, `uploads/`, etc.

## Browser / Manual testing

Agent will
1. boot up test container with docs/dev/test-container-workflow.md
2. use mcp browser to test
3. shutdown the test container

## App Crash Investigation

The swe-swe server has crash forensics built in. Follow these steps in order when investigating a crash.

### Step 1: Detect the crash
```bash
# Check container restarts (restarts > 0 = crash happened)
docker inspect --format '{{.Name}} status={{.State.Status}} restarts={{.RestartCount}} oom={{.State.OOMKilled}} started={{.State.StartedAt}}' $(docker ps -aq --filter "name=swe-swe")

# Heartbeat (PID + last-alive timestamp, written every 1s by server)
docker exec $(docker ps -q --filter "name=swe-swe-1") cat /tmp/swe-swe-heartbeat

# Docker events — look for die/kill/oom events
timeout 3 docker events --since "6h" --until "0s" --filter "type=container" \
  --filter "event=die" --filter "event=kill" --filter "event=oom" \
  --format '{{.Time}} {{.Action}} {{.Actor.Attributes.name}} exit={{.Actor.Attributes.exitCode}}'
```

### Step 2: Check the compose log
The server logs `[SIGNAL]`, `[KILL]`, and `[BUG]` prefixed entries for forensics:
- **`[SIGNAL]`** — All catchable Unix signals received by the server (from `startSignalMonitor()` in main.go). SIGURG/SIGCHLD are suppressed unless `LOGGING=verbose`. If server dies with no `[SIGNAL]` entry, it was SIGKILL (uncatchable).
- **`[KILL]`** — Every `syscall.Kill()` call the server makes, showing target PID and server PID. Appears before session process group kills, descendant kills, and port cleanup kills.
- **`[BUG]`** — Self-kill guard: logged if `collectDescendantPIDs()` ever includes the server's own PID (should never happen).

```bash
LATEST_COMPOSE=$(ls -t /workspace/.swe-swe/logs/compose-*.log | head -1)

# Check all forensic markers
grep '\[SIGNAL\]' "$LATEST_COMPOSE" | grep -v 'monitor started'
grep '\[KILL\]' "$LATEST_COMPOSE"
grep '\[BUG\]' "$LATEST_COMPOSE"

# Check for panics (caught by recoverGoroutine() in 7 goroutines)
grep -i -E 'panic|runtime error|goroutine [0-9]+|RECOVERED|recoverGoroutine' "$LATEST_COMPOSE"

# Check for fatal/OOM
grep -i -E 'fatal|oom|out of memory' "$LATEST_COMPOSE" | grep -v 'git'

# "Killed" bare message = SIGKILL death (exit 137)
grep '^swe-swe-1.*Killed$' "$LATEST_COMPOSE"

# Last 50 lines before crash
tail -50 "$LATEST_COMPOSE"
```

### Step 3: Check auditd for SIGKILL sender
auditd is installed on the host with a rule tracking all `kill -9` syscalls. This is the **only way** to identify who sent a SIGKILL.
```bash
docker run --rm --privileged --pid=host alpine:latest \
    nsenter -t 1 -m -u -i -n -- ausearch -k sigkill_track --start recent -i
```
Key fields in the output:
- **`pid`** + **`comm`/`exe`** = the process that sent SIGKILL (the killer)
- **`uid`** = user that sent it
- **`opid`** + **`ocomm`** = the process that received SIGKILL (the victim)
- **`subj=docker-default`** = kill came from inside a Docker container
- **`subj=unconfined`** = kill came from the host

Rule is persistent across reboots in `/etc/audit/rules.d/sigkill.rules`.

### Step 4: Check host-level state
```bash
# OOM killer (from inside container, use nsenter)
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- dmesg -T | grep -i oom

# SSH activity around crash time (someone Ctrl+C'd in screen?)
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- grep "Accepted\|session" /var/log/auth.log | tail -20

# Memory at crash time
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- sar -r -s HH:MM:00 -e HH:MM:00
```

### Step 5: Check crash forensic logs (if restart-loop2.sh triggered)
```bash
ls -lht /workspace/.swe-swe/logs/crash-*.log | head -5
cat /workspace/.swe-swe/logs/crash-YYYYMMDD-HHMMSS.log
```
These capture container state, docker events, heartbeat, and auditd output at the moment of each restart cycle.

### Interpretation cheat sheet
| Evidence | Meaning |
|----------|---------|
| No `[SIGNAL]` before crash | SIGKILL (uncatchable) from external source |
| `[SIGNAL] terminated` then crash | SIGTERM from Docker or systemd |
| `[SIGNAL] broken pipe` | SIGPIPE from websocket disconnect (harmless, caught by monitor) |
| `[KILL]` near crash with wrong target | Server's own kill call misfired |
| `exit=137` in docker events | SIGKILL (128+9) |
| `exit=143` in docker events | SIGTERM (128+15) — graceful shutdown |
| `OOMKilled=true` | Kernel OOM killer |
| Panic stacktrace in compose log | Go runtime crash (should be caught by `recoverGoroutine`) |
| auditd `comm=bash subj=docker-default` | A bash process inside a container sent the kill (likely a Claude session with `--dangerously-skip-permissions`) |
| auditd `comm=containerd-shim subj=unconfined` | Docker/containerd killed it (resource limit, health check, or `docker kill`) |

See `/workspace/.swe-swe/restart-loop2.md` for full documentation of the restart loop and forensics setup.

## Running Host Commands as Root

The Docker daemon runs on the host (not in a VM). We can execute host-level commands as root via:
```bash
docker run --rm --privileged --pid=host alpine:latest \
    nsenter -t 1 -m -u -i -n -- <command>
```
This enters PID 1's namespaces (mount, UTS, IPC, net) giving full host access. Used for:
- Installing packages (`apt-get install`)
- Managing systemd services
- Reading host logs (`/var/log/auth.log`, `dmesg`, `journalctl`)
- Running `auditctl`/`ausearch` for kernel audit
- Checking `ufw`, `fail2ban`, `ss`, etc.

## Host Security Audit

### Quick checks
```bash
# Failed SSH logins and brute force
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- sh -c 'grep -c "Failed password" /var/log/auth.log; grep "Invalid user" /var/log/auth.log | tail -10'

# Successful SSH logins
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- grep "Accepted" /var/log/auth.log | tail -10

# fail2ban status
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- fail2ban-client status sshd

# Listening ports
docker run --rm --privileged --pid=host --net=host alpine:latest nsenter -t 1 -m -u -i -n -- ss -tlnp

# Firewall
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- ufw status

# Users with login shells
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- grep -v 'nologin\|false' /etc/passwd

# Pending security updates
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- apt list --upgradable 2>/dev/null | grep security

# Suspicious processes
docker run --rm --privileged --pid=host alpine:latest nsenter -t 1 -m -u -i -n -- ps aux --sort=-%cpu | head -15
```

### Port layout (swe-swe managed)
| Ports | Purpose | Auth |
|-------|---------|------|
| 22 | SSH | pubkey + fail2ban |
| 80 | Traefik HTTP | entry point |
| 1977, 3000-3019, 4000-4019, 23000-23019, 24000-24019 | swe-swe session ports via Traefik | behind auth |
| 5000-5019 | Preview ports (direct access) | no auth, user-controlled |

## Coding Rules

**Never silently discard child process exit status.** Do not write `go func() { cmd.Wait() }()`. Always log the process name, PID, and exit status. A silent Wait hid the Chrome singleton bug for the entire per-session browser rollout — Chrome exited immediately on all but the first session but no log entry was produced.

## Known Issues

### Memory leak: per-request `http.Transport` (fixed 2026-03-07)

**Symptom:** Server RSS grows to 8+ GB over ~48 hours → kernel OOM kill (exit 137).

**Root cause:** `agentChatProxyHandler` created a new `http.Client{Transport: &http.Transport{}}` on every proxied request. Each Transport allocates its own connection pool and TLS state that are never released.

**Fix:** Shared package-level `var agentChatClient` with `MaxIdleConnsPerHost: 10` and `IdleConnTimeout: 90s`. Never create per-request `http.Transport` instances — always reuse a shared client.

**Rule:** Do not create `&http.Transport{}` inside request handlers. Use a package-level or struct-level `http.Client` with a shared Transport.