# swe-swe Agent Commands System

## Goal

Implement a file-based agent command system using `@` file mentions, and rename `.swe-swe/` to `swe-swe/` for better discoverability.

### Design

```
swe-swe/
  AGENTS.md              # Help, available commands, current setup
  setup                  # Command (no extension) - configure credentials, testing
  merge-this-worktree    # Command (worktree only, generated with context)
  discard-this-worktree  # Command (worktree only, generated with context)
```

### Key Decisions

- `swe-swe/` not `.swe-swe/` - visible, better `@swe` autocomplete
- No extension for commands - cleaner, more command-like
- `.md` extension for help files (AGENTS.md)
- Worktree commands generated with context baked in (branch, target, strategy)
- AGENTS.md = help + available commands + current setup state
- Terminal MOTD for discoverability
- `setup` injects pointer into root-level agent files (CLAUDE.md, AGENTS.md)

---

## Phase 1: Write ADR documenting the design

### What will be achieved
A formal Architecture Decision Record capturing the design decisions.

### Steps

1. Create ADR file `docs/adr/0022-agent-command-files.md`

2. Document the context:
   - Slash commands are agent-specific (Claude Code vs Aider vs Goose)
   - `@` file mentions work across many agents
   - Hidden `.swe-swe/` directory is less discoverable via `@` autocomplete

3. Document the decisions:
   - Use `swe-swe/` directory (visible, not hidden)
   - Commands are extensionless files (`setup`, `merge-this-worktree`)
   - Help/docs use `.md` extension (`AGENTS.md`)
   - `AGENTS.md` contains: help, available commands, current setup state
   - Worktree commands are generated with context baked in
   - Terminal MOTD for discoverability
   - `setup` injects relative path pointer into root agent files

4. Document consequences:
   - Breaking change for existing `.swe-swe/` installations
   - Better `@swe` autocomplete UX
   - Agent-agnostic command pattern

### Verification

- ADR follows existing format in `docs/adr/`
- Captures all decisions
- Numbered correctly (0022)

---

## Phase 2: Rename `.swe-swe/` to `swe-swe/`

### What will be achieved
All references to `.swe-swe/` become `swe-swe/` throughout the codebase.

### Steps

1. Audit all references:
   ```
   grep -r "\.swe-swe" --include="*.go" --include="*.md" --include="*.js" --include="*.sh"
   ```

2. Update Go source code (`cmd/swe-swe/`)
   - Constants/variables defining the directory path
   - Hardcoded paths in main.go, init logic, worktree handling

3. Update templates (`cmd/swe-swe/templates/`)
   - Host templates
   - Container templates
   - Static files (JS)

4. Update documentation
   - `docs/*.md`
   - `tasks/*.md`
   - `CLAUDE.md`
   - Existing ADRs

5. Update scripts (`scripts/`)

6. Update tests
   - Golden test expectations
   - Test code

7. Update .gitignore if needed

8. Run `make build golden-update`

9. Run `make test`

### Verification

- `grep -r "\.swe-swe"` returns only historical references
- `make test` passes
- `make build` succeeds

---

## Phase 3: Create `swe-swe/AGENTS.md` template

### What will be achieved
A template for `AGENTS.md` serving as help for users AND context for agents.

### Steps

1. Create template file `cmd/swe-swe/templates/host/swe-swe/AGENTS.md`

2. Structure the content:
   ```markdown
   # swe-swe

   This directory contains agent commands. Use `@swe-swe/<command>` to invoke.

   ## Available Commands

   | Command | Description |
   |---------|-------------|
   | `@swe-swe/setup` | Configure git, SSH, testing, credentials |
   | `@swe-swe/merge-this-worktree` | Merge this branch to target (worktree only) |
   | `@swe-swe/discard-this-worktree` | Discard this worktree (worktree only) |

   ## Current Setup

   <!-- Agent: Update this section when setup changes -->
   - Git: (not configured)
   - SSH: (not configured)
   - Testing: (not configured)

   ## For Agents

   When user mentions `@swe-swe/<command>`, read that file and follow its instructions.
   You may update the "Current Setup" section when configuration changes.
   ```

3. Update worktree copy logic to include `swe-swe/AGENTS.md`

4. Update init logic to create `swe-swe/AGENTS.md` in main workspace

### Verification

- `make build golden-update` - golden files include `swe-swe/AGENTS.md`
- File appears in `/workspace/swe-swe/` after init
- File is copied to worktree's `swe-swe/` directory

---

## Phase 4: Create `swe-swe/setup` command

### What will be achieved
A file containing instructions for agents to conversationally configure the environment.

### Steps

1. Create template file `cmd/swe-swe/templates/host/swe-swe/setup` (no extension)

2. Structure the content:
   ```
   # Setup swe-swe Environment

   Help the user configure their development environment conversationally.

   ## Tasks

   1. **Inject swe-swe pointer** (if not present)
      - Check if /workspace/CLAUDE.md or /workspace/AGENTS.md exists
      - If exists and doesn't mention swe-swe/AGENTS.md, append:
        ```
        ## swe-swe
        See `swe-swe/AGENTS.md` for available commands and setup.
        ```

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
      - Mention MCP browser at /chrome/

   5. **Update swe-swe/AGENTS.md**
      - Update the "Current Setup" section with configured values

   ## Style

   Be conversational. Ask one thing at a time. Skip what's already configured.
   ```

3. Ensure file is copied to both `/workspace/swe-swe/` and worktree `swe-swe/`

### Verification

- File exists at `swe-swe/setup` after init
- `@swe-swe/setup` is readable by agent
- Manual test: agent walks through setup conversationally
- Pointer gets injected into CLAUDE.md/AGENTS.md (relative path)

---

## Phase 5: Create worktree-specific commands (generated)

### What will be achieved
`merge-this-worktree` and `discard-this-worktree` generated at worktree creation with context baked in.

### Steps

1. Create template files with placeholders:
   - `cmd/swe-swe/templates/host/swe-swe/merge-this-worktree.tmpl`
   - `cmd/swe-swe/templates/host/swe-swe/discard-this-worktree.tmpl`

2. `merge-this-worktree.tmpl` content:
   ```
   # Merge This Worktree

   Branch: {{.BranchName}}
   Target: {{.TargetBranch}}
   Strategy: {{.MergeStrategy}}
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

   4. Perform merge based on strategy "{{.MergeStrategy}}":
      - merge-commit: `git -C /workspace checkout {{.TargetBranch}} && git merge {{.BranchName}} --no-ff`
      - merge-ff: `git -C /workspace checkout {{.TargetBranch}} && git merge {{.BranchName}}`
      - squash: `git -C /workspace checkout {{.TargetBranch}} && git merge --squash {{.BranchName}} && git commit`

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

4. Update worktree creation logic in Go code:
   - Render templates with context
   - Write to `{worktree}/swe-swe/merge-this-worktree` and `{worktree}/swe-swe/discard-this-worktree`

5. These files should NOT exist in `/workspace/swe-swe/` - only in worktrees

### Verification

- Create a new worktree session
- `swe-swe/merge-this-worktree` exists with correct branch name, target, strategy
- `swe-swe/discard-this-worktree` exists with correct values
- These files do NOT exist in main `/workspace/swe-swe/`
- `@swe-swe/merge-this-worktree` - agent understands context

---

## Phase 6: Terminal MOTD for discoverability

### What will be achieved
Print available `@swe-swe/` commands when session starts.

### Steps

1. Locate where session startup message is printed

2. Design the MOTD format:

   For main workspace:
   ```
   swe-swe: @swe-swe/setup, @swe-swe/AGENTS.md
   ```

   For worktree:
   ```
   swe-swe [fix-login-bug]: @swe-swe/merge-this-worktree, @swe-swe/discard-this-worktree, @swe-swe/AGENTS.md
   ```

3. Implementation options:
   - Option A: Print from Go when session starts
   - Option B: Add to `.bashrc`/`.zshrc` in container template
   - Option C: Create `swe-swe/motd` file, cat from shell init

4. Keep it minimal - one line, subtle styling

### Verification

- Start session in main workspace - see MOTD with `setup`, `AGENTS.md`
- Start session in worktree - see MOTD with worktree-specific commands
- MOTD doesn't interfere with terminal output

---

## Phase 7: Browser verification with test container

### What will be achieved
Verify the entire system works end-to-end.

### Steps

1. `make build`

2. Boot test container:
   - `./scripts/01-test-container-init.sh`
   - `./scripts/02-test-container-build.sh`
   - `HOST_PORT=9899 HOST_IP=host.docker.internal ./scripts/03-test-container-run.sh`

3. Test Scenario A: Main workspace session
   - Start session WITHOUT a name
   - Verify: MOTD shows `@swe-swe/setup, @swe-swe/AGENTS.md`
   - Verify: `ls swe-swe/` shows `AGENTS.md`, `setup`
   - Verify: `@swe-swe/setup` triggers conversational setup

4. Test Scenario B: Worktree session
   - Start session WITH a name (e.g., "test-feature")
   - Verify: MOTD shows worktree-specific commands
   - Verify: `ls swe-swe/` shows all files including `merge-this-worktree`, `discard-this-worktree`
   - Verify: `merge-this-worktree` has correct branch name baked in

5. Test Scenario C: Setup injects pointer
   - Create `/workspace/CLAUDE.md` if not exists
   - Run `@swe-swe/setup`
   - Verify: CLAUDE.md now contains pointer to `swe-swe/AGENTS.md`

6. Test Scenario D: No `.swe-swe/` remnants
   - Verify: `ls -la | grep swe-swe` shows `swe-swe/` (no dot)
   - Verify: No `.swe-swe/` directory exists

7. Shutdown: `./scripts/04-test-container-down.sh`

### Pass criteria

- `swe-swe/` directory exists (not `.swe-swe/`)
- MOTD displays on session start
- All command files present and readable
- Worktree commands have context baked in
- `@` mentions work for invoking commands

---

## Status

- [ ] Phase 1: Write ADR documenting the design
- [ ] Phase 2: Rename `.swe-swe/` to `swe-swe/`
- [ ] Phase 3: Create `swe-swe/AGENTS.md` template
- [ ] Phase 4: Create `swe-swe/setup` command
- [ ] Phase 5: Create worktree-specific commands (generated)
- [ ] Phase 6: Terminal MOTD for discoverability
- [ ] Phase 7: Browser verification with test container
