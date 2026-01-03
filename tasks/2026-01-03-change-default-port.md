# Change Default Port from 9899 to 1977

## Goal

Change the default exposed Traefik port from 9899 to 1977.

Users can still override at runtime: `SWE_PORT=8080 swe-swe up`

---

## Phase 1: Update docker-compose template + golden tests ✅

### What will be achieved
- Default port changes from 9899 to 1977

### Steps
1. ✅ Edit `cmd/swe-swe/templates/host/docker-compose.yml`
   - Change `${SWE_PORT:-9899}:7000` to `${SWE_PORT:-1977}:7000`
2. ✅ Run `make build golden-update`
3. ✅ Verify diff shows only port changes in docker-compose.yml files
4. ✅ Commit template + golden files together

### Verification
- ✅ `make test` passes
- Note: 9899 still appears in documentation files (browser-automation.md, main.go comments) - will be fixed in Phase 2

---

## Phase 2: Update documentation

### What will be achieved
- All docs reflect new default port 1977

### Steps
1. Update `README.md` (~10 occurrences of 9899)
2. Update `docs/adr/0002-path-based-routing.md`
3. Update `docs/browser-automation.md`
4. Update `cmd/swe-swe/templates/host/swe-swe-server/main.go` (VNC URL comment)
5. Update `cmd/swe-swe/templates/container/.swe-swe/browser-automation.md`
6. Run `make build golden-update` (for template changes)
7. Commit all changes

### Verification
- `make test` passes
- `grep -r 9899` shows only `research/` and `tasks/` (historical files)

---

## Final Verification

- All tests pass
- No 9899 in active code/templates/docs (only historical research/tasks files)
