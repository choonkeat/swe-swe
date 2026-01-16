# ADR-014: Slash commands cloning with --with-slash-commands flag

**Status**: Accepted
**Date**: 2026-01-02

## Context

Users want to equip swe-swe containers with reusable slash commands (custom prompts) that work across projects. Several AI agents support custom commands, but with different formats and directory structures.

### Agent compatibility research

| Agent | Commands Directory | Format | Custom Commands |
|-------|-------------------|--------|-----------------|
| Claude Code | `~/.claude/commands/<namespace>/` | `.md` with YAML frontmatter | Yes |
| OpenAI Codex | `~/.codex/prompts/<namespace>/` | `.md` with YAML frontmatter | Yes |
| OpenCode | `~/.config/opencode/command/<namespace>/` | `.md` with YAML frontmatter | Yes |
| Gemini CLI | `~/.gemini/commands/` | `.toml` | Yes |
| Goose | n/a (uses `.goosehints`) | n/a | No |
| Aider | n/a | n/a | No |

### Format compatibility

**Claude Code `.md` format**:
```markdown
---
description: Review code for security issues
allowed-tools: Bash(git:*)
argument-hint: [file] [severity]
---

Analyze $1 for security vulnerabilities with severity $2.
```

**Codex `.md` format**:
```markdown
---
description: Review code for security issues
argument-hint: FILE=<path> SEVERITY=<level>
---

Analyze $FILE for security vulnerabilities with severity $SEVERITY.
```

**Key finding**: Claude, Codex, and OpenCode all use `.md` files with YAML frontmatter (`---` delimiters). They share common fields (`description`, `argument-hint`) and all ignore unknown fields. A single `.md` file can work for all three agents.

**Gemini `.toml` format**:
```toml
description = "Review code for security issues"

prompt = """
Analyze the provided code for security vulnerabilities.
"""
```

Gemini requires a completely different format (`.toml` vs `.md`), making cross-compatibility impractical without maintaining parallel files.

### Subdirectory behavior

- **Claude**: `subdir/cmd.md` → `/cmd` (subdirectory shown as category in help)
- **Codex**: Subdirectory support unclear in documentation
- **Gemini**: `subdir/cmd.toml` → `/subdir:cmd` (colon-namespaced)

## Decision

Add `--with-slash-commands` flag to `swe-swe init` that:

1. **Supports Claude, Codex, and OpenCode** - All use compatible `.md` format
2. **Accepts space-separated git URLs** with optional alias prefix
3. **Clones at build time** to `/tmp/slash-commands/<alias>/`
4. **Copies at runtime** to agent home directories with correct permissions

### Flag format

```
--with-slash-commands="[alias@]<git-url> [alias@]<git-url> ..."
```

**Examples**:
```bash
# With alias - shorter namespace
--with-slash-commands=ck@https://github.com/choonkeat/slash-commands.git
# → ~/.claude/commands/ck/
# → ~/.codex/prompts/ck/
# → ~/.config/opencode/command/ck/

# Without alias - derives owner/repo from URL
--with-slash-commands=https://github.com/choonkeat/slash-commands.git
# → ~/.claude/commands/choonkeat/slash-commands/
# → ~/.codex/prompts/choonkeat/slash-commands/
# → ~/.config/opencode/command/choonkeat/slash-commands/

# Multiple repos
--with-slash-commands="ck@https://github.com/choonkeat/slash-commands.git https://github.com/org/team.git"
```

### Why not Gemini?

Gemini CLI uses `.toml` format with different structure. Supporting Gemini would require:
- Repository authors to maintain parallel `.md` and `.toml` files
- Or swe-swe to convert formats (complex, error-prone)

Decision: Leave Gemini support to future work. Repo authors who want Gemini support can include `.toml` files, but swe-swe won't clone to Gemini's directory.

### Volume mount problem

The swe-swe Dockerfile creates files at build time, but `/home/app` is mounted over by the `./home` volume at runtime:

```yaml
# docker-compose.yml
volumes:
  - ./home:/home/app  # Mounts over /home/app
```

Files created in `/home/app` during `docker build` are **hidden** when the volume mounts.

**Solution**: Two-phase approach:
1. **Dockerfile (build time)**: Clone to `/tmp/slash-commands/<alias>/`
2. **entrypoint.sh (runtime)**: Copy from `/tmp` to `/home/app` if not exists

### Runtime copy behavior

```bash
# In entrypoint.sh (runs as root before switching to app user)
if [ -d "/tmp/slash-commands/ck" ] && [ ! -d "/home/app/.claude/commands/ck" ]; then
    mkdir -p /home/app/.claude/commands
    cp -r /tmp/slash-commands/ck /home/app/.claude/commands/ck
    chown -R app:app /home/app/.claude/commands/ck
fi
```

- **Copy only if not exists**: Preserves user modifications/updates
- **chown to app:app**: Allows user to run `git pull` to update
- **Runs before su to app**: Has permission to write anywhere

### Shallow clone

Using `git clone --depth 1` for:
- Faster builds
- Smaller image size
- `git pull` works for normal fast-forward updates

Trade-off: May fail if upstream force-pushes (rare for command repos). Users can run `git fetch --unshallow` if needed.

## Consequences

**Good**:
- Users get reusable slash commands across projects
- Single repo can serve Claude, Codex, and OpenCode
- Commands persist in volume, survive container rebuilds
- Users can `git pull` inside container to update
- Alias support for shorter command prefixes (e.g., `/ck:draft-pr` vs `/choonkeat/slash-commands:draft-pr`)

**Bad**:
- Gemini not supported (different format)
- Aider and Goose not supported (no custom commands feature)
- First container startup copies files (slight delay)
- Requires network during `docker build` to clone repos
- Shallow clone may cause issues with force-pushed repos

## Alternatives considered

1. **Clone at runtime only** (in entrypoint.sh)
   - Pro: Always gets latest version
   - Con: Slower startup, requires network at runtime

2. **Clone during `swe-swe init`** (on host)
   - Pro: No network needed at container runtime
   - Con: Requires git on host machine

3. **Support Gemini with format conversion**
   - Pro: More agents supported
   - Con: Complex, error-prone, maintenance burden

4. **Embed commands in swe-swe binary**
   - Pro: No network needed
   - Con: Commands can't be customized per-user
