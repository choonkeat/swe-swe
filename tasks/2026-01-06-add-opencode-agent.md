# Add OpenCode Agent Support

**Date**: 2026-01-06
**Status**: In Progress
**Repository**: https://github.com/anomalyco/opencode

## Goal

Add **OpenCode** as the 6th supported AI agent in swe-swe, using the standalone binary installer with no Node.js dependency.

## Key Details

| Attribute | Value |
|-----------|-------|
| Binary name | `opencode` |
| Install command | `curl -fsSL https://opencode.ai/install \| bash` |
| Resume command | `opencode --continue` |
| Node.js required | No (standalone Go binary) |
| MCP compatible | Yes |

---

## Phase 1: Baseline (Flag Parsing + Golden Tests) ✅

### What Will Be Achieved

OpenCode will be recognized as a valid agent name in `--agents` and `--exclude-agents` flags, stored in `init.json`, but **not yet installed or configured** in the container. This establishes the test baseline before functional changes.

### Steps

1. **`cmd/swe-swe/main.go` line 305**: Add `"opencode"` to `allAgents` slice
   ```go
   var allAgents = []string{"claude", "gemini", "codex", "aider", "goose", "opencode"}
   ```

2. **`cmd/swe-swe/main.go` `printUsage()`**: Add `opencode` to the Available Agents list and flag description

3. **`cmd/swe-swe/main_test.go`**: Add test variants for opencode:
   - `TestInit_AgentsOpencode` - test `--agents=opencode`
   - `TestInit_ExcludeOpencode` - test `--exclude-agents=opencode`

### Verification

| Step | Command | Expected Result |
|------|---------|-----------------|
| **Red** | `go test ./cmd/swe-swe -run Opencode` | Tests fail (don't exist yet) |
| **Green** | Add code + tests, run `make build golden-update` | Tests pass, golden files created |
| **Verify** | `git diff --cached -- cmd/swe-swe/testdata/golden` | Only `init.json` shows `opencode`, Dockerfile unchanged |

**Regression check**: `go test ./cmd/swe-swe/...` — all existing tests pass.

### Commit

```
feat(agents): add opencode to agent list (baseline)

- Add "opencode" to allAgents slice
- Update help text to list opencode as available agent
- Add golden test variants for --agents=opencode
- No functional changes yet (Dockerfile, server unchanged)
```

---

## Phase 2: Dockerfile (Conditional Installation)

### What Will Be Achieved

When `opencode` is in the selected agents list, the Dockerfile will include the curl installer to download and install the OpenCode binary.

### Steps

1. **Research installer behavior**: Fetch and analyze `https://opencode.ai/install` to determine:
   - Where binary is installed (PATH location?)
   - Environment variable overrides (e.g., `INSTALL_DIR`)
   - Non-interactive/TTY compatibility

2. **`cmd/swe-swe/templates/host/Dockerfile`**: Add conditional block after GOOSE section (~line 91):
   ```dockerfile
   # {{IF OPENCODE}}
   # Install OpenCode CLI (https://github.com/anomalyco/opencode)
   RUN curl -fsSL https://opencode.ai/install | bash
   # {{ENDIF}}
   ```
   *(Adjust based on installer research if needed)*

3. **`cmd/swe-swe/main.go` `processDockerfileTemplate()`**: Add case for OPENCODE (~line 571):
   ```go
   case "OPENCODE":
       skip = !hasAgent("opencode")
   ```

### Verification

| Step | Command | Expected Result |
|------|---------|-----------------|
| **Golden update** | `make build golden-update` | Dockerfile in opencode tests includes install block |
| **Verify isolation** | Check `testdata/golden/agents-opencode/Dockerfile` | Contains ONLY opencode install, no Node.js/Python |
| **Verify default** | Check `testdata/golden/default/Dockerfile` | Now includes opencode install |

**Regression check**: Existing agent Dockerfiles unchanged (claude, aider, etc.).

---

## Phase 3: Server Config (AssistantConfig)

### What Will Be Achieved

The swe-swe-server will recognize OpenCode as an available assistant, enabling users to select it from the web UI.

### Steps

1. **`cmd/swe-swe/templates/host/swe-swe-server/main.go`**: Add to `assistantConfigs` slice (after Aider, ~line 137):
   ```go
   {
       Name:            "OpenCode",
       ShellCmd:        "opencode",
       ShellRestartCmd: "opencode --continue",
       Binary:          "opencode",
   },
   ```

### Verification

| Check | Method | Expected Result |
|-------|--------|-----------------|
| **Syntax** | `make build` | Compiles without error |
| **Golden update** | `make golden-update` | swe-swe-server/main.go updated in golden dirs |

---

## Phase 4: Documentation (Help Text)

### What Will Be Achieved

All user-facing help text includes OpenCode as an available agent.

### Steps

1. **`cmd/swe-swe/main.go` `printUsage()`** (~line 268-269): Update Available Agents:
   ```
   Available Agents:
     claude, gemini, codex, aider, goose, opencode
   ```

2. **`cmd/swe-swe/main.go` `printUsage()`** (~line 258): Update `--agents` flag description:
   ```
   --agents AGENTS                        Comma-separated agents: claude,gemini,codex,aider,goose,opencode (default: all)
   ```

3. **Add example** (optional):
   ```
   swe-swe init --agents=opencode              Initialize with OpenCode only
   ```

### Verification

```bash
./dist/swe-swe --help | grep opencode
# Should appear in agents list and flag description
```

---

## Phase 5: Integration Test (Manual Verification)

### What Will Be Achieved

End-to-end verification that OpenCode works in the swe-swe container.

### Steps

1. **Build and init**:
   ```bash
   make build
   ./dist/swe-swe init --project-directory /tmp/test-opencode --agents=opencode
   ```

2. **Verify Dockerfile**:
   ```bash
   cat ~/.swe-swe/projects/tmp-test-opencode-*/Dockerfile | grep -A2 "Install OpenCode"
   ```

3. **Build and start**:
   ```bash
   cd /tmp/test-opencode && swe-swe build && swe-swe up
   ```

4. **Verify detection**: Open `http://localhost:9898`, confirm OpenCode appears in UI

5. **Spawn session**: Click OpenCode, verify TUI renders, accepts input

6. **Test resume**: Exit session, verify `--continue` works

7. **Multi-agent test**:
   ```bash
   ./dist/swe-swe init --project-directory /tmp/test-multi --agents=claude,opencode
   # Verify both work
   ```

8. **Cleanup**:
   ```bash
   swe-swe down --project-directory /tmp/test-opencode
   swe-swe down --project-directory /tmp/test-multi
   rm -rf /tmp/test-opencode /tmp/test-multi
   ```

### Verification Checklist

| Test | Pass Criteria |
|------|---------------|
| Dockerfile generation | Contains `curl -fsSL https://opencode.ai/install` |
| Container build | No errors |
| Binary installed | `docker exec <container> which opencode` returns path |
| Agent detection | OpenCode appears in web UI |
| Session spawn | TUI renders, accepts input |
| Session resume | `--continue` restores session |
| Multi-agent | OpenCode + other agents work together |
| Unit tests | `go test ./cmd/swe-swe/...` all pass |

---

## Commit Strategy

Following CLAUDE.md two-commit TDD approach:

1. **Commit 1 (Baseline)**: Phase 1 only
   - Flag parsing, test variants
   - Golden files show `init.json` changes only

2. **Commit 2 (Implementation)**: Phases 2-4
   - Dockerfile, server config, documentation
   - Golden files show functional changes

---

## Open Questions

1. **Installer behavior**: Where does `opencode.ai/install` put the binary? Need to verify during Phase 2.

2. **Session persistence**: Does `opencode --continue` work across container restarts? (SQLite DB location)

3. **First-run flow**: Does OpenCode have an interactive setup that might break in PTY?
