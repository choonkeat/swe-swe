# YOLO Mode Toggle in Status Bar

**Date:** 2026-01-10
**Status:** Complete
**Branch:** `switch-to-yolo`

## Summary

Add a clickable YOLO mode toggle to the status bar. When active, "Connected" changes to "YOLO" (with border indicator). Only shown for agents that support YOLO mode.

## Agent YOLO Commands

| Agent | ShellRestartCmd | YoloRestartCmd |
|-------|-----------------|----------------|
| Claude | `claude --continue` | `claude --dangerously-skip-permissions --continue` |
| Gemini | `gemini --resume` | `gemini --resume --approval-mode=yolo` |
| Codex | `codex resume --last` | `codex --yolo resume --last` |
| Goose | `goose session -r` | `GOOSE_MODE=auto goose session -r` |
| Aider | `aider --restore-chat-history` | `aider --yes-always --restore-chat-history` |
| OpenCode | `opencode --continue` | `""` (not supported, toggle hidden) |

## UI Design

**Normal mode:**
```
┌─────────────────────────────────────────────────────────────┐
│ [●] Connected as Alice with Claude on My Project   120×40  │
└─────────────────────────────────────────────────────────────┘
      ═════════
      clickable (if agent supports YOLO)
```

**YOLO mode on:**
```
╔═════════════════════════════════════════════════════════════╗
║ [●] YOLO as Alice with Claude on My Project        120×40  ║
╚═════════════════════════════════════════════════════════════╝
      ════
      clickable, border indicates YOLO active
```

**Agent doesn't support YOLO (OpenCode):**
```
┌─────────────────────────────────────────────────────────────┐
│ [●] Connected as Alice with OpenCode on My Project 120×40  │
└─────────────────────────────────────────────────────────────┘
      (not clickable, no toggle)
```

---

## Phase 1: Data Model

### Goal
Add data structures to track YOLO mode state. No behavior changes.

### Steps

| Step | Description | File |
|------|-------------|------|
| 1.1 | Add `YoloRestartCmd string` field to `AssistantConfig` struct | main.go:~147 |
| 1.2 | Populate `YoloRestartCmd` in `assistantConfigs` array | main.go:152-189 |
| 1.3 | Add `yoloMode bool` field to `Session` struct | main.go:~192 |
| 1.4 | Add `pendingRestartCmd string` field to `Session` struct | main.go:~192 |
| 1.5 | Add `computeRestartCommand(yoloMode bool) string` method to `Session` | main.go |

### Tests

```go
func TestComputeRestartCommand_Claude_YoloOn(t *testing.T)   // expect YOLO cmd
func TestComputeRestartCommand_Claude_YoloOff(t *testing.T)  // expect normal cmd
func TestComputeRestartCommand_OpenCode_YoloOn(t *testing.T) // expect normal cmd (no YOLO support)
func TestYoloSupported(t *testing.T)                         // YoloRestartCmd != "" for supported agents
```

### Verification
- `make test` passes
- No behavior change yet

---

## Phase 2: Server API

### Goal
Handle `toggle_yolo` WebSocket message, restart agent with correct command, broadcast YOLO state.

### Steps

| Step | Description | File |
|------|-------------|------|
| 2.1 | Add `YoloMode bool` and `YoloSupported bool` to status struct in `BroadcastStatus()` | main.go:~363 |
| 2.2 | Populate `YoloSupported` from `s.AssistantConfig.YoloRestartCmd != ""` | main.go |
| 2.3 | Add `toggle_yolo` case in WebSocket message handler | main.go:~2062 |
| 2.3a | - If `YoloRestartCmd == ""`, ignore (agent doesn't support) | |
| 2.3b | - Toggle `s.yoloMode` | |
| 2.3c | - Set `s.pendingRestartCmd = computeRestartCommand(s.yoloMode)` | |
| 2.3d | - Call `BroadcastStatus()` | |
| 2.3e | - Write `[Switching YOLO mode ON/OFF, restarting agent...]` to terminal | |
| 2.3f | - Send SIGTERM to process | |
| 2.4 | Modify PTY reader restart logic: use `pendingRestartCmd` if set, then clear it | main.go:~801 |

### Tests

```go
func TestToggleYolo_SetsYoloMode(t *testing.T)       // verify yoloMode flips
func TestToggleYolo_SetsPendingCmd(t *testing.T)    // verify pendingRestartCmd set
func TestToggleYolo_UnsupportedAgent(t *testing.T)  // verify no state change for OpenCode
func TestBroadcastStatus_IncludesYolo(t *testing.T) // verify status JSON has yoloMode, yoloSupported
func TestPTYRestart_UsesPendingCmd(t *testing.T)    // verify pendingRestartCmd used and cleared
```

### Verification
- `make test` passes
- Manual: `wscat -c ws://localhost:PORT/ws/SESSION` → send `{"type":"toggle_yolo"}` → observe terminal output

---

## Phase 3: Frontend UI

### Goal
Show "YOLO" instead of "Connected" when active, make it clickable to toggle.

### Steps

| Step | Description | File |
|------|-------------|------|
| 3.1 | Add state variables `yoloMode` and `yoloSupported` | terminal-ui.js |
| 3.2 | Update status message handler to extract `yoloMode` and `yoloSupported` | terminal-ui.js |
| 3.3 | Modify `updateStatusInfo()`: show "YOLO" instead of "Connected" when `yoloMode === true` | terminal-ui.js:~1469 |
| 3.4 | Make "Connected"/"YOLO" text clickable (only if `yoloSupported`) | terminal-ui.js |
| 3.5 | Add click handler with confirmation dialog | terminal-ui.js |
| 3.6 | On confirm, send `{"type":"toggle_yolo"}` via WebSocket | terminal-ui.js |
| 3.7 | Add CSS class `.yolo` to status bar when active (border like `.multiuser`) | terminal-ui.js |
| 3.8 | Style "Connected"/"YOLO" span: underline + cursor:pointer when `yoloSupported` | terminal-ui.js |

### Tests (Manual)

| Test | Expected |
|------|----------|
| Claude session, YOLO off | "Connected" clickable, click → confirm → agent restarts → "YOLO" shown |
| Claude session, YOLO on | "YOLO" with border, click → confirm → agent restarts → "Connected" shown |
| OpenCode session | "Connected" NOT clickable |
| Cancel confirmation | No change |

### Verification
- Browser test with Claude agent
- Browser test with OpenCode agent (toggle hidden)

---

## Phase 4: Initial State Detection

### Goal
Detect YOLO mode from startup command so status reflects actual state on session join.

### Steps

| Step | Description | File |
|------|-------------|------|
| 4.1 | Add `detectYoloMode(cmd string) bool` function | main.go |
| 4.2 | Check patterns: `--dangerously-skip-permissions`, `--approval-mode=yolo`, `--yolo`, `--yes-always`, `GOOSE_MODE=auto` | main.go |
| 4.3 | In `getOrCreateSession()`, call `detectYoloMode()` on startup command | main.go:~1886 |
| 4.4 | Set `session.yoloMode` to detected value | main.go |

### Tests

```go
func TestDetectYoloMode_Claude(t *testing.T)        // "--dangerously-skip-permissions" → true
func TestDetectYoloMode_Claude_Normal(t *testing.T) // "--continue" → false
func TestDetectYoloMode_Gemini(t *testing.T)        // "--approval-mode=yolo" → true
func TestDetectYoloMode_Codex(t *testing.T)         // "--yolo" → true
func TestDetectYoloMode_Aider(t *testing.T)         // "--yes-always" → true
func TestDetectYoloMode_Goose(t *testing.T)         // "GOOSE_MODE=auto" → true
func TestDetectYoloMode_NoMatch(t *testing.T)       // "opencode --continue" → false
```

### Verification
- `make test` passes
- Start session with YOLO flag → status shows "YOLO" immediately

---

## Files Modified

| File | Changes |
|------|---------|
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | AssistantConfig, Session struct, WebSocket handler, PTY reader, BroadcastStatus, detectYoloMode |
| `cmd/swe-swe/templates/host/swe-swe-server/static/terminal-ui.js` | Status bar UI, WebSocket handler, CSS |

---

## Edge Cases

| Case | Handling |
|------|----------|
| Toggle during process startup | SIGTERM kills starting process, restart happens with new command |
| Toggle when process already dead | SIGTERM fails silently, pendingRestartCmd unused |
| Rapid toggle clicks | Last state wins, may cause multiple restarts (acceptable) |
| Agent doesn't support YOLO | Toggle not shown, `toggle_yolo` message ignored |

---

## Phase 5: Fix Process Exit Handling for YOLO Toggle

**Date:** 2026-01-11
**Status:** Planned

### Problem

When YOLO mode is toggled, the agent receives SIGTERM and may exit with code 0 (clean shutdown). The current logic treats `exitCode == 0` as "session ended" and shows a "session ended" dialog to viewers, even though we intended to restart with a new command.

**Root cause:** The `pendingRestartCmd` check happens AFTER the `exitCode == 0` check, so it's never reached when the agent exits cleanly.

**Deeper issue:** The code conflates two concepts:
1. "Restart on crash" (automatic recovery)
2. "Process replacement" (explicit user action like YOLO toggle)

### Solution

Simplify the mental model:
- **Process exit = session ends** (always, regardless of exit code)
- **Process replacement** = explicit user action that kills old process and starts new one

Remove crash-restart behavior entirely. Check for pending replacement BEFORE ending session.

---

### Phase 5.1: Remove crash-restart behavior [DONE]

#### Goal
All process exits end the session, regardless of exit code. Simplifies mental model.

#### Steps

| Step | Description | File | Status |
|------|-------------|------|--------|
| 5.1.1 | Remove the `exitCode != 0` restart branch (lines 912-943) | main.go | Done |
| 5.1.2 | Unify exit handling: all exits go through same path (save metadata, broadcast exit) | main.go | Done |
| 5.1.3 | Change exit message to include exit code for all exits | main.go | Done |
| 5.1.4 | Call `BroadcastExit(exitCode)` for all exits (not just code 0) | main.go | Done |

#### Verification
- [x] `make test` passes
- [ ] Browser test (test container): Start session → exit agent → "session ended" dialog → OK → redirects to homepage
- [ ] Manual: Kill agent with `kill -9` → should show "session ended" dialog (not restart)

---

### Phase 5.2: Add pending replacement check [DONE]

#### Goal
Before ending session, check for pending replacement. If set, start replacement process instead.

#### Steps

| Step | Description | File | Status |
|------|-------------|------|--------|
| 5.2.1 | Rename `pendingRestartCmd` to `pendingReplacement` for clarity | main.go | Done |
| 5.2.2 | Add replacement check as FIRST decision point after getting exit code | main.go | Done |
| 5.2.3 | If `pendingReplacement` set: start new process, clear flag, continue loop (no BroadcastExit) | main.go | Done |
| 5.2.4 | If `pendingReplacement` NOT set: proceed to session end | main.go | Done |
| 5.2.5 | Update `toggle_yolo` handler to use `pendingReplacement` | main.go | Done |

#### New Exit Flow

```
PTY read error → get exit code
    ↓
pendingReplacement set? ─── YES ──→ Start replacement, clear flag, continue
    │
    NO
    ↓
clientCount == 0? ─── YES ──→ End silently (no viewers)
    │
    NO
    ↓
BroadcastExit(exitCode), session ends
```

#### Verification
- [x] `make test` passes
- [ ] Manual: Toggle YOLO → agent restarts, NO "session ended" dialog
- [ ] Manual: Agent exits normally → session ends, dialog shown

---

### Phase 5.3: Clean up YOLO toggle flow [DONE]

#### Goal
Ensure YOLO toggle provides good UX. Add YOLO toggle to settings panel.

#### Steps

| Step | Description | File | Status |
|------|-------------|------|--------|
| 5.3.1 | Verify `toggle_yolo` sets `pendingReplacement` before killing process | main.go | Done (in 5.2) |
| 5.3.2 | Verify `BroadcastStatus()` called before killing (immediate status bar update) | main.go | Done (already correct) |
| 5.3.3 | Remove redundant messages for replacement path (YOLO message already shown) | main.go | Done (no redundant messages) |
| 5.3.4 | Skip 500ms sleep for replacement path (feedback already shown) | main.go | Done (no sleep in replacement) |
| 5.3.5 | Add YOLO toggle to settings panel (only if `yoloSupported`) | terminal-ui.js | Done |
| 5.3.6 | Settings toggle calls same `toggleYoloMode()` function | terminal-ui.js | Done |
| 5.3.7 | Update settings panel state when `yoloMode` changes | terminal-ui.js | Done |

#### Verification (Browser test in test container)
1. [ ] Start Claude session → open settings panel
2. [ ] Verify YOLO toggle visible in settings
3. [ ] Toggle YOLO via settings
4. [ ] Verify: Terminal shows "[Switching YOLO mode ON, restarting agent...]"
5. [ ] Verify: Status bar changes to "YOLO"
6. [ ] Verify: Settings panel reflects new state
7. [ ] Verify: NO "session ended" dialog
8. [ ] Test via status bar click (existing flow still works)

---

### Phase 5.4: Update golden files and documentation

#### Steps

| Step | Description |
|------|-------------|
| 5.4.1 | Run `make build golden-update` |
| 5.4.2 | Review golden diff - should show main.go and terminal-ui.js changes |
| 5.4.3 | Run `make test` - all tests pass |
| 5.4.4 | Update task file status to Complete |

#### Verification
- `make build` succeeds
- `make golden-update` completes
- `git diff --cached -- cmd/swe-swe/testdata/golden` shows expected changes
- `make test` passes
- Final browser test by user: full YOLO toggle flow works end-to-end

---

## Future Enhancements (Out of Scope)

- Keyboard shortcut (Ctrl+Shift+Y)
- URL parameter `?yolo=true`
- Session-level YOLO preference persistence
- Audit logging for YOLO toggles
