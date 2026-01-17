# Proposal: YOLO Mode Toggle in Status Bar

**Date:** 2026-01-09
**Status:** Draft
**Author:** Research handoff from Claude

## Summary

Add a clickable "YOLO: ON/OFF" toggle in the browser status bar that allows users to switch agents in and out of YOLO mode (auto-approve/skip-permissions mode) without navigating away from the terminal session.

## Background

### Problem

Users want to switch between supervised mode and YOLO mode mid-session. Currently, the only way to do this with Claude Code is to:
1. Exit the session
2. Run `claude --dangerously-skip-permissions --continue` to enter YOLO mode
3. Or run `claude --continue` to exit YOLO mode

This causes the swe-swe frontend to treat the exit as session termination, redirecting to homepage.

### Goal

Allow YOLO mode toggling where:
- The agent process exits and restarts with different flags
- The swe-swe frontend does NOT redirect to homepage
- The xterm continues flowing seamlessly
- User sees only a brief restart message

## Research Findings

### Agent YOLO Mode Flags

| Agent | Continue Flag | YOLO Mode Flag | Combined Command |
|-------|---------------|----------------|------------------|
| **Claude** | `--continue` | `--dangerously-skip-permissions` | `claude --dangerously-skip-permissions --continue` |
| **Gemini** | `--resume` | `--approval-mode=yolo` | `gemini --resume --approval-mode=yolo` |
| **Codex** | `resume --last` | `--yolo` | `codex --yolo resume --last` |
| **Aider** | `--restore-chat-history` | `--yes-always` | `aider --yes-always --restore-chat-history` |
| **Goose** | `session -r` | N/A (uses env var) | `GOOSE_MODE=auto goose session -r` |
| **OpenCode** | `--continue` | Unknown | TBD - needs research |

### Current Server Architecture

**File:** `cmd/swe-swe/templates/host/swe-swe-server/main.go`

#### Process Exit Flow (lines 765-812)

```
PTY read EOF → cmd.Wait() → exitCode
  │
  ├─ exitCode == 0 → BroadcastExit() → frontend redirects to homepage
  │
  └─ exitCode != 0 → RestartProcess(ShellRestartCmd) → xterm continues
```

Key insight: **Non-zero exits already restart without BroadcastExit!**

#### Relevant Code Locations

| Component | Location | Purpose |
|-----------|----------|---------|
| `AssistantConfig` struct | Line 77-82 | Defines ShellCmd, ShellRestartCmd per agent |
| `assistantConfigs` array | Line 147-180 | Hardcoded agent configurations |
| `Session` struct | Line 183-208 | Session state including Cmd, PTY |
| `RestartProcess()` | Line 673-714 | Restarts process with given command |
| `startPTYReader()` | Line 716-825 | PTY read loop, handles exit/restart |
| Exit code 0 handling | Line 765-787 | Calls `BroadcastExit(0)` |
| Non-zero exit handling | Line 790-812 | Calls `RestartProcess()` |
| WebSocket handler | Line 1952-2197 | Handles client messages |
| JSON message switch | Line 2062-2127 | `ping`, `chat`, `rename_session` types |

#### AssistantConfig Structure (current)

```go
type AssistantConfig struct {
    Name            string
    ShellCmd        string  // e.g., "claude"
    ShellRestartCmd string  // e.g., "claude --continue"
    Binary          string  // e.g., "claude"
}
```

## Proposed Implementation

### Phase 1: Server Changes

#### 1.1 Extend Session Struct

Add fields to track YOLO state and pending restart command:

```go
// In Session struct (around line 183)
type Session struct {
    // ... existing fields ...

    // YOLO mode state
    yoloMode          bool   // Current YOLO mode state
    pendingRestartCmd string // If set, use this instead of ShellRestartCmd on next restart
}
```

#### 1.2 Add YOLO Command Builder

Add a method to compute the appropriate command based on agent and YOLO state:

```go
// Add after RestartProcess() around line 714
func (s *Session) computeRestartCommand(yoloMode bool) string {
    switch s.AssistantConfig.Name {
    case "Claude":
        if yoloMode {
            return "claude --dangerously-skip-permissions --continue"
        }
        return "claude --continue"
    case "Gemini":
        if yoloMode {
            return "gemini --resume --approval-mode=yolo"
        }
        return "gemini --resume"
    case "Codex":
        if yoloMode {
            return "codex --yolo resume --last"
        }
        return "codex resume --last"
    case "Aider":
        if yoloMode {
            return "aider --yes-always --restore-chat-history"
        }
        return "aider --restore-chat-history"
    case "Goose":
        // Goose uses environment variable, handled separately
        if yoloMode {
            return "GOOSE_MODE=auto goose session -r"
        }
        return "goose session -r"
    case "OpenCode":
        // TODO: Research OpenCode YOLO flag
        return "opencode --continue"
    default:
        return s.AssistantConfig.ShellRestartCmd
    }
}
```

#### 1.3 Add WebSocket Message Handler

Add `toggle_yolo` message type in the JSON message switch (around line 2062):

```go
case "toggle_yolo":
    s.mu.Lock()
    newYoloMode := !s.yoloMode
    s.yoloMode = newYoloMode
    s.pendingRestartCmd = s.computeRestartCommand(newYoloMode)
    cmd := s.Cmd
    s.mu.Unlock()

    log.Printf("Session %s: toggling YOLO mode to %v", s.UUID, newYoloMode)

    // Broadcast status update with new YOLO state
    s.BroadcastStatus()

    // Send visual feedback to terminal
    modeStr := "OFF"
    if newYoloMode {
        modeStr = "ON"
    }
    msg := []byte(fmt.Sprintf("\r\n[Switching YOLO mode %s, restarting agent...]\r\n", modeStr))
    s.vtMu.Lock()
    s.vt.Write(msg)
    s.writeToRing(msg)
    s.vtMu.Unlock()
    s.Broadcast(msg)

    // Kill process - will exit non-zero, triggering restart with pendingRestartCmd
    if cmd != nil && cmd.Process != nil {
        cmd.Process.Signal(syscall.SIGTERM)
    }
```

#### 1.4 Modify PTY Reader Restart Logic

Modify the restart logic in `startPTYReader()` (around line 801):

```go
// Replace:
// if err := s.RestartProcess(s.AssistantConfig.ShellRestartCmd); err != nil {

// With:
s.mu.Lock()
restartCmd := s.AssistantConfig.ShellRestartCmd
if s.pendingRestartCmd != "" {
    restartCmd = s.pendingRestartCmd
    s.pendingRestartCmd = "" // Clear after use
}
s.mu.Unlock()

if err := s.RestartProcess(restartCmd); err != nil {
```

#### 1.5 Extend BroadcastStatus

Add YOLO mode to the status broadcast (around line 363):

```go
// In BroadcastStatus(), add to the status struct:
type statusMsg struct {
    Type        string `json:"type"`
    Viewers     int    `json:"viewers"`
    Cols        int    `json:"cols"`
    Rows        int    `json:"rows"`
    Assistant   string `json:"assistant"`
    SessionName string `json:"sessionName"`
    YoloMode    bool   `json:"yoloMode"`  // ADD THIS
}
```

### Phase 2: Frontend Changes

#### 2.1 Status Bar Component

Add YOLO toggle to the status bar (location TBD based on current UI):

```typescript
// Pseudocode - adapt to actual frontend framework
interface StatusBarProps {
    yoloMode: boolean;
    onToggleYolo: () => void;
}

function YoloToggle({ yoloMode, onToggleYolo }: StatusBarProps) {
    const handleClick = () => {
        const action = yoloMode ? "disable" : "enable";
        if (confirm(`${action.charAt(0).toUpperCase() + action.slice(1)} YOLO mode? The agent will restart.`)) {
            onToggleYolo();
        }
    };

    return (
        <button
            onClick={handleClick}
            className={yoloMode ? "yolo-on" : "yolo-off"}
        >
            YOLO: {yoloMode ? "ON" : "OFF"}
        </button>
    );
}
```

#### 2.2 WebSocket Message Sender

```typescript
function toggleYoloMode(ws: WebSocket) {
    ws.send(JSON.stringify({ type: "toggle_yolo" }));
}
```

#### 2.3 Status Message Handler

Update the status message handler to track YOLO state:

```typescript
// In WebSocket message handler
case "status":
    setViewerCount(msg.viewers);
    setAssistant(msg.assistant);
    setYoloMode(msg.yoloMode);  // ADD THIS
    break;
```

### Phase 3: Initial YOLO State

#### 3.1 Detect Initial State from Command

When session starts, detect if YOLO mode was enabled:

```go
func detectYoloMode(cmd string) bool {
    // Check for YOLO flags in the startup command
    yoloPatterns := []string{
        "--dangerously-skip-permissions",
        "--approval-mode=yolo",
        "--yolo",
        "--yes-always",
        "GOOSE_MODE=auto",
    }
    for _, pattern := range yoloPatterns {
        if strings.Contains(cmd, pattern) {
            return true
        }
    }
    return false
}
```

#### 3.2 Set Initial State on Session Creation

In `getOrCreateSession()` (around line 1886):

```go
// After creating session, set initial YOLO state
session.yoloMode = detectYoloMode(cmdStr)
```

## User Experience Flow

1. User is in a session with YOLO mode OFF
2. User clicks "YOLO: OFF" in status bar
3. Browser shows: `confirm("Enable YOLO mode? The agent will restart.")`
4. User clicks OK
5. Frontend sends `{type: "toggle_yolo"}` via WebSocket
6. Server:
   - Sets `pendingRestartCmd` to YOLO-enabled command
   - Sends `[Switching YOLO mode ON, restarting agent...]` to terminal
   - Sends SIGTERM to process
7. Process exits with non-zero code
8. PTY reader detects exit, uses `pendingRestartCmd` instead of default
9. New process starts with `--dangerously-skip-permissions --continue`
10. User sees agent restart in terminal, continues with YOLO mode ON
11. Status bar now shows "YOLO: ON"

## Edge Cases

### 1. Toggle During Process Startup

If user toggles while process is still starting:
- The SIGTERM will kill the starting process
- Restart will happen with new command
- Should work fine, but may look confusing

**Mitigation:** Disable toggle button briefly after restart (e.g., 2 seconds)

### 2. Toggle When Process Already Dead

If process has already exited:
- SIGTERM will fail silently
- `pendingRestartCmd` is set but never used
- Session may be in terminal state

**Mitigation:** Check process state before sending SIGTERM, handle appropriately

### 3. Rapid Toggle Clicks

If user clicks toggle multiple times quickly:
- Multiple SIGTERMs sent
- `pendingRestartCmd` overwritten each time
- Final state will be correct, but may cause multiple restarts

**Mitigation:** Debounce toggle or disable during restart

### 4. Agent Doesn't Support YOLO Mode

For agents where YOLO mode is unknown (e.g., OpenCode):
- Could hide the toggle
- Or show but with warning
- Or use best-guess command

**Mitigation:** Add `supportsYoloMode` to AssistantConfig

## Testing Plan

### Unit Tests

1. `computeRestartCommand()` returns correct command for each agent/mode combination
2. `detectYoloMode()` correctly identifies YOLO flags in commands
3. `pendingRestartCmd` is cleared after use

### Integration Tests

1. Toggle YOLO ON: verify process restarts with correct flags
2. Toggle YOLO OFF: verify process restarts without YOLO flags
3. Status broadcast includes correct `yoloMode` value
4. Frontend receives and displays correct YOLO state

### Manual Testing

1. Start session in normal mode, toggle to YOLO, verify agent behavior changes
2. Start session in YOLO mode, toggle to normal, verify prompts return
3. Test with each supported agent (Claude, Gemini, Codex, Aider, Goose)
4. Test rapid toggling doesn't break anything
5. Test toggle during agent startup

## Files to Modify

| File | Changes |
|------|---------|
| `cmd/swe-swe/templates/host/swe-swe-server/main.go` | Session struct, WebSocket handler, PTY reader, BroadcastStatus |
| `cmd/swe-swe/templates/host/swe-swe-server/static/index.html` | Status bar UI (if inline) |
| `cmd/swe-swe/templates/host/swe-swe-server/static/*.js` | WebSocket handler, UI logic |
| `cmd/swe-swe/templates/host/swe-swe-server/static/*.css` | YOLO toggle styling |

## Open Questions

1. **OpenCode YOLO flag:** What is the equivalent flag for OpenCode? Needs research.

2. **Goose environment variable:** The current approach passes env var inline (`GOOSE_MODE=auto goose...`). Does this work with the `script` wrapper? May need to modify environment in `RestartProcess()` instead.

3. **Session persistence:** Should YOLO state be saved in session metadata so it persists across server restarts?

4. **URL parameter:** Should YOLO mode be settable via URL parameter when creating session? (e.g., `?yolo=true`)

5. **Per-agent toggle visibility:** Should the toggle be hidden for agents that don't support YOLO mode?

## Future Enhancements

1. **Keyboard shortcut:** Add hotkey (e.g., Ctrl+Shift+Y) to toggle YOLO mode
2. **Session-level default:** Remember user's YOLO preference per assistant type
3. **Warning indicators:** Visual warning when YOLO mode is active (red border, etc.)
4. **Audit log:** Log YOLO mode changes for security/debugging

## References

- Claude Code: `--dangerously-skip-permissions` flag
- Gemini CLI: `--approval-mode=yolo`, Ctrl+Y in-session toggle
- Codex CLI: `--yolo` (alias for `--dangerously-bypass-approvals-and-sandbox`)
- Aider: `--yes-always` flag
- Goose: `GOOSE_MODE=auto` environment variable, `/mode` in-session command
