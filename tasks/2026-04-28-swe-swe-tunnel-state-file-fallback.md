# swe-swe-tunnel state-file fallback (v1.1 follow-up)

## Context

This is the **v1.1 follow-up** to `tasks/2026-04-27-swe-swe-tunnel-integration.md`. The v1 slice (env-only `--public-hostname` + frontend label-swap branching + Playwright e2e in both modes) shipped on `origin/main` in commits `2a7958c17` / `2cfd26fac` / `6c1314c60`.

The companion repo (`choonkeat/swe-swe-tunnel`, lives at `/repos/swe-swe-tunnel/workspace` if cloned alongside) just landed the **producer side** of the state-file fallback — when the tunnel client establishes a session, it now writes a JSON file describing the registration. The default path is `/workspace/.swe-swe/tunnel-state.json`. See `swe-swe-tunnel` commits `66e118d` (`tunnelclient.WriteState`) and `710fa6b` (`--state-file` flag wiring).

This task wires the **consumer side**: extend swe-swe's `resolvePublicHostname` to fall back to that file when neither flag nor env supplies a hostname.

The motivating use case: a wrapper script (or systemd unit, or future swe-swe `init` flow) starts both processes:

```
$ swe-swe-tunnel --server https://tunnel.example.com --unique alice &
$ swe-swe-server   # no --public-hostname, no SWE_PUBLIC_HOSTNAME
```

Today the second command runs in legacy port-mode because it doesn't know its own public hostname. After this change, it reads `/workspace/.swe-swe/tunnel-state.json` (written by the first command) and switches into subdomain mode automatically.

## Concurrent work to be aware of

At the time this task was written, recent commits on `main`:
- `f02c1cb6e` `feat(slash-commands): redesigned /swe-swe:setup`
- `f68eeb79b` `fix(broker): move session-gitconfig dir to /tmp to avoid perm issues`
- `266bc0dcb` / `73bb172bd` / `276bdf8ab` credential-broker follow-ups
- `2a7958c17` / `2cfd26fac` / `6c1314c60` swe-swe-tunnel v1 (the predecessor of this task)

No expected file-level conflicts with that ongoing work. The flag block in `main.go` (line ~1700) was last edited for the tunnel v1 — touch it carefully.

Do **not** start a feature branch — commit directly on `main` per user instruction.

## Contract

The state file written by `swe-swe-tunnel` is **JSON, mode 0600**, with this shape:

```json
{
  "hostname": "alice-tunnel.example.com",
  "unique": "alice",
  "registered_at": "2026-04-28T00:47:31Z"
}
```

Field semantics:
- `hostname` — the full public hostname (`{unique}-tunnel.{apex}`). This is the value `serverPublicHostname` should receive.
- `unique` — the bare label without the `-tunnel` suffix. Informational; swe-swe doesn't currently consume it.
- `registered_at` — RFC3339 UTC, no nanoseconds. Informational; useful for staleness checks if/when added.

The producer guarantees:
- **Atomic write** (tempfile + fsync + rename). Readers see either the previous content or the new content, never partial.
- **Mode 0600**, parent dirs created at 0700 if missing.
- **Default path:** `/workspace/.swe-swe/tunnel-state.json`. Override via `--state-file` flag or `SWE_TUNNEL_STATE_FILE` env on the producer.

## Backend changes

### 1. Extend `resolvePublicHostname` to take a state-file path

Current signature at `cmd/swe-swe/templates/host/swe-swe-server/main.go:135`:

```go
func resolvePublicHostname(flagPublicHostname, envPublicHostname string) string {
    if flagPublicHostname != "" {
        return flagPublicHostname
    }
    return envPublicHostname
}
```

New signature (precedence: flag → env → state file → empty):

```go
// resolvePublicHostname picks the public hostname for tunnel-mode URL
// templating. Precedence: --public-hostname flag > SWE_PUBLIC_HOSTNAME env >
// state file (path may be empty to skip) > "" (legacy/off).
//
// State file is the JSON file written by swe-swe-tunnel after RegisterOK.
// A missing or unreadable file is NOT an error — it just means the
// tunnel isn't running, so we fall through to legacy mode. A *malformed*
// file IS logged as a warning but still falls through; we don't want a
// stale/corrupt state file to crash swe-swe.
func resolvePublicHostname(flagPublicHostname, envPublicHostname, stateFile string, logger *slog.Logger) string {
    if flagPublicHostname != "" {
        return flagPublicHostname
    }
    if envPublicHostname != "" {
        return envPublicHostname
    }
    if stateFile == "" {
        return ""
    }
    hostname, err := readTunnelStateHostname(stateFile)
    if err != nil {
        if !errors.Is(err, os.ErrNotExist) {
            logger.Warn("read tunnel state file", "path", stateFile, "err", err)
        }
        return ""
    }
    return hostname
}
```

(Pick the logger flavor that swe-swe-server already uses — `*slog.Logger` per the snippet, but match local convention. If swe-swe-server doesn't pass a logger here, just use `slog.Default()`.)

### 2. Add `readTunnelStateHostname`

```go
// readTunnelStateHostname reads the JSON state file produced by
// swe-swe-tunnel and returns the hostname field. Errors are returned
// verbatim — the caller decides how to react (a missing file is fine, a
// malformed one is worth logging).
//
// The file shape is documented in the swe-swe-tunnel repo at
// internal/tunnelclient/state.go.
func readTunnelStateHostname(path string) (string, error) {
    raw, err := os.ReadFile(path)
    if err != nil {
        return "", err
    }
    var st struct {
        Hostname string `json:"hostname"`
        // Unique and RegisteredAt are intentionally not consumed today.
    }
    if err := json.Unmarshal(raw, &st); err != nil {
        return "", fmt.Errorf("parse tunnel state %q: %w", path, err)
    }
    if st.Hostname == "" {
        return "", fmt.Errorf("tunnel state %q has empty hostname field", path)
    }
    return st.Hostname, nil
}
```

### 3. Wire the new arg in `main()`

Current call site at `cmd/swe-swe/templates/host/swe-swe-server/main.go:1714`:

```go
serverPublicHostname = resolvePublicHostname(*publicHostname, os.Getenv("SWE_PUBLIC_HOSTNAME"))
```

Add a flag for the state-file path (default `/workspace/.swe-swe/tunnel-state.json`, env `SWE_TUNNEL_STATE_FILE`, empty disables), put it next to `publicHostname`:

```go
publicHostname := flag.String("public-hostname", "",
    "Public hostname swe-swe is reachable at when behind a reverse tunnel "+
        "(e.g. abc-tunnel.example.com). Env: SWE_PUBLIC_HOSTNAME. "+
        "When set, the frontend builds cross-port URLs as {port}.{public-hostname} "+
        "instead of using proxy-port offsets.")
tunnelStateFile := flag.String("tunnel-state-file", "/workspace/.swe-swe/tunnel-state.json",
    "Path to the JSON state file produced by swe-swe-tunnel. Used to discover "+
        "the public hostname when --public-hostname / SWE_PUBLIC_HOSTNAME are unset. "+
        "Env: SWE_TUNNEL_STATE_FILE. Empty string disables.")
```

Then resolve env override (matching the existing pattern for `--public-hostname`):

```go
flag.Parse()
stateFilePath := *tunnelStateFile
if envSF, ok := os.LookupEnv("SWE_TUNNEL_STATE_FILE"); ok && !flagPassed("tunnel-state-file") {
    stateFilePath = envSF
}
serverPublicHostname = resolvePublicHostname(
    *publicHostname,
    os.Getenv("SWE_PUBLIC_HOSTNAME"),
    stateFilePath,
    slog.Default(),
)
```

(`flagPassed` is a tiny `flag.Visit`-based helper — there's likely already a similar one in the codebase; reuse if so. If not, add one alongside the rest of the `main()`-local helpers.)

## Tests

Per the standing test mandate: every feature ships with extensive unit + e2e tests. Live smoke is NOT a substitute.

### Unit tests in `cmd/swe-swe/templates/host/swe-swe-server/tunnel_test.go`

Extend `TestResolvePublicHostname` (or add a new one) with a state-file table:

| Scenario | flag | env | state file content | want |
|---|---|---|---|---|
| All empty, no state file | "" | "" | (file absent) | "" |
| Flag wins | "f.example.com" | "e.example.com" | `{"hostname":"s.example.com",…}` | `"f.example.com"` |
| Env wins over state file | "" | "e.example.com" | `{"hostname":"s.example.com",…}` | `"e.example.com"` |
| State file used when flag+env empty | "" | "" | `{"hostname":"s.example.com",…}` | `"s.example.com"` |
| Empty stateFile path skips reading | "" | "" | (path = "") | `""` |
| Malformed JSON | "" | "" | `not json` | `""` (and a Warn was logged) |
| Empty hostname field | "" | "" | `{"hostname":""}` | `""` |
| Missing file at given path | "" | "" | (no file) | `""` (no Warn — ENOENT is silent) |

Use `t.TempDir()` for state-file path. Inject a `bytes.Buffer`-backed `slog.Logger` so you can assert the malformed-JSON case logs a Warn but the missing-file case does not.

Also add a focused unit test for `readTunnelStateHostname` covering: happy path, missing file, malformed JSON, empty hostname, file with extra unknown fields (forwards-compat — should still parse), 0600 perms not required (we read; we don't enforce).

### E2E test (Playwright)

Extend the existing tunnel e2e (the one added in `6c1314c60`) with a third mode:

- **State-file mode**: pre-write `/workspace/.swe-swe/tunnel-state.json` with `{"hostname":"fake-tunnel.example.com","unique":"fake","registered_at":"2026-04-28T00:00:00Z"}` BEFORE starting swe-swe-server, with `--public-hostname=""` and `SWE_PUBLIC_HOSTNAME` unset. Assert iframe `src` is `https://{previewPort}.fake-tunnel.example.com` (raw port, no `proxyPortOffset`) — exactly the same assertion as the env-mode test, just sourced differently.
- Tear down by removing the state file.

Keep the env-unset regression and the env-set tunnel-mode tests untouched — this is purely additive.

## Acceptance

- `go test ./...` clean across the whole repo.
- `go vet ./...` clean.
- Playwright e2e: legacy mode unchanged, env mode unchanged, state-file mode green.
- Manual one-off: with both binaries running, `swe-swe-server` (no flag, no env) picks up `tunnel-state.json` at startup and the iframe shows the subdomain URL.

## Gotchas

- **Two repos.** This task is for **swe-swe** (`/workspace`). The producer is **swe-swe-tunnel** at `/repos/swe-swe-tunnel/workspace`. Don't conflate paths or push to the wrong remote.
- **State file ENOENT is silent.** A missing file is the *normal* case when no tunnel is running; logging a Warn there would be noise. Only log on malformed-JSON / empty-hostname / read-error cases.
- **Don't fail-stop on bad state file.** Stale or corrupt state file → log a Warn + fall through to `""` (legacy mode). Never `os.Exit` from the resolver.
- **Don't read the state file every request.** Resolution happens *once* at startup (current pattern), then `serverPublicHostname` is set. Don't change that — re-reading on every request would invite races with the producer's atomic-replace and add IO to the hot path.
- **Don't enforce file mode.** The producer writes 0600; the consumer should not require it. Permissive read is the right call.
- **Forwards compat.** The state struct in this task only consumes `hostname`. The producer also writes `unique` and `registered_at`. If we later want `registered_at` (e.g. staleness check: ignore files older than 24h), add it as a separate change with its own justification.
- **Test discipline.** Standing user mandate: extensive unit + e2e tests for every feature. Live smoke verification of the manual flow is NICE but does not replace the test files above.
- **Commit style.** Match recent commit messages (single-line subject ≤ ~70 chars + body; co-author trailer per the team convention).

## Files you'll touch

- `cmd/swe-swe/templates/host/swe-swe-server/main.go` — extend `resolvePublicHostname`, add `readTunnelStateHostname`, add `--tunnel-state-file` flag + env wiring.
- `cmd/swe-swe/templates/host/swe-swe-server/tunnel_test.go` — extend `TestResolvePublicHostname`, add `TestReadTunnelStateHostname`.
- The Playwright e2e file added in commit `6c1314c60` — add a third scenario.
