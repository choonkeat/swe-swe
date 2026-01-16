# Claude Code Instructions

## `swe-swe init` Changes

Always run `make build golden-update` after modifying templates or generated files, then verify:
```bash
git add -A cmd/swe-swe/testdata/golden
git diff --cached -- cmd/swe-swe/testdata/golden
```

### Adding new flags

Use a two-commit TDD approach:

1. **Baseline**: Add flag parsing (no effect yet) + golden test variants
   - Add flag to command parsing and `InitConfig` struct
   - Add test variants in `cmd/swe-swe/main_test.go`
   - Run `make build golden-update`, commit (shows flag in init.json only)

2. **Implementation**: Make flag take effect
   - Implement functionality (template changes, etc.)
   - Run `make build golden-update`, verify diff shows only functional changes, commit
