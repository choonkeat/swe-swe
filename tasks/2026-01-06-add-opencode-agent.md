# Add OpenCode Agent Support

**Date**: 2026-01-06
**Status**: Complete ✅
**Repository**: https://github.com/anomalyco/opencode

## Goal

Add **OpenCode** as the 6th supported AI agent in swe-swe.

## Key Details

| Attribute | Value |
|-----------|-------|
| Binary name | `opencode` |
| Install command | `npm install -g opencode-ai` |
| Resume command | `opencode --continue` |
| Node.js required | Yes (npm package) |
| MCP compatible | Yes |

**Note**: Originally planned to use the standalone curl installer, but it had issues with GitHub API rate limits during Docker builds. Switched to npm package which is more reliable.

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

## Phase 2: Dockerfile (Conditional Installation) ✅

### What Will Be Achieved

When `opencode` is in the selected agents list, the Dockerfile will include npm installation of OpenCode.

### Steps

1. **Research installer behavior**: The curl installer (`https://opencode.ai/install`) failed in Docker due to GitHub API rate limits. Switched to npm package.

2. **`cmd/swe-swe/templates/host/Dockerfile`**: Add conditional block after GOOSE section:
   ```dockerfile
   # {{IF OPENCODE}}
   # Install OpenCode CLI (https://github.com/anomalyco/opencode)
   RUN npm install -g opencode-ai
   # {{ENDIF}}
   ```

3. **`cmd/swe-swe/main.go` `processDockerfileTemplate()`**:
   - Add case for OPENCODE in switch statement
   - Add `opencode` to `needsNodeJS` check (since it now requires npm)

### Verification

| Step | Command | Expected Result |
|------|---------|-----------------|
| **Golden update** | `make build golden-update` | Dockerfile in opencode tests includes install block |
| **Verify opencode-only** | Check golden Dockerfile | Contains Node.js + OpenCode install |
| **Docker build** | `./scripts/02-test-container-build.sh` | Build succeeds |
| **Binary present** | `docker run --rm swe-swe-test:latest which opencode` | Returns `/usr/bin/opencode` |

**Regression check**: Existing agent Dockerfiles unchanged (claude, aider, etc.).

---

## Phase 3: Server Config (AssistantConfig) ✅

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

## Phase 4: Documentation (Help Text) ✅

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

## Phase 5: Integration Test (Manual Verification) ✅

### What Will Be Achieved

End-to-end verification that OpenCode works in the swe-swe container.

### Automated Tests (Complete ✅)

| Test | Result |
|------|--------|
| `make build` | ✅ Pass |
| `make golden-update` | ✅ Pass |
| `go test ./cmd/swe-swe/...` | ✅ All pass |
| `./scripts/01-test-container-init.sh` | ✅ Pass |
| `./scripts/02-test-container-build.sh` | ✅ Pass |
| `docker run ... which opencode` | ✅ `/usr/bin/opencode` |
| `docker run ... opencode --help` | ✅ Shows help |

### Manual Tests (Complete ✅)

- [x] Start container with `swe-swe up`
- [x] Verify OpenCode appears in web UI
- [x] Click OpenCode, verify TUI renders
- [x] Test session resume with `--continue`
- [x] Multi-agent test (claude + opencode)

---

## Commit Strategy

Originally planned two-commit TDD approach, but implemented in a single iteration due to:
- User had already made baseline changes before planning session
- Curl installer issue required mid-implementation pivot to npm

**Recommended commit**:
```
feat(agents): implement OpenCode support

- Add "opencode" to allAgents and help text
- Install via npm (opencode-ai package)
- Add AssistantConfig for swe-swe-server
- OpenCode requires Node.js (shares NODEJS conditional)
- Update golden tests
```

---

## Resolved Questions

1. **Installer behavior**: Curl installer failed due to GitHub API rate limits. Switched to `npm install -g opencode-ai` which installs to `/usr/bin/opencode`.

2. **Session persistence**: ✅ Works - `opencode --continue` resumes sessions correctly.

3. **First-run flow**: ✅ Works - TUI renders properly in web UI, accepts input.
