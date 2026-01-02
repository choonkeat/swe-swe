# Init Config Storage and Reuse

## Goal

Add `--previous-init-flags` flag to `swe-swe init` that:
1. Stores init configuration in metadata directory after successful init
2. Detects when project is already initialized and errors with helpful message
3. Supports `--previous-init-flags=reuse` to reapply stored config
4. Supports `--previous-init-flags=ignore` to allow fresh init with new flags
5. Errors when `--previous-init-flags=reuse` is combined with other flags

## Phases

- [x] [Phase 1](#phase-1-parse-flag-and-add-golden-scenarios): Parse `--previous-init-flags` flag + add golden test scenarios
- [ ] [Phase 2](#phase-2-store-init-configuration): Store init configuration after successful init
- [ ] [Phase 3](#phase-3-detect-already-initialized): Detect already-initialized projects and error
- [ ] [Phase 4](#phase-4-implement-reuse): Implement `--previous-init-flags=reuse`
- [ ] [Phase 5](#phase-5-implement-ignore): Implement `--previous-init-flags=ignore`
- [ ] [Phase 6](#phase-6-validate-reuse-isolation): Validate `--previous-init-flags=reuse` cannot combine with other flags

---

## Phase 1: Parse flag and add golden scenarios

### What will be achieved
Add `--previous-init-flags` flag to init command (accepts "reuse" or "ignore", no behavior yet). Update golden infrastructure to capture stderr. Create golden test scenarios to establish baseline.

### Steps
1. Update `_golden-variant` in Makefile to capture stderr:
   ```makefile
   @HOME=/tmp/swe-swe-golden/$(NAME)/home $(SWE_SWE_CLI) init $(FLAGS) \
       --project-directory /tmp/swe-swe-golden/$(NAME)/target \
       2> $(GOLDEN_TESTDATA)/$(NAME)/stderr.txt || true
   ```

2. Add `--previous-init-flags` flag to `handleInit()` with validation (must be empty, "reuse", or "ignore")

3. Update `printUsage()` to document the new flag

4. Add golden test scenarios to Makefile:
   - `previous-init-flags-reuse` - with `--previous-init-flags=reuse`
   - `previous-init-flags-ignore` - with `--previous-init-flags=ignore`
   - `previous-init-flags-ignore-claude` - with `--previous-init-flags=ignore --agents=claude`

5. Run `make build golden-update`

6. Verify golden diff shows new directories + stderr.txt files

### Verification
1. `make test` passes
2. `git diff` on golden files shows new directories and stderr.txt added
3. New flag is documented in `--help` output
4. Invalid values like `--previous-init-flags=invalid` error out

---

## Phase 2: Store init configuration

### What will be achieved
1. Define `InitConfig` struct and use it throughout `handleInit()` to ensure sync
2. Save config to `init.json` after successful init
3. Add regression tests to detect breaking changes to `InitConfig` schema

### Steps
1. Define `InitConfig` struct with JSON tags:
   ```go
   type InitConfig struct {
       Agents        []string            `json:"agents"`
       AptPackages   string              `json:"aptPackages"`
       NpmPackages   string              `json:"npmPackages"`
       WithDocker    bool                `json:"withDocker"`
       SlashCommands []SlashCommandsRepo `json:"slashCommands"`
   }
   ```

2. Refactor `handleInit()` to:
   - Parse flags into `InitConfig` struct early
   - Pass `InitConfig` to helper functions instead of individual params

3. Add `saveInitConfig(sweDir string, config InitConfig) error` function

4. Add `loadInitConfig(sweDir string) (InitConfig, error)` function

5. Add unit tests in `main_test.go`:
   - `TestInitConfigRoundTrip` - marshal/unmarshal preserves all fields
   - `TestInitConfigBackwardsCompatibility` - with prominent comment:
   ```go
   // TestInitConfigBackwardsCompatibility ensures we can load init.json
   // files created by older versions of swe-swe.
   //
   // ⚠️  IF THIS TEST FAILS AFTER YOUR CHANGES:
   //     DO NOT edit the JSON fixture below to make the test pass.
   //     Instead, fix your code to remain compatible with existing init.json files.
   //     Users have real projects with these files - breaking compatibility
   //     means their `swe-swe init --previous-init-flags=reuse` will fail.
   //
   // If you need to add new fields:
   //     - Add them with zero-value defaults (omitempty or default handling)
   //     - Old init.json files without the field should still work
   //
   func TestInitConfigBackwardsCompatibility(t *testing.T) {
       // JSON fixture representing v1 format (DO NOT MODIFY)
       const v1JSON = `{...}`
       ...
   }
   ```

6. Call `saveInitConfig()` at end of successful `handleInit()`

7. Run `make build golden-update`

8. Verify golden diff shows `init.json` with correct content

### Verification
1. `make test` passes including new unit tests
2. Golden files include `init.json`
3. Different scenarios have different `init.json` content
4. Changing a JSON field name would break `TestInitConfigBackwardsCompatibility`

---

## Phase 3: Detect already-initialized

### What will be achieved
When running `swe-swe init` on an already-initialized project (without `--previous-init-flags` flag), error out with a helpful message.

### Steps
1. At start of `handleInit()`, after resolving path, check if `init.json` exists in metadata dir

2. If `init.json` exists and `--previous-init-flags` flag is empty:
   - Print error to stderr:
     ```
     Error: Project already initialized at /path/to/project

       To reapply saved configuration:
         swe-swe init --previous-init-flags=reuse

       To overwrite with new configuration:
         swe-swe init --previous-init-flags=ignore [options]
     ```
   - Exit with code 1

3. Add golden test scenario:
   - `already-initialized` - runs init twice on same project, captures error

4. Update `_golden-variant` to support multi-step scenarios (or add separate target)

5. Run `make build golden-update`

6. Verify `stderr.txt` contains expected error message

### Verification
1. `make test` passes
2. Golden `already-initialized/stderr.txt` shows error message
3. Manual test: run init twice, second time errors

---

## Phase 4: Implement reuse

### What will be achieved
When `--previous-init-flags=reuse` is specified, load config from `init.json` and reapply it.

### Steps
1. In `handleInit()`, when `--previous-init-flags=reuse`:
   - Check if `init.json` exists, error if not: "No saved configuration to reuse"
   - Call `loadInitConfig()` to load stored config
   - Use loaded config instead of parsed flags
   - Proceed with normal init flow (regenerate all files)

2. Update golden test scenario `previous-init-flags-reuse`:
   - First init with `--agents=claude --with-docker`
   - Second init with `--previous-init-flags=reuse`
   - Verify output files match first init

3. Add golden scenario `previous-init-flags-reuse-no-config`:
   - Run `--previous-init-flags=reuse` on fresh project (no prior init)
   - Verify stderr shows "No saved configuration to reuse"

4. Run `make build golden-update`

5. Verify golden diffs show expected behavior

### Verification
1. `make test` passes
2. `previous-init-flags-reuse` golden files match the original config
3. `previous-init-flags-reuse-no-config/stderr.txt` shows appropriate error
4. Manual test: init with flags, then `--previous-init-flags=reuse` produces same files

---

## Phase 5: Implement ignore

### What will be achieved
When `--previous-init-flags=ignore` is specified on an already-initialized project, ignore stored config and init fresh with provided flags.

### Steps
1. In `handleInit()`, when `--previous-init-flags=ignore`:
   - Skip the "already initialized" check
   - Proceed with normal init flow using provided flags
   - Save new config to `init.json` (replaces old)

2. Update golden scenario `previous-init-flags-ignore`:
   - First init with `--agents=claude`
   - Second init with `--previous-init-flags=ignore --agents=aider`
   - Verify final files reflect aider config

3. Run `make build golden-update`

4. Verify golden diffs show new config

### Verification
1. `make test` passes
2. `previous-init-flags-ignore` golden shows aider config
3. `init.json` reflects the new flags

---

## Phase 6: Validate reuse isolation

### What will be achieved
When `--previous-init-flags=reuse` is used with any other init flags, error out immediately.

### Steps
1. In `handleInit()`, after parsing flags, if `--previous-init-flags=reuse`:
   - Check if any other init flags were explicitly set
   - If so, error: `"--previous-init-flags=reuse cannot be combined with other flags"`
   - Exit with code 1

2. Add golden scenario `previous-init-flags-reuse-with-other-flags`:
   - Run `swe-swe init --previous-init-flags=reuse --agents=claude`
   - Verify stderr shows error message

3. Run `make build golden-update`

4. Verify `stderr.txt` contains expected error

### Verification
1. `make test` passes
2. Golden `previous-init-flags-reuse-with-other-flags/stderr.txt` shows error
3. Manual test: `swe-swe init --previous-init-flags=reuse --with-docker` errors

---

## Summary Table

| Command | Already Initialized | Result |
|---------|---------------------|--------|
| `swe-swe init` | No | ✅ Works, saves config |
| `swe-swe init` | Yes | ❌ Error with helpful message |
| `swe-swe init --previous-init-flags=reuse` | No | ❌ Error: no config to reuse |
| `swe-swe init --previous-init-flags=reuse` | Yes | ✅ Reapply stored config |
| `swe-swe init --previous-init-flags=reuse --agents=X` | Any | ❌ Error: cannot combine |
| `swe-swe init --previous-init-flags=ignore` | No | ✅ Works (same as no flag) |
| `swe-swe init --previous-init-flags=ignore --agents=X` | Yes | ✅ Fresh init with new flags |
