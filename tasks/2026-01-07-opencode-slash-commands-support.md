# Task: Add OpenCode Support to --with-slash-commands

**Date**: 2026-01-07
**Status**: In Progress

## Background

Research revealed that OpenCode uses the same `.md` + YAML frontmatter format as Claude and Codex for custom commands:

| Agent | Directory | Format | Compatible |
|-------|-----------|--------|------------|
| Claude | `~/.claude/commands/` | `.md` + YAML | Yes |
| Codex | `~/.codex/prompts/` | `.md` + YAML | Yes |
| OpenCode | `~/.config/opencode/command/` | `.md` + YAML | Yes |
| Gemini | `~/.gemini/commands/` | `.toml` | No |

ADR-0014 originally only covered Claude and Codex. This task extends `--with-slash-commands` to also support OpenCode.

## Goal

Extend the `--with-slash-commands` flag to copy cloned slash command repos to OpenCode's directory (`~/.config/opencode/command/`) when OpenCode is among the enabled agents.

---

## Phase 1: Update ADR-0014 Documentation ✅

### What will be achieved
ADR-0014 (`docs/adr/0014-slash-commands-cloning.md`) will be updated to include OpenCode in the compatibility table and document its directory/format.

### Steps
1. Read current ADR-0014 to understand its structure
2. Add OpenCode row to the "Agent compatibility research" table (line ~12-18)
3. Add note about OpenCode's different directory path (`~/.config/opencode/command/`)
4. Update "Decision" section to mention OpenCode alongside Claude and Codex
5. Update "Consequences" section if needed

### Verification
- Manual review of the ADR changes
- No code changes, so no regression risk
- Documentation-only commit

---

## Phase 2: Add Golden Test Variants for OpenCode

### What will be achieved
New golden test cases that exercise `--with-slash-commands` with OpenCode enabled, ensuring the feature works correctly when OpenCode is among the selected agents.

### Steps
1. Review existing slash commands test variants in `cmd/swe-swe/main_test.go`
2. Add new test case: `with-slash-commands-opencode-only` (opencode agent + slash commands)
3. Add new test case: `with-slash-commands-claude-opencode` (claude,opencode agents + slash commands)
4. Run `make build golden-update` to generate baseline golden files
5. Verify baseline files are generated (should match current behavior - no OpenCode copy logic yet)

### Verification
- `go test ./cmd/swe-swe/...` passes
- New golden directories created under `testdata/golden/`
- Existing golden tests unchanged (no regression)
- At this point, entrypoint.sh in new goldens will NOT have OpenCode copy logic (that comes in Phase 3)

---

## Phase 3: Update Entrypoint Template to Copy to OpenCode Directory

### What will be achieved
The entrypoint.sh template will be updated to copy cloned slash command repos to OpenCode's directory (`~/.config/opencode/command/`) when OpenCode is an enabled agent.

### Steps
1. Read current entrypoint template (`cmd/swe-swe/templates/host/entrypoint.sh`) to understand the existing Claude/Codex copy logic
2. Read the template processing code in `cmd/swe-swe/main.go` to understand how agent-specific copy blocks are generated
3. Add OpenCode to the list of agents that receive slash command copies
4. Use correct path: `/home/app/.config/opencode/command/<alias>/` (not `/home/app/.opencode/...`)
5. Ensure the copy block follows same pattern:
   ```bash
   # OpenCode
   if [ -d "/tmp/slash-commands/<alias>" ] && [ ! -d "/home/app/.config/opencode/command/<alias>" ]; then
       mkdir -p /home/app/.config/opencode/command
       cp -r /tmp/slash-commands/<alias> /home/app/.config/opencode/command/<alias>
       chown -R app:app /home/app/.config/opencode/command/<alias>
   fi
   ```
6. Run `make build` to verify it compiles

### Verification
- Build succeeds: `make build`
- Unit tests pass: `go test ./cmd/swe-swe/...`
- Manual inspection of generated entrypoint logic

---

## Phase 4: Run `make build golden-update` and Verify Diffs

### What will be achieved
All golden test files will be regenerated to reflect the OpenCode slash command copy logic, and we verify the changes are correct and limited to expected files.

### Steps
1. Run `make build golden-update`
2. Stage golden files: `git add -A cmd/swe-swe/testdata/golden`
3. Review diffs: `git diff --cached -- cmd/swe-swe/testdata/golden`
4. Verify the diff shows:
   - New OpenCode copy blocks in `with-slash-commands-opencode-only/entrypoint.sh`
   - New OpenCode copy blocks in `with-slash-commands-claude-opencode/entrypoint.sh`
   - OpenCode copy blocks added to existing `with-slash-commands*/entrypoint.sh` files (for tests that include all agents)
5. Verify NO unexpected changes to other golden files
6. Run `go test ./cmd/swe-swe/...` to confirm all tests pass

### Verification
- All tests pass: `go test ./cmd/swe-swe/...`
- Diff shows ONLY OpenCode-related additions to entrypoint.sh files
- No changes to Dockerfile, docker-compose.yml, or other templates
- Existing Claude/Codex copy blocks unchanged (no regression)

---

## Phase 5: Update README and Help Text

### What will be achieved
User-facing documentation will be updated to reflect that `--with-slash-commands` now supports OpenCode in addition to Claude and Codex.

### Steps
1. Update `README.md`:
   - Find the `--with-slash-commands` documentation section
   - Update description to mention OpenCode alongside Claude/Codex
   - Example: "Git repos to clone as slash commands for Claude, Codex, and OpenCode"
2. Update `printUsage()` in `cmd/swe-swe/main.go`:
   - Find the `--with-slash-commands` help text
   - Update to mention OpenCode support
3. Run `make build` to rebuild with updated help
4. Verify help output: `./dist/swe-swe init --help`
5. Run `go test ./cmd/swe-swe/...` to ensure no regression

### Verification
- Build succeeds: `make build`
- All tests pass: `go test ./cmd/swe-swe/...`
- Help output shows OpenCode mentioned for `--with-slash-commands`
- README accurately describes the feature

---

## Summary

| Phase | Description | Key Files |
|-------|-------------|-----------|
| 1 | Update ADR-0014 | `docs/adr/0014-slash-commands-cloning.md` |
| 2 | Add golden test variants | `cmd/swe-swe/main_test.go` |
| 3 | Update entrypoint template | `cmd/swe-swe/main.go` |
| 4 | Golden update and verify | `cmd/swe-swe/testdata/golden/` |
| 5 | Update docs | `README.md`, `cmd/swe-swe/main.go` |
