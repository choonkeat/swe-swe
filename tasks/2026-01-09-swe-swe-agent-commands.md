# swe-swe Agent Commands System

## Goal

Implement a file-based agent command system using `@` file mentions.

### Directory Convention

Two directories with distinct purposes:

```
swe-swe/                   # Commands ONLY (all @-mentionable, clean autocomplete)
  setup                    # Command - configure credentials, testing
  merge-this-worktree      # Command (worktree only, generated with context)
  discard-this-worktree    # Command (worktree only, generated with context)

.swe-swe/                  # Internal (only subdirectories, no loose files)
  docs/                    # Documentation for agents
    AGENTS.md              # Index - explains swe-swe, lists commands, current setup
    browser-automation.md
    docker.md
  uploads/                 # File uploads
```

### What exists after `swe-swe init`

**Main workspace (`/workspace/`):**
```
swe-swe/
  setup                    # Only command available in main workspace

.swe-swe/
  docs/
    AGENTS.md
    browser-automation.md
    docker.md
```

Note: `merge-this-worktree` and `discard-this-worktree` do NOT exist in main workspace.

### What exists after worktree creation

**Worktree (`/worktrees/<branch>/`):**
```
swe-swe/
  setup                    # Copied from main workspace
  merge-this-worktree      # Generated with branch/target context baked in
  discard-this-worktree    # Generated with branch context baked in

.swe-swe/
  docs/                    # Copied from main workspace
    AGENTS.md
    browser-automation.md
    docker.md
```

### Key Decisions

- `swe-swe/` = commands only, clean `@swe-swe/` autocomplete
- `.swe-swe/` = internal, only subdirectories (no loose files)
- `AGENTS.md` lives in `.swe-swe/docs/` (not `swe-swe/`) to avoid autocomplete pollution
- `setup` command injects pointer into user's `CLAUDE.md`/`AGENTS.md` pointing to `.swe-swe/docs/AGENTS.md`
- No extension for commands - cleaner, more command-like
- Worktree commands generated with context baked in (branch, target)
- Terminal MOTD for discoverability

### Template Directory Structure

```
cmd/swe-swe/templates/
  container/           # init-time → copied to /workspace/
    .mcp.json
    swe-swe/
      setup
    .swe-swe/
      docs/
        AGENTS.md
        browser-automation.md
        docker.md

  host/                # init-time → docker setup, server code
    Dockerfile
    docker-compose.yml
    swe-swe-server/
      main.go
      ...

  worktree/            # worktree-creation-time → /worktrees/<branch>/
    swe-swe/
      merge-this-worktree.tmpl
      discard-this-worktree.tmpl
```

The directory name documents when templates are used:
- `container/` = `swe-swe init`
- `host/` = `swe-swe init` (docker/server setup)
- `worktree/` = worktree creation (runtime, by swe-swe-server)

### Container Template Files

Files in `templates/container/` are NOT automatically copied. They must be listed in `containerFiles` slice in `main.go:1136`:

```go
containerFiles := []string{
    "templates/container/.mcp.json",
    "templates/container/.swe-swe/docs/AGENTS.md",
    "templates/container/.swe-swe/docs/browser-automation.md",
    "templates/container/swe-swe/setup",
}
if *withDocker {
    containerFiles = append(containerFiles, "templates/container/.swe-swe/docs/docker.md")
}
```

When adding new container template files: update both the template AND the `containerFiles` list.

### Worktree Copy Policy

When creating a worktree, `copyUntrackedFiles` copies:
- Untracked dotfiles (except `.git`, `.swe-swe`, `swe-swe`)
- Untracked `CLAUDE.md`, `AGENTS.md`
- Tracked files handled by git worktree itself

Additionally for swe-swe:
- `.swe-swe/docs/` copied to worktree's `.swe-swe/docs/`
- `swe-swe/setup` copied from main workspace
- `swe-swe/merge-this-worktree` and `discard-this-worktree` generated fresh with context

---

## Phase 1: Update ADR with finalized design

### What will be achieved
Update ADR-0022 to reflect the two-directory convention.

### Steps

1. Update `docs/adr/0022-simplified-worktree-exit.md`:
   - Document two-directory convention
   - `swe-swe/` for commands only, `.swe-swe/` for internal data
   - `.swe-swe/` contains only subdirectories (no loose files)
   - `AGENTS.md` in `.swe-swe/docs/`, not `swe-swe/`

### Verification

- ADR reflects final design decisions

---

## Phase 2: Restructure `.swe-swe/` to use subdirectories

### What will be achieved
Move `.swe-swe/*.md` files into `.swe-swe/docs/` subdirectory.

### Steps

1. Move template files:
   - `templates/container/.swe-swe/browser-automation.md` → `templates/container/.swe-swe/docs/browser-automation.md`
   - `templates/container/.swe-swe/docker.md` → `templates/container/.swe-swe/docs/docker.md`

2. Create `templates/container/.swe-swe/docs/AGENTS.md`:
   ```markdown
   # swe-swe

   ## Commands

   Use `@swe-swe/<command>` to invoke:

   | Command | Description | Where |
   |---------|-------------|-------|
   | `setup` | Configure git, SSH, testing, credentials | All sessions |
   | `merge-this-worktree` | Merge this branch to target | Worktree only |
   | `discard-this-worktree` | Discard this worktree | Worktree only |

   ## Current Setup

   <!-- Agent: Update this section when setup changes -->
   - Git: (not configured)
   - SSH: (not configured)
   - Testing: (not configured)

   ## Documentation

   - `browser-automation.md` - MCP browser at /chrome/
   - `docker.md` - Docker access from container

   ## For Agents

   When user mentions `@swe-swe/<command>`, read that file and follow its instructions.
   You may update the "Current Setup" section when configuration changes.
   ```

3. Update `copySweSweMarkdownFiles` in `main.go`:
   - Change to copy `.swe-swe/docs/` directory instead of `.swe-swe/*.md`
   - Rename function to `copySweSweDocsDir` or similar

4. Update any references to `.swe-swe/*.md` paths in:
   - Documentation
   - CLAUDE.md (browser/manual testing section)
   - Other templates

5. Run `make build golden-update && make test`

### Verification

- `.swe-swe/` contains only subdirectories
- `.swe-swe/docs/` contains `AGENTS.md`, `browser-automation.md`, `docker.md`
- Worktree copy still works

---

## Phase 3: Create `swe-swe/` directory with `setup` command

### What will be achieved
A new `swe-swe/` directory containing only the `setup` command.

### Steps

1. Create template directory `cmd/swe-swe/templates/container/swe-swe/`

2. Create `cmd/swe-swe/templates/container/swe-swe/setup` (no extension):
   ```
   # Setup swe-swe Environment

   Help the user configure their development environment conversationally.

   ## Tasks

   1. **Inject swe-swe pointer** (if not present)
      - Check if /workspace/CLAUDE.md or /workspace/AGENTS.md exists
      - If exists and doesn't mention .swe-swe/docs/AGENTS.md, append:
        ```
        ## swe-swe
        See `.swe-swe/docs/AGENTS.md` for commands and setup.
        ```
      - If neither exists, create /workspace/CLAUDE.md with the pointer

   2. **Git identity**
      - Check `git config user.name` and `git config user.email`
      - If not set, ask user and configure
      - Ask about GPG signing preference

   3. **SSH keys**
      - Check if ~/.ssh/id_* exists
      - If not, offer to generate
      - Help add to ssh-agent
      - Provide public key for GitHub/GitLab

   4. **Testing setup**
      - Ask: what command starts your dev server?
      - Ask: what port does it run on?
      - Document how to access via host.docker.internal
      - See `.swe-swe/docs/browser-automation.md` for MCP browser details

   5. **Update .swe-swe/docs/AGENTS.md**
      - Update the "Current Setup" section with configured values

   ## Style

   Be conversational. Ask one thing at a time. Skip what's already configured.
   ```

3. Update `excludeFromCopy` to include `"swe-swe"`:
   ```go
   var excludeFromCopy = []string{".git", ".swe-swe", "swe-swe"}
   ```

4. Run `make build golden-update && make test`

### Verification

- `swe-swe/setup` exists in container after init
- `swe-swe/` contains ONLY `setup` (no AGENTS.md, no worktree commands)
- `swe-swe/` not copied to worktrees (handled separately)
- Golden tests updated

---

## Phase 4: Create worktree-specific commands (generated)

### What will be achieved
`merge-this-worktree` and `discard-this-worktree` generated at worktree creation with context baked in.

### Steps

1. Create template files with placeholders:
   - `cmd/swe-swe/templates/worktree/swe-swe/merge-this-worktree.tmpl`
   - `cmd/swe-swe/templates/worktree/swe-swe/discard-this-worktree.tmpl`

2. `merge-this-worktree.tmpl` content:
   ```
   # Merge This Worktree

   Branch: {{.BranchName}}
   Target: {{.TargetBranch}}
   Worktree: {{.WorktreePath}}
   Main repo: /workspace

   ## Instructions

   Help the user merge this worktree branch to the target branch.

   1. Check for uncommitted changes: `git status`
      - If dirty, ask user: commit, stash, or abort?

   2. Fetch latest: `git -C /workspace fetch origin`

   3. Check if target has moved:
      - `git -C /workspace log {{.TargetBranch}}..origin/{{.TargetBranch}} --oneline`
      - If commits exist, ask if user wants to update target first

   4. Perform merge:
      - `git -C /workspace checkout {{.TargetBranch}} && git merge {{.BranchName}}`
      - Ask user about merge strategy (ff, no-ff, squash) if they have preference

   5. If conflict:
      - Show conflicted files
      - Help resolve conversationally
      - Don't leave user in broken state

   6. After successful merge:
      - `git -C /workspace worktree remove {{.WorktreePath}}`
      - `git -C /workspace branch -d {{.BranchName}}`
      - Confirm cleanup complete
   ```

3. `discard-this-worktree.tmpl` content:
   ```
   # Discard This Worktree

   Branch: {{.BranchName}}
   Worktree: {{.WorktreePath}}
   Main repo: /workspace

   ## Instructions

   Help the user discard this worktree and its branch.

   1. Check for uncommitted changes: `git status`
      - If dirty, WARN user: "You have uncommitted changes that will be lost"
      - List the files
      - Ask for explicit confirmation

   2. Check for unpushed commits:
      - `git log origin/{{.BranchName}}..{{.BranchName}} --oneline 2>/dev/null || git log --oneline`
      - If unpushed commits exist, WARN user
      - Ask for explicit confirmation

   3. After confirmation:
      - `git -C /workspace worktree remove --force {{.WorktreePath}}`
      - `git -C /workspace branch -D {{.BranchName}}`
      - Confirm cleanup complete
   ```

4. Update worktree creation logic in `main.go`:
   - After creating worktree, create `swe-swe/` directory in worktree
   - Copy `setup` from `/workspace/swe-swe/`
   - Render and write `merge-this-worktree` and `discard-this-worktree` templates

5. These files should NOT exist in `/workspace/swe-swe/` - only in worktrees

### Verification

- Create a new worktree session
- Worktree `swe-swe/` contains: `setup`, `merge-this-worktree`, `discard-this-worktree`
- Main `/workspace/swe-swe/` contains only: `setup`
- `merge-this-worktree` has correct branch name, target baked in

---

## Phase 5: Terminal MOTD for discoverability

### What will be achieved
Print available `@swe-swe/` commands when session starts.

### Steps

1. Locate where session startup message is printed

2. Design the MOTD format:

   For main workspace:
   ```
   swe-swe: @swe-swe/setup
   ```

   For worktree:
   ```
   swe-swe [fix-login-bug]: @swe-swe/merge-this-worktree, @swe-swe/discard-this-worktree
   ```

3. Implementation options:
   - Option A: Print from Go when session starts
   - Option B: Add to `.bashrc`/`.zshrc` in container template
   - Option C: Create `.swe-swe/motd` file, cat from shell init

4. Keep it minimal - one line, subtle styling

### Verification

- Start session in main workspace - see MOTD with `setup`
- Start session in worktree - see MOTD with worktree-specific commands
- MOTD doesn't interfere with terminal output

---

## Phase 6: Browser verification with test container

### What will be achieved
Verify the entire system works end-to-end.

### Steps

1. `make build`

2. Boot test container:
   - `./scripts/01-test-container-init.sh`
   - `./scripts/02-test-container-build.sh`
   - `HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh`

3. Test Scenario A: Main workspace after init
   - Verify: `ls swe-swe/` shows only `setup`
   - Verify: `ls .swe-swe/` shows only `docs/`
   - Verify: `ls .swe-swe/docs/` shows `AGENTS.md`, `browser-automation.md`, `docker.md`

4. Test Scenario B: Main workspace session
   - Start session WITHOUT a name
   - Verify: MOTD shows `@swe-swe/setup`
   - Verify: `@swe-swe/setup` triggers conversational setup
   - Verify: Setup injects pointer into CLAUDE.md

5. Test Scenario C: Worktree session
   - Start session WITH a name (e.g., "test-feature")
   - Verify: MOTD shows worktree-specific commands
   - Verify: `ls swe-swe/` shows `setup`, `merge-this-worktree`, `discard-this-worktree`
   - Verify: `merge-this-worktree` has correct branch name baked in
   - Verify: `.swe-swe/docs/` copied to worktree

6. Test Scenario D: Pointer injection
   - After running `@swe-swe/setup`
   - Verify: `/workspace/CLAUDE.md` contains pointer to `.swe-swe/docs/AGENTS.md`

7. Shutdown: `./scripts/04-test-container-down.sh`

### Pass criteria

- `swe-swe/` contains ONLY commands (no AGENTS.md)
- `.swe-swe/` contains only subdirectories (`docs/`)
- Main workspace: `swe-swe/setup` only
- Worktree: `swe-swe/setup` + generated worktree commands
- MOTD displays on session start
- Pointer injection works

---

## Status

- [ ] Phase 1: Update ADR with finalized design
- [ ] Phase 2: Restructure `.swe-swe/` to use subdirectories
- [ ] Phase 3: Create `swe-swe/` directory with `setup` command
- [ ] Phase 4: Create worktree-specific commands (generated)
- [ ] Phase 5: Terminal MOTD for discoverability
- [ ] Phase 6: Browser verification with test container
