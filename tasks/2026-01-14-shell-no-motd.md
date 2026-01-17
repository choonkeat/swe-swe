# Task: Shell sessions should not show MOTD

**Date**: 2026-01-14
**Status**: Complete

## Problem

Shell sessions currently show MOTD with `@swe-swe/setup` instructions. Shell is not an AI agent and cannot process commands, so the MOTD is confusing and unhelpful.

## Current State

```go
// SlashCommandFormat type
const (
    SlashCmdMD   SlashCommandFormat = "md"   // Claude, Codex, OpenCode
    SlashCmdTOML SlashCommandFormat = "toml" // Gemini
    SlashCmdNone SlashCommandFormat = ""     // Goose, Aider, Shell (all lumped together)
)

// Shell config has no SlashCmdFormat, defaults to SlashCmdNone
{
    Name:     "Shell",
    Binary:   "shell",
    Homepage: false,
}

// generateMOTD logic
useSlashCmd := cfg.SlashCmdFormat != SlashCmdNone
// This treats Shell the same as Goose/Aider - showing @swe-swe/ syntax
```

## Solution

Make `SlashCmdFormat` a 3-way switch:

| Format | Agents | MOTD Behavior |
|--------|--------|---------------|
| `SlashCmdMD` | Claude, Codex, OpenCode | `/swe-swe:setup` |
| `SlashCmdTOML` | Gemini | `/swe-swe:setup` |
| `SlashCmdFile` | Goose, Aider | `@swe-swe/setup` |
| `SlashCmdNone` | Shell | No MOTD |

---

## Phase 1: Add SlashCmdFile constant ✅

### Changes

1. **Add `SlashCmdFile` constant** in `main.go`:
   ```go
   const (
       SlashCmdMD   SlashCommandFormat = "md"   // Markdown (Claude, Codex, OpenCode)
       SlashCmdTOML SlashCommandFormat = "toml" // TOML (Gemini)
       SlashCmdFile SlashCommandFormat = "file" // File mention (Goose, Aider)
       SlashCmdNone SlashCommandFormat = ""     // No commands (Shell)
   )
   ```

2. **Update Goose config**:
   ```go
   SlashCmdFormat: SlashCmdFile,  // was SlashCmdNone
   ```

3. **Update Aider config**:
   ```go
   SlashCmdFormat: SlashCmdFile,  // was SlashCmdNone
   ```

4. **Shell remains unchanged** (no SlashCmdFormat field, defaults to `SlashCmdNone`)

### Verification

- Code compiles (`make build`)

---

## Phase 2: Refactor generateMOTD to switch-case ✅

### Changes

Replace the boolean `useSlashCmd` logic with a switch statement:

```go
func generateMOTD(workDir, branchName string, cfg AssistantConfig) string {
    // Shell sessions don't need MOTD - they're not AI agents
    if cfg.SlashCmdFormat == SlashCmdNone {
        return ""
    }

    // Determine the workspace directory
    wsDir := "/workspace"
    if workDir != "" && strings.HasPrefix(workDir, worktreeDir) {
        wsDir = workDir
    }

    // Determine command invocation syntax based on agent's slash command support
    var tipText, setupCmd string
    switch cfg.SlashCmdFormat {
    case SlashCmdMD, SlashCmdTOML:
        tipText = "Tip: type /swe-swe to see commands available"
        setupCmd = "/swe-swe:setup"
    case SlashCmdFile:
        tipText = "Tip: @swe-swe to see available commands"
        setupCmd = "@swe-swe/setup"
    default:
        return "" // Safety: unknown format gets no MOTD
    }

    // Rest of function unchanged...
    // Update subsequent useSlashCmd checks to use the appropriate condition
}
```

Also update all `useSlashCmd` references in the function to check `cfg.SlashCmdFormat != SlashCmdFile` or similar as appropriate.

### Verification

- Code compiles (`make build`)
- Unit tests pass (`make test`)
- Golden update (`make build golden-update`)
- Golden diff review shows expected changes

---

## Phase 3: Manual verification (user)

### Test container verification

1. **Start test container** with OpenCode (avoids Claude auth issues):
   ```bash
   ./scripts/test-container/01-init.sh
   # During init, enable OpenCode
   ./scripts/test-container/02-build.sh
   ./scripts/test-container/03-run.sh
   ```

2. **Verify via MCP browser** at http://host.docker.internal:19770/:
   - OpenCode session: Should show `/swe-swe:setup` MOTD
   - Shell session (via status bar): Should show NO MOTD

3. **Teardown**:
   ```bash
   ./scripts/test-container/04-down.sh
   ```

### Note on agent testing

The test container installs only configured agents. OpenCode is recommended because:
- Uses `ANTHROPIC_API_KEY` from environment
- Doesn't require interactive login
- Has slash command support (`SlashCmdMD`)

To test file-mention agents (Goose/Aider), enable them during init.

---

## Files to modify

- `cmd/swe-swe/templates/host/swe-swe-server/main.go`
  - Add `SlashCmdFile` constant (line ~127)
  - Update Goose config (line ~237)
  - Update Aider config (line ~246)
  - Refactor `generateMOTD` function (line ~1403)
