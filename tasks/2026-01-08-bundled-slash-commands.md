# Bundled Slash Commands for swe-swe

## Goal

Enable swe-swe to ship its own slash commands that are automatically installed into `~/.claude/commands/swe-swe/` during `swe-swe init`, without requiring users to specify `--with-slash-commands`.

Initial implementation ships a placeholder `README.adoc` (using `.adoc` extension to avoid being picked up by agents as a slash command). Actual commands can be added later.

## Design

- Files embedded in binary via `go:embed`
- Written to `./home/.claude/commands/swe-swe/` during `swe-swe init`
- Existing `./home:/home/app` volume mount makes them available in container
- No entrypoint.sh changes needed
- `swe-swe` namespace is reserved - error if user tries `--with-slash-commands=swe-swe@...`

---

## Phase 1: Embed placeholder in binary

### What will be achieved
The swe-swe binary will contain an embedded `slash-commands/swe-swe/README.adoc` file that can be extracted during init.

### Steps

- [x] Create directory `cmd/swe-swe/slash-commands/swe-swe/`
- [x] Create `README.adoc` with brief explanation of what this directory is for
- [x] Add `//go:embed slash-commands/*` directive in `main.go` (or separate `embed.go` file)
- [x] Expose embedded filesystem as a package-level variable

### Verification

- `make build` succeeds (no compilation errors from embed directive)
- Existing golden tests pass unchanged (`make golden-test`)

---

## Phase 2: Write bundled commands during init

### What will be achieved
During `swe-swe init`, embedded slash command files are extracted and written to `./home/.claude/commands/swe-swe/`.

### Steps

- [x] Add unit test `TestParseSlashCommandsEntry` case for `swe-swe@https://...` expecting error (TDD red)
- [x] Add validation in `parseSlashCommandsEntry()` - return error if alias equals `swe-swe` (TDD green)
- [x] Add unit test `TestWriteBundledSlashCommands` (TDD red)
- [x] Add function `writeBundledSlashCommands(destDir string) error` that:
  - Iterates over embedded filesystem
  - Creates `destDir/swe-swe/` directory
  - Writes each file (preserving subdirectory structure if any)
- [x] Implement function (TDD green)
- [x] Call function in `handleInit()` after creating `home/` directory
- [x] Destination: `filepath.Join(hostDir, "home", ".claude", "commands", "swe-swe")`

### Verification

- Unit tests pass
- Existing golden tests pass (`make golden-test`)
- Manual: `swe-swe init` on test project produces `./home/.claude/commands/swe-swe/README.adoc`

---

## Phase 3: Golden tests

### What will be achieved
Golden tests verify bundled slash commands are correctly written during init.

### Steps

- [x] Run `make build golden-update` to regenerate expected outputs
- [x] Verify diff shows `home/.claude/commands/swe-swe/README.adoc` added to all variants
- [x] Commit golden test updates

### Verification

- `git diff --cached -- cmd/swe-swe/testdata/golden` shows only new README.adoc in each variant
- `make golden-test` passes
- No unintended changes to other golden files

---

## Status

- [x] Phase 1: Embed placeholder in binary
- [x] Phase 2: Write bundled commands during init
- [x] Phase 3: Golden tests
