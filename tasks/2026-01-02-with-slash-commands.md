# Task: Add --with-slash-commands flag to swe-swe init

## Goal

Add a `--with-slash-commands` flag to `swe-swe init` that accepts one or more git clone URLs (space-separated, with optional alias prefix) and clones each repository into the appropriate command directories for enabled agents (Claude and Codex) inside the container.

### Format
```
--with-slash-commands="[alias@]<git-url> [alias@]<git-url> ..."
```

### Examples
```bash
# Single repo without alias - derives namespace from URL
swe-swe init --with-slash-commands=https://github.com/choonkeat/slash-commands.git
# → /home/app/.claude/commands/choonkeat/slash-commands/
# → /home/app/.codex/prompts/choonkeat/slash-commands/

# Single repo with alias - uses alias as namespace
swe-swe init --with-slash-commands=ck@https://github.com/choonkeat/slash-commands.git
# → /home/app/.claude/commands/ck/
# → /home/app/.codex/prompts/ck/

# Multiple repos
swe-swe init --with-slash-commands="ck@https://github.com/choonkeat/slash-commands.git https://github.com/org/team-cmds.git"
```

---

## Phases

### Phase 1: ADR ✅

**What will be achieved**: Create `docs/adr/0014-slash-commands-cloning.md` documenting all design decisions.

**Content to include**:

1. **Context**
   - Users want reusable slash commands across swe-swe projects
   - Claude Code and Codex support custom commands via `.md` files
   - Gemini requires `.toml` format (different structure)
   - Aider and Goose don't support custom commands

2. **Agent compatibility research**

   | Agent | Directory | Format | Supported |
   |-------|-----------|--------|-----------|
   | Claude | `~/.claude/commands/<namespace>/` | `.md` | Yes |
   | Codex | `~/.codex/prompts/<namespace>/` | `.md` | Yes |
   | Gemini | `~/.gemini/commands/` | `.toml` | No (different format) |
   | Goose | n/a | n/a | No (no custom commands) |
   | Aider | n/a | n/a | No (no custom commands) |

3. **Decision: Support Claude and Codex only**
   - Both use `.md` format with compatible YAML frontmatter
   - Same repo can serve both agents

4. **Flag design**
   - Format: `--with-slash-commands="[alias@]<git-url> ..."`
   - Space-separated for multiple repos
   - Optional alias prefix for shorter command namespaces
   - Derive `owner/repo` from URL if no alias provided

5. **Volume mount problem**
   - `/home/app` is mounted over by `./home` volume at runtime
   - Files cloned in Dockerfile are hidden when volume mounts
   - Solution: Clone to `/tmp/slash-commands/<alias>/` at build time, copy to home at runtime in entrypoint.sh

6. **Runtime copy behavior**
   - Copy only if destination doesn't exist (preserves user modifications)
   - `chown -R app:app` for correct permissions
   - User can `git pull` inside container to update

7. **Shallow clone (`--depth 1`)**
   - Faster builds, smaller images
   - `git pull` works for normal updates
   - May fail on force-push (rare for command repos)

**Steps**:
1. Create `docs/adr/0014-slash-commands-cloning.md` with full content
2. Update `docs/adr/README.md` - Add entry for ADR-0014
3. Git commit: `git add docs/adr/ && git commit -m "docs: add ADR-0014 slash commands cloning"`

**Verification**:
- ADR is complete with all design decisions
- README updated with ADR-0014 entry
- Commit created

---

### Phase 2: Flag Skeleton ✅

**What will be achieved**: Add `--with-slash-commands` flag to `handleInit()` that accepts a string value but doesn't process it yet.

**Steps**:
1. Add flag definition in `handleInit()`:
   ```go
   slashCommands := fs.String("with-slash-commands", "", "Git repos to clone as slash commands (space-separated, format: [alias@]<git-url>)")
   ```
2. Add temporary print statement when flag is provided (for verification)
3. Build and smoke test
4. Run tests: `go test ./cmd/swe-swe/...`
5. Git commit: `git add -A && git commit -m "feat: add --with-slash-commands flag skeleton"`

**Verification**:
- Build succeeds: `go build ./cmd/swe-swe/...`
- Existing tests pass: `go test ./cmd/swe-swe/...`
- Flag appears in help: `./swe-swe init --help`
- Generated files unchanged (no regression)

---

### Phase 3: Golden Test Variants ✅

**What will be achieved**: Add new golden test cases that exercise `--with-slash-commands` with various agent and input combinations.

**Test cases to add** (6 total):

| Test Case | Agents | Slash Commands |
|-----------|--------|----------------|
| `with-slash-commands` | all | single with alias |
| `with-slash-commands-multi` | all | multiple (mixed alias/no-alias) |
| `with-slash-commands-claude-only` | claude | single with alias |
| `with-slash-commands-codex-only` | codex | single with alias |
| `with-slash-commands-no-alias` | all | single without alias |
| `with-slash-commands-claude-codex` | claude,codex | single with alias |

**Steps**:
1. Review existing golden test structure
2. Add test cases to `main_test.go`
3. Run golden test update: `make golden-update`
4. Verify baseline files (should match non-slash-commands equivalents for now)
5. Git commit: `git add -A && git commit -m "test: add golden test variants for --with-slash-commands"`

**Verification**:
- Golden tests pass: `go test ./cmd/swe-swe/...`
- Baseline files generated under `testdata/golden/`
- Existing golden tests unchanged

---

### Phase 4: URL Parsing ✅

**What will be achieved**: Functions to parse the `--with-slash-commands` input string and extract structured data.

**Struct definition**:
```go
type SlashCommandsRepo struct {
    Alias string // "ck" or derived "choonkeat/slash-commands"
    URL   string // "https://github.com/choonkeat/slash-commands.git"
}
```

**Functions to implement**:
- `deriveAliasFromURL(url string) (string, error)` - Extract `owner/repo` from git URL
- `parseSlashCommandsEntry(entry string) (SlashCommandsRepo, error)` - Parse single `[alias@]<url>`
- `parseSlashCommandsFlag(flag string) ([]SlashCommandsRepo, error)` - Parse full flag value

**Test cases**:

| Input | Expected Alias | Expected URL |
|-------|---------------|--------------|
| `ck@https://github.com/choonkeat/slash-commands.git` | `ck` | `https://github.com/choonkeat/slash-commands.git` |
| `https://github.com/choonkeat/slash-commands.git` | `choonkeat/slash-commands` | `https://github.com/choonkeat/slash-commands.git` |
| `https://github.com/choonkeat/slash-commands` | `choonkeat/slash-commands` | `https://github.com/choonkeat/slash-commands` |
| `https://gitlab.com/org/repo.git` | `org/repo` | `https://gitlab.com/org/repo.git` |
| `not-a-url` | error | error |
| `""` | error | error |

**Steps**:
1. Add struct definition
2. Add `deriveAliasFromURL` helper
3. Add `parseSlashCommandsEntry` function
4. Add `parseSlashCommandsFlag` function
5. Add unit tests in `main_test.go`
6. Run tests: `go test ./cmd/swe-swe/...`
7. Git commit: `git add -A && git commit -m "feat: add slash commands URL parsing"`

**Verification**:
- Unit tests pass: `go test ./cmd/swe-swe/... -run TestSlashCommands`
- All tests pass: `go test ./cmd/swe-swe/...`
- Golden tests unchanged (parsing not wired yet)

---

### Phase 5: Dockerfile Template + Wiring ✅

**What will be achieved**: Add `git clone` commands to Dockerfile template and wire parsed flag data to template processor.

**Dockerfile template addition** (`templates/host/Dockerfile`):
```dockerfile
# {{IF SLASH_COMMANDS}}
# Clone slash commands repositories to temp location
# (will be copied to home directory by entrypoint.sh)
{{SLASH_COMMANDS_CLONE}}
# {{ENDIF}}
```

**Generated content example**:
```dockerfile
RUN git clone --depth 1 https://github.com/choonkeat/slash-commands.git /tmp/slash-commands/ck
RUN git clone --depth 1 https://github.com/org/team-cmds.git /tmp/slash-commands/org/team-cmds
```

**Steps**:
1. Update `processDockerfileTemplate` signature to accept `slashCommands []SlashCommandsRepo`
2. Add conditional section to Dockerfile template
3. Generate `SLASH_COMMANDS_CLONE` content in processor
4. Wire flag to processor in `handleInit()`
5. Run golden update: `make golden-update`
6. Verify golden Dockerfile diffs
7. Run tests: `go test ./cmd/swe-swe/...`
8. Git commit: `git add -A && git commit -m "feat: add git clone for slash commands to Dockerfile"`

**Verification**:
- Tests pass: `go test ./cmd/swe-swe/...`
- Golden diffs show `git clone` lines in `with-slash-commands*/Dockerfile`
- Other golden files unchanged
- Commit created

---

### Phase 6: Entrypoint Template + Wiring ✅

**What will be achieved**: Add copy + chown logic to entrypoint.sh that copies cloned repos to agent directories with correct ownership.

**Entrypoint template addition** (`templates/host/entrypoint.sh`), before `exec su` line:
```bash
# {{IF SLASH_COMMANDS}}
# Copy slash commands to agent directories
{{SLASH_COMMANDS_COPY}}
# {{ENDIF}}
```

**Generated content example**:
```bash
# Claude
if [ -d "/tmp/slash-commands/ck" ] && [ ! -d "/home/app/.claude/commands/ck" ]; then
    mkdir -p /home/app/.claude/commands
    cp -r /tmp/slash-commands/ck /home/app/.claude/commands/ck
    chown -R app:app /home/app/.claude/commands/ck
fi
# Codex
if [ -d "/tmp/slash-commands/ck" ] && [ ! -d "/home/app/.codex/prompts/ck" ]; then
    mkdir -p /home/app/.codex/prompts
    cp -r /tmp/slash-commands/ck /home/app/.codex/prompts/ck
    chown -R app:app /home/app/.codex/prompts/ck
fi
```

**Steps**:
1. Update entrypoint template processor to handle slash commands
2. Add conditional section to entrypoint template
3. Generate `SLASH_COMMANDS_COPY` content (per repo, per enabled agent)
4. Wire to template processor
5. Run golden update: `make golden-update`
6. Verify golden entrypoint.sh diffs
7. Run tests: `go test ./cmd/swe-swe/...`
8. Git commit: `git add -A && git commit -m "feat: add slash commands copy to entrypoint.sh"`

**Verification**:
- Tests pass: `go test ./cmd/swe-swe/...`
- Golden diffs show copy + chown blocks in `with-slash-commands*/entrypoint.sh`
- `with-slash-commands-claude-only` copies only to `.claude/commands/`
- `with-slash-commands-codex-only` copies only to `.codex/prompts/`
- Commit created

---

### Phase 7: Documentation

**What will be achieved**: Update all user-facing documentation.

**Steps**:
1. Update `printUsage()` in `main.go`:
   ```
   --with-slash-commands REPOS        Git repos to clone as slash commands (space-separated)
                                      Format: [alias@]<git-url>
   ```
   Add example to Examples section

2. Update `README.md`:
   - Add to Options section (around line 66):
     ```markdown
     - `--with-slash-commands REPOS`: Git repos to clone as slash commands (space-separated, format: [alias@]<git-url>)
     ```
   - Add example:
     ```bash
     # Initialize with custom slash commands for Claude/Codex
     swe-swe init --path ~/my-project --with-slash-commands=ck@https://github.com/choonkeat/slash-commands.git
     ```

3. Run tests: `go test ./cmd/swe-swe/...`
4. Manual verification: `go build ./cmd/swe-swe && ./swe-swe help`
5. Git commit: `git add -A && git commit -m "docs: add --with-slash-commands to help and README"`

**Verification**:
- Tests pass
- Help output shows new flag with format and examples
- README updated with new option and example
- Commit created

---

## Summary

| Phase | Description | Commit Message |
|-------|-------------|----------------|
| 1 | ADR | `docs: add ADR-0014 slash commands cloning` |
| 2 | Flag skeleton | `feat: add --with-slash-commands flag skeleton` |
| 3 | Golden test variants | `test: add golden test variants for --with-slash-commands` |
| 4 | URL parsing | `feat: add slash commands URL parsing` |
| 5 | Dockerfile + wiring | `feat: add git clone for slash commands to Dockerfile` |
| 6 | Entrypoint + wiring | `feat: add slash commands copy to entrypoint.sh` |
| 7 | Documentation | `docs: add --with-slash-commands to help and README` |
