# Tmpfs Uploads: Robust File Upload Solution

## Background

The file upload feature saves files to `.swe-swe/uploads/` inside `/workspace`. The server runs as the `app` user (non-root) for security. The `entrypoint.sh` runs `chown -R app:app /workspace/.swe-swe` as root to fix permissions.

**Problem**: On macOS Docker Desktop, bind mounts use VirtioFS/gRPC-FUSE which doesn't support `chown` — it fails with EPERM, crashing the container before the server starts.

**Solution**: Combine two approaches:
- **Option A (tmpfs)**: Add a tmpfs overlay for uploads in docker-compose — bulletproof, always writable
- **Option C (pre-create)**: Create `.swe-swe/uploads/` during `swe-swe init` on the host — best-effort optimization

This allows removing the problematic `chown` block from `entrypoint.sh`.

## Phases

1. **Phase 1**: Pre-create `.swe-swe/uploads/` during `swe-swe init`
2. **Phase 2**: Add tmpfs mount for uploads in docker-compose template
3. **Phase 3**: Remove the `chown` block from `entrypoint.sh`
4. **Phase 4**: Integration testing

---

## Phase 1: Pre-create `.swe-swe/uploads/` during `swe-swe init` ✅

### What will be achieved

When `swe-swe init` runs on the host, it will create the `.swe-swe/uploads/` directory inside the project. This ensures the directory exists with host-user ownership before the container starts.

### Small steps

1. Locate where `swe-swe init` creates directories in `cmd/swe-swe/main.go`
2. Add `os.MkdirAll(projectDir+"/.swe-swe/uploads", 0755)` alongside existing directory creation
3. **On failure**: Log a warning, do not abort init (tmpfs handles it at runtime)
4. Update golden tests to reflect the new directory in test output

### Verification

1. **Unit test (golden)**: Run `make build golden-update`, verify the golden diff shows `.swe-swe/uploads` being created
2. **Manual test**: Run `./dist/swe-swe init` in a test directory, confirm `.swe-swe/uploads/` exists with correct permissions
3. **Regression check**: Existing golden tests still pass for all init variants

---

## Phase 2: Add tmpfs mount for uploads in docker-compose template ✅

### What will be achieved

The generated `docker-compose.yml` will include a tmpfs mount at `/workspace/.swe-swe/uploads`, ensuring the container can always write uploads regardless of bind mount permission issues.

### Small steps

1. Locate the docker-compose template in `cmd/swe-swe/templates/host/docker-compose.yml`
2. Add tmpfs volume entry under the swe-swe service's volumes section:
   ```yaml
   - type: tmpfs
     target: /workspace/.swe-swe/uploads
     tmpfs:
       mode: 0755
   ```
3. Update golden tests to reflect the new volume in generated compose files

### Verification

1. **Unit test (golden)**: Run `make build golden-update`, verify the golden diff shows the tmpfs mount in docker-compose.yml
2. **Manual test**:
   - Run `swe-swe init` + `swe-swe start`
   - Exec into container: `docker exec -it <container> ls -la /workspace/.swe-swe/uploads`
   - Confirm it's a tmpfs mount: `docker exec -it <container> mount | grep uploads`
3. **Regression check**:
   - All existing golden tests pass
   - Container starts successfully on both Linux and macOS

---

## Phase 3: Remove the `chown` block from `entrypoint.sh` ✅

### What will be achieved

The `chown -R app:app /workspace/.swe-swe` block (lines 51-57) will be removed from `entrypoint.sh`. This eliminates the EPERM failures on macOS Docker Desktop bind mounts.

### Small steps

1. Remove lines 51-57 from `cmd/swe-swe/templates/host/entrypoint.sh`:
   ```bash
   # Ensure .swe-swe/uploads directory exists and is writable by app user
   # (the .swe-swe directory may have been created by a different user on the host)
   if [ -d /workspace/.swe-swe ]; then
       mkdir -p /workspace/.swe-swe/uploads
       chown -R app:app /workspace/.swe-swe
       echo -e "${GREEN}✓ Ensured .swe-swe directory is writable${NC}"
   fi
   ```
2. Update the header comment (lines 4-9) to remove mention of uploads directory handling
3. Update golden tests to reflect the simplified entrypoint

### Verification

1. **Unit test (golden)**: Run `make build golden-update`, verify the golden diff shows the removed block
2. **Manual test on macOS**:
   - Run `swe-swe init` + `swe-swe start` on macOS Docker Desktop
   - Confirm container starts without EPERM errors
   - Confirm file upload works (drop a file in the terminal UI)
3. **Regression check**:
   - Container starts on Linux
   - File upload still works (tmpfs mount handles writes)
   - Certificate installation still works
   - Docker socket permission handling still works

---

## Phase 4: Integration testing

### What will be achieved

End-to-end verification that the complete file upload flow works on a fresh container, confirming all three phases work together.

### Small steps

1. Build the binary: `make build`
2. Deploy test instance using existing scripts:
   ```bash
   ./scripts/01-test-container-init.sh
   ./scripts/02-test-container-build.sh
   HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh
   ```
3. Verify tmpfs mount:
   ```bash
   docker exec swe-swe-test mount | grep uploads
   ```
4. Test file upload via Playwright MCP:
   - Navigate to `http://host.docker.internal:9899/`
   - Upload a test file (text and/or image)
   - Confirm upload succeeds and path is sent to terminal
5. Verify no chown errors in container logs:
   ```bash
   docker logs swe-swe-test 2>&1 | grep -i "chown\|permission\|EPERM"
   ```
6. Teardown: `./scripts/04-test-container-down.sh`

### Verification

1. **Success criteria**:
   - Container starts without errors
   - `mount | grep uploads` shows tmpfs
   - File upload completes successfully
   - No permission-related errors in logs
2. **Regression check**:
   - Existing features (terminal, websocket, certs, docker socket) still work
   - Desktop drag-and-drop still works
   - Mobile file picker still works (if implemented)

---

## Files to modify

| File | Change |
|------|--------|
| `cmd/swe-swe/main.go` | Add `.swe-swe/uploads` directory creation during init |
| `cmd/swe-swe/templates/host/docker-compose.yml` | Add tmpfs mount for uploads |
| `cmd/swe-swe/templates/host/entrypoint.sh` | Remove chown block |
| `cmd/swe-swe/testdata/golden/*` | Update to reflect changes |
