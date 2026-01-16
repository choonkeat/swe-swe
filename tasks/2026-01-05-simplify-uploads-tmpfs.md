# Simplify .swe-swe/uploads: Remove Pre-creation, Improve UX, Document in ADR

## Goal

Simplify the `.swe-swe/uploads` implementation by:
1. Removing unnecessary pre-creation code from main.go
2. Relying solely on tmpfs mount in docker-compose.yml
3. Improving UX with user feedback about ephemeral uploads
4. Documenting the decision and learnings in ADR-0017

## Background

The `tmpfs-uploads` branch introduced a hybrid approach:
- Pre-create `.swe-swe/uploads/` on host during `swe-swe init`
- Mount tmpfs at `/workspace/.swe-swe/uploads` in docker-compose.yml

Code review identified that pre-creation is unnecessary:
- On macOS/Windows: tmpfs shadows the host directory completely
- On Linux: tmpfs also works fine, pre-creation is redundant
- Creates false user expectation that uploads persist on host

Additionally, users have no indication that uploads are ephemeral (stored in RAM, lost on container stop).

---

## Phase 1: Remove pre-creation from main.go

### What will be achieved
Remove the host-side `.swe-swe/uploads/` directory creation code. The tmpfs mount in docker-compose.yml handles everything at container runtime.

### Steps

1. [x] Identify the code to remove in `cmd/swe-swe/main.go`
   - Find the `os.MkdirAll` call for uploads directory
   - Find any associated logging/warning code

2. [x] Remove the pre-creation code
   - Delete the uploads directory creation logic
   - Keep any other `.swe-swe` directory handling intact

3. [x] Update golden test files
   - Run `make build golden-update`
   - Review golden diff shows only expected removal
   - Commit changes to testdata/golden

### Verification

1. **Before changes (baseline)**:
   - `swe-swe init` creates `.swe-swe/uploads/` on host
   - Golden files include this behavior

2. **After removal**:
   - `swe-swe init` no longer creates `.swe-swe/uploads/` on host
   - Container still starts successfully (tmpfs creates the directory)
   - File uploads still work end-to-end

3. **Regression check**:
   - `make build` passes
   - `make golden-update` shows only expected changes
   - Integration test verifies uploads still work

---

## Phase 2: UX improvement - inform users uploads are ephemeral

### What will be achieved
Add clear user feedback in the web UI that uploaded files are temporary (stored in RAM) and will be lost when the container stops. Guide users to move important files to `/workspace` for persistence.

### Steps

1. [x] Identify where to add UX feedback
   - Find file upload UI components in `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js`
   - Locate the upload button area and success message handling

2. [x] Add tooltip/hint near the attachment button
   - Add info text indicating uploads are temporary
   - Text: "Uploads are temporary (in-memory). Move to /workspace to keep."

3. [x] Enhance upload success message
   - When file upload succeeds, include persistence guidance
   - E.g., append "(temporary)" to the upload confirmation

4. [x] Update golden test files
   - Run `make build golden-update`
   - Commit changes to testdata/golden

### Verification

1. **Before changes**:
   - Upload a file → no indication it's temporary
   - User has no way to know uploads are ephemeral

2. **After changes**:
   - Attachment button area shows hint about temporary storage
   - Upload success message includes persistence guidance
   - Visual inspection in browser confirms UX improvement

3. **Regression check**:
   - `make build` passes
   - Existing upload functionality unchanged
   - Golden files updated and committed

---

## Phase 3: Write ADR-0017 documenting uploads architecture

### What will be achieved
Create an Architecture Decision Record capturing all learnings from this scenario.

### Steps

1. [x] Create `docs/adr/0017-tmpfs-uploads.md` with ADR format

2. [x] Document the following learnings:
   - **Original problem**: macOS Docker Desktop VirtioFS doesn't support `chown`
   - **Solution**: tmpfs mount bypasses VirtioFS (Docker-created filesystem)
   - **Why pre-creation was removed**: shadowed by tmpfs, misleading to users
   - **Ephemeral by design**: session-scoped uploads, move to /workspace for persistence
   - **100MB limit rationale**: prevents unbounded memory usage
   - **OS detection rejected**: adds complexity, reduces portability, tmpfs works everywhere
   - **UX considerations**: users need to know uploads are temporary

3. [x] Update `docs/adr/README.md` index with ADR-0017

### Verification

1. **Completeness check**:
   - ADR answers: What? Why? What alternatives? What tradeoffs?
   - All key learnings captured

2. **Format check**:
   - Matches existing ADR style
   - Proper numbering (0017)

3. **Cross-reference check**:
   - Links to relevant files are valid
   - Technical details are accurate

---

## References

- Previous task: `tasks/2026-01-05-tmpfs-uploads.md`
- WebSocket protocol docs: `docs/websocket-protocol.md` (file upload protocol)
- Related commits on `tmpfs-uploads` branch:
  - `e1eeb7c` fix(docker-compose): use mode 0777 for tmpfs
  - `ce61947` feat(docker-compose): limit tmpfs uploads to 100MB
  - `c954ece` test: verify tmpfs uploads integration
