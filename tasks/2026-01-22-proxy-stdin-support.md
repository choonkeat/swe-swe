# Task: Add stdin Support to `swe-swe proxy`

## Goal
Enable the container script to forward stdin to the proxied command on the host for non-interactive (piped/redirected) input.

## Scope
- **In scope**: Piped stdin (`echo "hi" | proxy/cat`), redirected stdin (`proxy/wc < file.txt`)
- **Out of scope**: Interactive/streaming stdin (Phase 2, future work)

## Behavior

| Scenario | stdin type | Behavior |
|----------|------------|----------|
| `.swe-swe/proxy/make build` | TTY | Warning to stderr, no stdin forwarded |
| `echo "hi" \| .swe-swe/proxy/cat` | Pipe | stdin captured and forwarded |
| `.swe-swe/proxy/wc < file.txt` | File | stdin captured and forwarded |

**Warning message** (stderr):
```
[proxy] Warning: stdin is a TTY, input will not be forwarded (use piping instead)
```

---

## Steps

### Step 1.1: Add integration test for stdin (RED)
- [x] Add test in `proxy_integration_test.go` that pipes stdin to a proxied command (e.g., `echo "hello" | proxy/cat`)
- [x] Verify test fails (stdin not implemented yet)

### Step 1.2: Fix UUID generation for cross-platform compatibility
- [x] Replace `/proc/sys/kernel/random/uuid` with `/dev/urandom` approach
- [x] Add uniqueness loop to prevent collisions
- [x] Code:
  ```bash
  while true; do
      uuid=$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')
      [[ ! -f "$PROXY_DIR/$uuid.req" ]] && break
  done
  ```

### Step 1.3: Modify container script - TTY detection, warning, stdin capture
- [x] Add TTY detection with `[[ -t 0 ]]`
- [x] If TTY: print warning to stderr, skip stdin capture
- [x] If not TTY: read stdin into `$PROXY_DIR/$uuid.stdin` file
- [x] Code:
  ```bash
  stdin_file="$PROXY_DIR/$uuid.stdin"
  if [[ -t 0 ]]; then
      echo "[proxy] Warning: stdin is a TTY, input will not be forwarded (use piping instead)" >&2
  else
      cat > "$stdin_file"
  fi
  ```

### Step 1.4: Modify host `processRequest()` - pipe stdin file to command
- [ ] Check if `<uuid>.stdin` file exists
- [ ] If exists: open and set as `cmd.Stdin`
- [ ] Clean up `.stdin` file in cleanup (add to orphan suffixes)

### Step 1.5: Update golden tests
- [ ] Run `make build golden-update`
- [ ] Verify diff shows only stdin-related changes (UUID generation, stdin handling)
- [ ] Commit golden updates

### Step 1.6: Run full test suite
- [ ] Run `make test`
- [ ] Verify all tests pass (no regressions)

---

## Verification Checklist

- [ ] `echo "hello" | .swe-swe/proxy/cat` outputs "hello"
- [ ] `.swe-swe/proxy/echo test` works without hanging (TTY case)
- [ ] TTY case prints warning to stderr
- [ ] `make test` passes
- [ ] Golden diff is minimal and correct

---

## Files to Modify

1. `cmd/swe-swe/proxy.go` - container script template, `processRequest()`, orphan cleanup
2. `cmd/swe-swe/proxy_integration_test.go` - new stdin test
3. `cmd/swe-swe/testdata/golden/*/swe-swe-server/main.go` - golden updates (via `make golden-update`)
