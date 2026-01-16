# Fix Goose CLI Commands

**Date**: 2026-01-06
**Status**: In Progress

## Background

The `ShellRestartCmd` field in `AssistantConfig` allows assistants to have different commands for starting fresh vs resuming a session. Investigation revealed:

- Current code uses `goose` for both `ShellCmd` and `ShellRestartCmd` (redundant)
- Research doc incorrectly shows `goose run --interactive --text ready` and `goose run --resume`
- Official Goose documentation shows correct commands:
  - Start session: `goose session`
  - Resume session: `goose session -r`

**Sources**:
- https://block.github.io/goose/docs/guides/sessions/session-management/
- https://block.github.io/goose/docs/quickstart/

## Goal

Fix the Goose CLI commands to use the correct `goose session` and `goose session -r` syntax.

---

## Phase 1: Update the template code ✅

### What will be achieved
The Goose assistant configuration in `cmd/swe-swe/templates/host/swe-swe-server/main.go` will use the correct CLI commands.

### Steps
1. Edit line 127: Change `ShellCmd: "goose"` to `ShellCmd: "goose session"`
2. Edit line 128: Change `ShellRestartCmd: "goose"` to `ShellRestartCmd: "goose session -r"`

### Verification
- Manual inspection of the change
- Golden file propagation in Phase 3 will confirm correctness

---

## Phase 2: Update the research documentation

### What will be achieved
The research document will have its example code snippet corrected to match the actual implementation.

### Steps
1. Edit `research/2025-12-24-aider-goose-missing-from-assistant-list.md` line 40
2. Change from: `{Name: "Goose", ShellCmd: "goose run --interactive --text ready", ShellRestartCmd: "goose run --resume", Binary: "goose"},`
3. Change to: `{Name: "Goose", ShellCmd: "goose session", ShellRestartCmd: "goose session -r", Binary: "goose"},`

### Verification
- Manual inspection of the change
- Documentation file has no downstream dependencies

---

## Phase 3: Regenerate golden test files

### What will be achieved
All golden test files will be updated to reflect the Goose command changes.

### Steps
1. Run `make build` to rebuild swe-swe binary
2. Run `make golden-update` to regenerate golden files
3. Stage: `git add -A cmd/swe-swe/testdata/golden`
4. Review: `git diff --cached -- cmd/swe-swe/testdata/golden`
5. Verify diff shows ONLY Goose ShellCmd/ShellRestartCmd changes

### Verification
- Diff should show exactly 2 line changes per golden main.go file
- ~25 golden files affected (one per test variant)
- Run `go test ./...` to ensure all tests pass

---

## Expected Changes Summary

| File | Change |
|------|--------|
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | Lines 127-128: Goose commands |
| `research/2025-12-24-aider-goose-missing-from-assistant-list.md` | Line 40: Example snippet |
| `cmd/swe-swe/testdata/golden/*/swe-swe-server/main.go` | ~25 files: Goose commands |
