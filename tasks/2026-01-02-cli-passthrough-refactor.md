# CLI Refactor: Docker Compose Pass-through Model

## Goal

Refactor the swe-swe CLI to:
1. Replace `--path` with `--project-directory` for consistency with docker compose
2. Make all non-special commands pass through directly to docker compose
3. Remove the `--` delimiter since it's no longer needed

## Phases

- [x] [Phase 1](#phase-1-update-flag-parsing): Update flag parsing (`--path` → `--project-directory`)
- [x] [Phase 2](#phase-2-refactor-to-pass-through-model): Refactor to pass-through model
- [x] [Phase 3](#phase-3-remove-delimiter-handling): Remove `--` delimiter handling (done as part of Phase 2)
- [x] [Phase 4](#phase-4-update-documentation): Update documentation and golden tests

---

## Phase 1: Update flag parsing

### What will be achieved
All commands (`init`, `up`, `down`, `build`) will use `--project-directory` instead of `--path`. The behavior remains identical, only the flag name changes.

### Steps
1. In `handleInit()`: rename `path` flag to `project-directory`
2. In `handleUp()`: rename `path` flag to `project-directory`
3. In `handleDown()`: rename `path` flag to `project-directory`
4. In `handleBuild()`: rename `path` flag to `project-directory`
5. Update `printUsage()` to reflect the new flag name

### Verification
1. **Red**: Update golden test expected output first to use `--project-directory` - tests will fail
2. **Green**: Make the code changes - tests pass
3. **Refactor**: Clean up any redundant code
4. Run `make build golden-update` and verify the diff is only the flag rename
5. Manual smoke test: `./bin/swe-swe init --project-directory /tmp/test-project`

---

## Phase 2: Refactor to pass-through model

### What will be achieved
The main function will only handle `init`, `list`, and `-h`/`--help` flags. All other commands pass through directly to docker compose.

### Steps
1. Create a new `handlePassthrough(command string, args []string)` function that:
   - Parses `--project-directory` from args (removing it from the pass-through args)
   - Defaults to `.` if not specified
   - Resolves the metadata directory path
   - Builds docker compose command: `docker compose -f <compose-file> --project-directory <abs-path> <command> <remaining-args>`
   - Sets up environment variables (`WORKSPACE_DIR`, `SWE_SWE_PASSWORD`, filters cert vars)
   - Uses `syscall.Exec` on Unix / subprocess on Windows

2. Refactor `main()`:
   ```go
   switch command {
   case "init":
       handleInit()
   case "list":
       handleList()
   case "-h", "--help":
       printUsage()
   default:
       handlePassthrough(command, os.Args[2:])
   }
   ```

3. Remove `handleUp()`, `handleDown()`, `handleBuild()` functions

4. Remove `splitAtDoubleDash()` function

5. Remove `runDockerComposeWindows()` (fold into `handlePassthrough`)

### Verification
1. Manual tests:
   - `swe-swe up` works as before
   - `swe-swe down` works as before
   - `swe-swe build` works (now without auto `--no-cache`)
   - `swe-swe ps`, `swe-swe logs -f` work (new capabilities)
2. Golden tests still pass after Phase 1 changes

---

## Phase 3: Remove delimiter handling

### What will be achieved
Remove the `splitAtDoubleDash()` function and all related logic since pass-through model doesn't need argument separation.

### Steps
1. Delete `splitAtDoubleDash()` function (if not already removed in Phase 2)
2. In `handlePassthrough()`, implement simple `--project-directory` extraction:
   - Scan args for `--project-directory` or `--project-directory=<value>`
   - Remove it from args, use value for metadata dir lookup
   - Pass remaining args directly to docker compose
3. Ensure `handleInit()` still works with its own flag parsing (it uses `flag.NewFlagSet` so unaffected)

### Verification
1. Test commands with mixed flag positions:
   - `swe-swe up --project-directory /path`
   - `swe-swe --project-directory /path up` (if supported)
   - `swe-swe logs --project-directory /path -f swe-swe`
2. Test that arbitrary docker compose flags pass through:
   - `swe-swe up -d`
   - `swe-swe down --remove-orphans`
   - `swe-swe exec swe-swe bash`

---

## Phase 4: Update documentation

### What will be achieved
All user-facing documentation reflects the new CLI behavior: `--project-directory` flag, pass-through model, no `--` delimiter, no `help` subcommand.

### Steps
1. Update `printUsage()`:
   - Replace all `--path` references with `--project-directory`
   - Remove `help` from commands list
   - Remove `-- docker-args...` from usage pattern
   - Add examples showing pass-through commands (`ps`, `logs`, `exec`)
   - Update examples to remove `--` delimiter

2. Update any existing documentation files if they reference the old flags

3. Run `make build golden-update` to regenerate golden test files

4. Review golden diff to ensure changes are expected:
   - Flag rename `--path` → `--project-directory`
   - Help text updates

### Verification
1. `make test` passes
2. `./bin/swe-swe --help` shows updated usage
3. Golden file diff reviewed and committed
4. Manual verification of help text accuracy

---

## Design Decisions

1. **`--project-directory` over `--path`**: Matches docker compose flag name for consistency. Users don't need to remember different flags for init vs other commands.

2. **No `help` subcommand**: Only `-h` and `--help` flags. Avoids confusion since `docker compose help` exists and would show docker compose help, not swe-swe help.

3. **Pure pass-through for `build`**: No automatic `--no-cache`. Users control caching behavior explicitly. Consistent mental model for all pass-through commands.

4. **Special commands**: Only `init`, `list`, `-h`/`--help` are handled by swe-swe. Everything else goes to docker compose.
