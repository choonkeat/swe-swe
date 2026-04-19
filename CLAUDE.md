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
- **`.swe-swe/`** — Internal. Subdirectories (`docs/`, `uploads/`, etc.) plus one explicit loose file: **`.swe-swe/env`** (per-workspace env vars sourced by swe-swe-server and `/etc/profile.d/zz-swe-swe-env.sh`). Do not add other loose files here.

## Browser / Manual testing

Agent will
1. boot up test container with docs/dev/test-container-workflow.md
2. use mcp browser to test
3. shutdown the test container

## App Crash Investigation

See `docs/dev/crash-forensics.md` for the full 5-step crash forensics runbook (detect, compose logs, auditd, host state, crash logs) and interpretation cheat sheet.

## Running Host Commands as Root

The Docker daemon runs on the host (not in a VM). We can execute host-level commands as root via:
```bash
docker run --rm --privileged --pid=host alpine:latest \
    nsenter -t 1 -m -u -i -n -- <command>
```

## Host Security Audit

See `docs/dev/host-security-audit.md` for quick checks (SSH, fail2ban, ports, firewall, processes) and port layout.

## Coding Rules

**Never silently discard child process exit status.** Do not write `go func() { cmd.Wait() }()`. Always log the process name, PID, and exit status. A silent Wait hid the Chrome singleton bug for the entire per-session browser rollout — Chrome exited immediately on all but the first session but no log entry was produced.

## Known Issues

### Memory leak: per-request `http.Transport` (fixed 2026-03-07)

**Symptom:** Server RSS grows to 8+ GB over ~48 hours → kernel OOM kill (exit 137).

**Root cause:** `agentChatProxyHandler` created a new `http.Client{Transport: &http.Transport{}}` on every proxied request. Each Transport allocates its own connection pool and TLS state that are never released.

**Fix:** Shared package-level `var agentChatClient` with `MaxIdleConnsPerHost: 10` and `IdleConnTimeout: 90s`. Never create per-request `http.Transport` instances — always reuse a shared client.

**Rule:** Do not create `&http.Transport{}` inside request handlers. Use a package-level or struct-level `http.Client` with a shared Transport.
See .swe-swe/docs/AGENTS.md (if it exists) for context of this current environment
