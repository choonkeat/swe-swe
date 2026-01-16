# Add `--copy-home-paths` Flag to `swe-swe init`

## Goal

Add a `--copy-home-paths` flag to `swe-swe init` that copies specified directories/files from the user's `$HOME` into the project's `home` directory (`~/.swe-swe/projects/{project}/home/`), preserving directory structure.

Example usage:
```bash
swe-swe init --copy-home-paths .ssh,.claude/commands
```

Result:
- `$HOME/.ssh` → `~/.swe-swe/projects/{project}/home/.ssh`
- `$HOME/.claude/commands` → `~/.swe-swe/projects/{project}/home/.claude/commands`

---

## Phase 1 - Baseline: Flag Parsing + Config Storage ✅ COMPLETE

### What Will Be Achieved

The `--copy-home-paths` flag is recognized, validated, stored in `InitConfig`, and wired into `--previous-init-flags=reuse`. The flag does NOT yet copy files.

### Steps

1. ✅ **Add flag definition** in `handleInit()`:
   ```go
   copyHomePathsFlag := fs.String("copy-home-paths", "", "Comma-separated paths relative to $HOME to copy into container home")
   ```

2. ✅ **Parse into slice** (split by comma, trim whitespace)

3. ✅ **Validate each path**:
   - Reject if starts with `/`
   - Reject if contains `..`

4. ✅ **Add field to `InitConfig` struct**:
   ```go
   CopyHomePaths []string `json:"copyHomePaths,omitempty"`
   ```

5. ✅ **Wire into reuse logic** - restore `CopyHomePaths` from saved config

6. ✅ **Add golden test variant** that uses the flag

### Verification

1. ✅ `make build golden-update`
2. ✅ Golden diff shows:
   - New test variant directory `with-copy-home-paths` exists
   - `init.json` contains `"copyHomePaths": [".gitconfig", ".ssh"]`
3. ✅ `make test` passes

---

## Phase 2 - Implementation: Make Flag Take Effect ✅ COMPLETE

### What Will Be Achieved

Files are actually copied from `$HOME/<path>` to `{sweDir}/home/<path>`.

**Note:** Config storage and reuse wiring were completed in Phase 1.

### Steps

1. ✅ **Implement the copy logic**:
   - For each path in `CopyHomePaths`:
     - Source: `$HOME/<path>`
     - Dest: `{sweDir}/home/<path>`
     - Check source exists (warn and skip if not)
     - Create parent directories at destination (`mkdir -p`)
     - Use recursive copy with preserved permissions

   Added `copyDir()` function for recursive directory copy and updated `copyFile()` to preserve permissions.

### Verification

1. ✅ `make build golden-update`
2. ✅ Golden diff shows minimal changes (copy happens at runtime, golden test HOME is temp dir)
3. ✅ `make test` passes
4. ✅ Manual test verified:
   ```bash
   swe-swe init --copy-home-paths .gitconfig,.ssh
   # Output: "Copied /home/.gitconfig → .../home/.gitconfig"
   # Output: "Copied /home/.ssh → .../home/.ssh"
   ```

---

## Phase 3 - Refactor: Reuse Validation to Allowlist

### What Will Be Achieved

Change `--previous-init-flags=reuse` validation from blocklist (what cannot be combined) to allowlist (what can be combined). This is safer - forgetting to update an allowlist fails safely, forgetting to update a blocklist allows invalid combinations.

### Steps

1. **Identify current blocklist check** (~line 840):
   ```go
   // Current: blocklist - breaks if we forget new flag
   if *agentsFlag != "" || *excludeFlag != "" || ... {
       // error
   }
   ```

2. **Refactor to allowlist**:
   ```go
   // New: allowlist - safe default if we forget new flag
   // Only --project-directory and --previous-init-flags are allowed with reuse
   hasOtherFlags := false
   fs.Visit(func(f *flag.Flag) {
       if f.Name != "project-directory" && f.Name != "previous-init-flags" {
           hasOtherFlags = true
       }
   })
   if hasOtherFlags {
       // error
   }
   ```

### Verification

1. `make build golden-update`
2. Golden should be unchanged (same behavior, different implementation)
3. `make test` passes
4. Manual test: verify error still shown when combining `--previous-init-flags=reuse` with other flags
