# Claude Code Instructions

## Testing

Always use `make test` (not `go test` directly) to run tests. The Makefile ensures consistent test execution.

## `swe-swe init` Changes

Always run `make build golden-update` after modifying templates or generated files, then verify:
```bash
git add -A cmd/swe-swe/testdata/golden
git diff --cached -- cmd/swe-swe/testdata/golden
```

**Note**: Always use `make golden-update` (not individual `_golden-variant` targets). The Makefile manages a temporary symlink that only exists during the full run.

### Adding new flags

Use a two-commit TDD approach:

1. **Baseline**: Add flag parsing (no effect yet) + golden test variants
   - Add flag to command parsing and `InitConfig` struct
   - Add test variants in `cmd/swe-swe/main_test.go`
   - Run `make build golden-update`, commit (shows flag in init.json only)

2. **Implementation**: Make flag take effect
   - Implement functionality (template changes, etc.)
   - Run `make build golden-update`, verify diff shows only functional changes, commit

## swe-swe Directory Convention

Inside the container's `/workspace/`:

- **`swe-swe/`** — Agent commands only. All files here are `@`-mentionable.
- **`.swe-swe/`** — Internal. Only subdirectories (no loose files): `docs/`, `uploads/`, etc.

## Browser / Manual testing

Agent will
1. boot up test container with docs/dev/test-container-workflow.md
2. use mcp browser to test
3. shutdown the test container