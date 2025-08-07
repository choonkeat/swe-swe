## Task: Implement Slash Command System

### Overview
Add a slash command system that activates when the user types `/` as the first character in the input field, similar to Slack or Discord. The first command to implement is `/security-review`.

### Requirements

#### 1. Slash Command Detection & UI
- When user types `/` as the first character in an empty input field, display a dropdown list of available commands
- Command list should show:
  - Command name (e.g., `/security-review`)
  - Brief description of what the command does
- Support keyboard navigation (arrow keys to select, Enter to confirm, Esc to cancel)
- Filter commands as user continues typing after `/`

#### 2. `/security-review` Command Implementation
**Condition**: Only available when application started with `-agent claude` flag

**Behavior**:
- Instead of using the existing claude command line arguments (from the agent configuration)
- Execute: `claude --continue` 
- Send `/security-review` to the command's stdin
- The command essentially pipes `/security-review` into `claude --continue`

### Technical Implementation Details

1. **Frontend (Elm)**:
   - Add new message type for slash command state
   - Modify input handler to detect `/` at position 0
   - Create dropdown UI component for command list
   - Handle keyboard events for navigation

2. **Backend (Go)**:
   - Add endpoint to fetch available commands based on current agent
   - For `/security-review` with `-agent claude`:
     
     cmd := exec.Command("claude", "--continue")
     stdin, _ := cmd.StdinPipe()
     stdin.Write([]byte("/security-review\n"))
     


   - Handle command execution differently from regular messages

3. **Command Registry**:
   - Define command structure:
     
     type SlashCommand struct {
         Name        string
         Description string
         AgentFilter string // e.g., "claude" 
         Handler     func() error
     }
     


### Future Extensibility
- Easy to add new commands to the registry
- Commands can be agent-specific or universal
- Could support command parameters (e.g., `/review file.go`)
