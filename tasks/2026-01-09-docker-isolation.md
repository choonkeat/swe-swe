# Docker Label Constraints for Multi-Project Isolation

## Goal

Implement Docker label constraints to enable robust multi-project isolation, allowing multiple swe-swe instances to run simultaneously without Traefik discovery conflicts. This enables agents to perform MCP browser tests reliably.

## Background

When running multiple swe-swe projects simultaneously, Traefik instances can conflict because:
1. Each Traefik sees ALL containers via the shared Docker socket
2. Router/middleware names are hardcoded (e.g., `auth`, `chrome`, `swe-swe`)
3. Port collisions if `SWE_PORT` not set differently

Solution: Docker label constraints (Option 2 from analysis) - each Traefik only discovers containers with a matching `swe.project` label.

---

## Phase 1: Template Changes ✅ COMPLETE

### What Will Be Achieved
The docker-compose.yml template will be modified so that:
- Traefik uses `--providers.docker.constraints` to only discover containers with a matching project label
- All services get a `swe.project=${PROJECT_NAME}` label
- Router and middleware names are prefixed with `${PROJECT_NAME}` as a safety net

### Steps

1. **Add constraint flag to Traefik command section**:
   ```yaml
   - "--providers.docker.constraints=Label(`swe.project`,`${PROJECT_NAME}`)"
   ```

2. **Add project label to each service** (traefik, auth, chrome, swe-swe, vscode-proxy, code-server):
   ```yaml
   labels:
     - "swe.project=${PROJECT_NAME}"
   ```

3. **Prefix router names** in all services:
   - `traefik.http.routers.auth` → `traefik.http.routers.${PROJECT_NAME}-auth`
   - `traefik.http.routers.chrome` → `traefik.http.routers.${PROJECT_NAME}-chrome`
   - `traefik.http.routers.swe-swe` → `traefik.http.routers.${PROJECT_NAME}-swe-swe`
   - `traefik.http.routers.vscode` → `traefik.http.routers.${PROJECT_NAME}-vscode`

4. **Prefix middleware names**:
   - `chrome-redirect` → `${PROJECT_NAME}-chrome-redirect`
   - `chrome-strip` → `${PROJECT_NAME}-chrome-strip`
   - Update middleware references in router configs

5. **Prefix service names** (Traefik load balancer references):
   - `traefik.http.services.auth` → `traefik.http.services.${PROJECT_NAME}-auth`
   - etc.

### Verification

1. **Red**: Before changes, run `make build golden-update` to capture current state
2. **Green**: After template changes, run `make build golden-update` and verify:
   - Golden diffs show new labels and prefixed names
   - Template still parses correctly (no syntax errors)
3. **Refactor**: Review diff for completeness - every service must have the label, every router/middleware/service name must be prefixed

---

## Phase 2: Init Logic

### What Will Be Achieved
The `swe-swe init` command will generate a `PROJECT_NAME` variable that:
- Is written to the `.env` file in the initialized project
- Has a sensible default derived from the project directory name
- Is sanitized to be a valid Docker label value (alphanumeric, hyphens, underscores)

### Steps

1. **Research current init flow**: Examine how `swe-swe init` generates files and where environment variables are set

2. **Add PROJECT_NAME generation logic**:
   - Derive from directory name (e.g., `/home/user/myproject` → `myproject`)
   - Sanitize: lowercase, replace invalid chars with hyphens, truncate if too long
   - Ensure uniqueness hint (directory name should suffice)

3. **Write PROJECT_NAME to output**:
   - Add to `.env` file template: `PROJECT_NAME=<sanitized-name>`

4. **Add flag for override** (optional but recommended):
   - `--project-name` flag to allow explicit override
   - Follows the two-commit TDD approach from CLAUDE.md

### Verification

1. **Red**:
   - Write a test case in `cmd/swe-swe/main_test.go` that expects `PROJECT_NAME` in output
   - Test should fail initially

2. **Green**:
   - Implement the generation logic
   - Run `make test`
   - Test passes

3. **Refactor**:
   - Run `make build golden-update`
   - Verify golden files show `PROJECT_NAME` in `.env` or config
   - Verify sanitization works (test with directory names containing spaces, special chars)

4. **Edge cases to test**:
   - Directory name with spaces: `my project` → `my-project`
   - Directory name with dots: `my.project` → `my-project`
   - Very long names: truncate to reasonable length (32 chars?)
   - Already valid names pass through unchanged

---

## Phase 3: Test Container Workflow

### What Will Be Achieved
The test container workflow will be updated so that:
- Agents use `swe-swe init` to create properly isolated test stacks
- A slot-based semaphore mechanism assigns unique ports and project names
- Each test stack gets a unique `PROJECT_NAME` and `SWE_PORT`
- Temp workspace is initialized as a git repo for worktree operations
- Scales transparently - add more slots as machine capacity grows

### Slot-Based Design

| Slot | PROJECT_NAME | SWE_PORT |
|------|--------------|----------|
| 0    | swe-test-0   | 19770    |
| 1    | swe-test-1   | 19771    |
| 2    | swe-test-2   | 19772    |
| ...  | ...          | ...      |

MCP browser uses `host.docker.internal:{SWE_PORT}` to reach the correct stack.

### Steps

1. **Audit current test workflow**:
   - Read `/workspace/.swe-swe/test-container-workflow.md`
   - Understand current approach

2. **Design semaphore mechanism**:
   - Lock files: `/tmp/swe-swe-test-slot-{N}.lock`
   - Agent acquires first available slot
   - Gets PROJECT_NAME and SWE_PORT from slot number
   - Timeout/stale lock handling (e.g., 10 minute timeout)

3. **Initialize temp workspace as git repo**:
   ```bash
   mkdir -p /tmp/swe-swe-test-{slot}
   cd /tmp/swe-swe-test-{slot}
   git init
   git commit --allow-empty -m "initial"
   ```

4. **Update test workflow to use `swe-swe init`**:
   - Acquire slot
   - Init git repo in temp directory
   - `swe-swe init --project-directory=/tmp/swe-swe-test-{slot}`
   - Set `PROJECT_NAME=swe-test-{slot}`, `SWE_PORT=1977{slot}` in .env
   - `docker-compose up -d`
   - MCP browser tests on `http://host.docker.internal:{SWE_PORT}`
   - `docker-compose down`
   - Release slot

5. **Handle cleanup on failure**:
   - Ensure containers are torn down even if tests fail
   - Stale lock detection

6. **Evaluate `./scripts` redundancy**:
   - If workflow now uses `swe-swe init` directly, deprecate or remove scripts

### Verification

1. **Red**:
   - Current workflow may not have semaphore
   - Document what breaks if two agents try simultaneously

2. **Green**:
   - Single agent can acquire slot, init, test, teardown, release
   - Second agent waits for slot or takes different slot

3. **Refactor**:
   - Test semaphore manually
   - Verify second agent blocks or uses different slot
   - Verify cleanup happens correctly

4. **Integration test**:
   - Spin up test stack
   - Use MCP browser to hit `host.docker.internal:{port}`
   - Verify Traefik routes correctly
   - Tear down

---

## Phase 4: Documentation

### What Will Be Achieved
All relevant documentation will be updated to reflect:
- The new `PROJECT_NAME` isolation mechanism
- Slot-based test container workflow with port assignments
- Consolidated docs (no redundant sections)

### Steps

1. **Update `/workspace/.swe-swe/test-container-workflow.md`**:
   - Add slot acquisition/release procedure
   - Add git repo initialization for temp workspace
   - Document `PROJECT_NAME` and `SWE_PORT` per slot
   - Full procedure: acquire slot → mkdir → git init → swe-swe init → set env vars → docker-compose up → MCP browser test on host.docker.internal:{port} → docker-compose down → release slot

2. **Update `/workspace/CLAUDE.md`**:
   - Reference the updated test-container-workflow.md
   - Clarify that agents should follow the workflow for MCP browser testing
   - Remove or update any references to `./scripts` if deprecated
   - Remove any redundant mentions of test containers (consolidate to one place)

3. **Update worktree CLAUDE.md** (keep in sync):
   - `/workspace/.swe-swe/worktrees/troubleshoot-test-stack/CLAUDE.md`

4. **Add inline comments in docker-compose.yml template**:
   - Brief comment explaining `swe.project` label purpose
   - Comment explaining the constraint flag on Traefik

5. **Check for other docs**:
   - Search for any other mentions of test containers, MCP browser
   - Ensure no redundant or conflicting instructions

### Verification

1. **Red**: Current docs don't mention PROJECT_NAME, slots, or ports

2. **Green**: After updates, an agent following the docs can:
   - Understand why PROJECT_NAME and slots exist
   - Follow slot-based procedure correctly
   - Use correct port for MCP browser

3. **Refactor**:
   - Review docs for clarity and completeness
   - Ensure no stale references to old approach
   - Ensure no redundant sections across docs

---

## Phase 5: Golden Tests & Integration Verification

### What Will Be Achieved
- Golden test files updated to reflect template changes
- No regressions in `swe-swe init` behavior
- Full end-to-end verification that isolation works

### Steps

1. **Run golden test update**:
   ```bash
   make build golden-update
   ```

2. **Review golden diffs**:
   ```bash
   git add -A cmd/swe-swe/testdata/golden
   git diff --cached -- cmd/swe-swe/testdata/golden
   ```
   - Verify: `swe.project` labels present
   - Verify: Prefixed router/middleware names
   - Verify: Traefik constraint flag
   - Verify: `PROJECT_NAME` in `.env`

3. **Run unit tests**:
   ```bash
   make test
   ```

4. **Integration test** (manual, follows new workflow):
   - Acquire slot 0
   - Create temp workspace: `mkdir -p /tmp/swe-swe-test-0 && cd /tmp/swe-swe-test-0 && git init && git commit --allow-empty -m "initial"`
   - `swe-swe init --project-directory=/tmp/swe-swe-test-0`
   - Edit `.env`: `PROJECT_NAME=swe-test-0`, `SWE_PORT=19770`
   - `docker-compose up -d`
   - MCP browser navigate to `http://host.docker.internal:19770`
   - Verify page loads, auth works
   - `docker-compose down`
   - Release slot

5. **Multi-instance verification** (if machine allows):
   - Spin up slot 0 and slot 1 simultaneously
   - Verify `docker ps` shows distinct `swe.project` labels
   - Verify each responds on its own port
   - Verify Traefik logs show constraint filtering
   - Tear down both

### Verification

1. **Red**: Before any changes, document current golden state

2. **Green**:
   - All unit tests pass
   - Golden files show expected changes
   - Integration test succeeds

3. **Refactor**:
   - Clean up any test artifacts
   - Ensure temp directories are removed
   - Verify no orphaned containers/networks

---

## Summary

| Phase | Key Deliverable |
|-------|-----------------|
| 1     | docker-compose.yml template with labels and constraints |
| 2     | `swe-swe init` generates PROJECT_NAME |
| 3     | Slot-based test workflow with semaphore and ports |
| 4     | Consolidated documentation |
| 5     | Golden tests updated, integration verified |
