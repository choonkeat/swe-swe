# Claude Code Instructions

## swe-swe init Changes

When making changes that affect `swe-swe init` output (templates, generated files, etc.), you must:

1. **Regenerate golden files:**
   ```bash
   make build golden-update
   ```

2. **Stage the golden file changes:**
   ```bash
   git add -A cmd/swe-swe/testdata/golden
   ```

3. **Verify the diff:** Review the staged changes to ensure they match expected output:
   ```bash
   git diff --cached -- cmd/swe-swe/testdata/golden
   ```
   The golden files should reflect your template changes across all test scenarios.
