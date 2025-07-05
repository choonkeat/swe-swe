# swe-swe vs async-code: Architectural Comparison

This document provides a detailed comparison between the two Claude CLI integration approaches implemented in this repository.

## Architecture Overview

### swe-swe: Real-time Streaming
- **Communication**: WebSocket with real-time JSON streaming
- **Execution**: Direct process execution on host
- **UI Updates**: Live streaming as Claude works
- **Storage**: In-memory, session-based
- **Target**: Local development and prototyping

### async-code: Task-based Processing
- **Communication**: HTTP REST API with polling
- **Execution**: Docker container isolation
- **UI Updates**: Periodic status polling
- **Storage**: Persistent database (Supabase)
- **Target**: Production multi-user service

## Claude CLI Integration Differences

### Command Execution

**swe-swe**:
```bash
# Direct host execution with full permissions
claude --output-format stream-json --verbose --dangerously-skip-permissions --print "user prompt"
claude --continue --output-format stream-json --verbose --dangerously-skip-permissions --print "follow-up"
```

**async-code**:
```bash
# Container execution with restricted tools
cat /tmp/prompt.txt | node /usr/local/bin/claude --print --allowedTools "Edit,Bash"
```

### Permission Models

| Aspect | swe-swe | async-code |
|--------|---------|------------|
| **Permission Bypassing** | `--dangerously-skip-permissions` | No bypass - permissions enforced |
| **Tool Availability** | All tools enabled | Hardcoded to `Edit,Bash` only |
| **User Control** | Full trust model | No user control over tools |
| **File System Access** | Full host filesystem | Container `/workspace` only |
| **Network Access** | Full host network | Container network isolation |
| **Execution Environment** | Direct host | Sandboxed container |

## Tool Permission Analysis

### swe-swe: Maximum Capability
- **Available Tools**: Read, Write, Edit, MultiEdit, Bash, Grep, TodoWrite, etc.
- **Permission Handling**: Completely bypassed with `--dangerously-skip-permissions`
- **Failure Mode**: No tool restrictions, minimal failures
- **Security Model**: Trust-based (suitable for local development)

### async-code: Security-First Restrictions
- **Available Tools**: Only `Edit` and `Bash`
- **Permission Handling**: Normal Claude permission system active
- **Failure Mode**: Tasks fail when Claude needs forbidden tools
- **Security Model**: Restriction-based (suitable for production)

### Critical Limitation in async-code

The hardcoded tool restriction causes significant functionality loss:

```bash
# These common scenarios will FAIL in async-code:
# 1. Claude tries to read existing files
claude: "Let me read the current implementation"
# → Error: Read tool not allowed

# 2. Claude tries to create new files  
claude: "I'll create a new test file"
# → Error: Write tool not allowed

# 3. Claude tries to search codebase
claude: "Let me search for similar patterns"
# → Error: Grep tool not allowed

# 4. Claude tries to make complex edits
claude: "I'll update multiple files"
# → Error: MultiEdit tool not allowed
```

**Result**: async-code Claude is significantly less capable than swe-swe Claude for most real-world coding tasks.

### Missing User Control

async-code provides **no mechanism** for users to:
- Choose which tools to allow
- Understand why tasks failed due to tool restrictions
- Retry with different tool permissions
- Configure per-user or per-project tool preferences

## Communication Protocols

### swe-swe: WebSocket Streaming

**Message Flow**:
1. User types → WebSocket send
2. Process starts → `exec_start` event
3. Claude outputs line-by-line → `claudejson` messages
4. Process ends → `exec_end` event
5. User can stop → `stop` command

**Data Structures**:
```go
type ClientMessage struct {
    Type         string `json:"type,omitempty"`
    Sender       string `json:"sender,omitempty"`
    Content      string `json:"content,omitempty"`
    FirstMessage bool   `json:"firstMessage,omitempty"`
}

type ChatItem struct {
    Type    string `json:"type"`
    Sender  string `json:"sender,omitempty"`
    Content string `json:"content,omitempty"`
}
```

**Claude JSON Parsing**:
```elm
type alias ClaudeMessage =
    { type_ : String
    , subtype : Maybe String
    , durationMs : Maybe Int
    , result : Maybe String
    , message : Maybe ClaudeMessageContent
    }

type alias ClaudeContent =
    { type_ : String
    , text : Maybe String
    , name : Maybe String
    , input : Maybe Decode.Value
    , content : Maybe String
    , id : Maybe String
    , toolUseId : Maybe String
    }
```

### async-code: HTTP Polling

**Message Flow**:
1. User submits → HTTP POST `/start-task`
2. Task queued → Returns task ID
3. Container starts → Background execution
4. Frontend polls → GET `/task-status/{id}` every 2s
5. Container completes → Results parsed and stored
6. User sees results → Final status with git diff

**Data Structures**:
```python
# No structured Claude JSON parsing
# Uses text markers instead:
lines = logs.split('\n')
if line == '=== PATCH START ===':
    capturing_patch = True
elif line == '=== GIT DIFF START ===':
    capturing_diff = True
```

## Claude JSON Message Handling

### swe-swe: Comprehensive JSON Processing

**Real-time Parsing**: Each JSON line from Claude is immediately parsed and typed:

```elm
-- Sophisticated type system for Claude messages
parseClaudeMessage : Model -> ClaudeMessage -> ParseResult
parseClaudeMessage model msg =
    case msg.type_ of
        "assistant" -> -- Handle AI responses with tool uses
        "user" -> -- Handle tool results
        "result" -> -- Handle completion messages
```

**Tool Use Correlation**: 
- Tool uses tracked by ID in `pendingToolUses` dictionary
- Results matched with tool uses via `toolUseId`
- Combined into `ChatToolUseWithResult` for rich display

**Error Recovery**:
- JSON parse failures fall back to raw text display
- Malformed messages don't break the interface
- Streaming continues even with occasional bad JSON

### async-code: No Claude JSON Processing

**Text-based Parsing**: Uses simple string markers instead of JSON:

```python
# Primitive text parsing - no JSON structure
if line.startswith('COMMIT_HASH='):
    commit_hash = line.split('=', 1)[1]
elif line == '=== PATCH START ===':
    capturing_patch = True
```

**No Tool Visibility**: 
- Users can't see individual tool uses
- No real-time feedback on what Claude is doing  
- Tool failures are hidden in container logs

**Limited Error Handling**:
- Container failures cause entire task to fail
- No granular error recovery
- Users get minimal feedback on what went wrong

## Type Safety Comparison

### swe-swe: Strongly Typed with Complete Claude JSON Support

**Full Claude Message Type Definitions**:
```elm
-- Complete type hierarchy for Claude's stream-json format
type alias ClaudeMessage =
    { type_ : String            -- "assistant", "user", "result"
    , subtype : Maybe String    -- Additional categorization
    , durationMs : Maybe Int    -- Execution timing
    , result : Maybe String     -- Success/failure status
    , message : Maybe ClaudeMessageContent
    }

type alias ClaudeMessageContent =
    { role : Maybe String       -- "assistant" or "user"
    , content : List ClaudeContent
    }

type alias ClaudeContent =
    { type_ : String            -- "text", "tool_use", "tool_result"
    , text : Maybe String       -- Text content
    , name : Maybe String       -- Tool name (Read, Edit, Bash, etc.)
    , input : Maybe Decode.Value -- Tool parameters as JSON
    , content : Maybe String    -- Tool result content
    , id : Maybe String         -- Unique tool use ID
    , toolUseId : Maybe String  -- Reference to tool use
    }
```

**Type-Safe JSON Decoders**:
```elm
-- Elm decoders ensure runtime type safety
claudeMessageDecoder : Decode.Decoder ClaudeMessage
claudeContentDecoder : Decode.Decoder ClaudeContent

-- Custom applicative helpers for robust parsing
required : String -> Decode.Decoder a -> Decode.Decoder (a -> b) -> Decode.Decoder b
optional : String -> Decode.Decoder a -> a -> Decode.Decoder (a -> b) -> Decode.Decoder b
```

**Benefits**:
- Every Claude message field is typed and validated
- Compile-time guarantees for message handling
- Runtime validation with graceful fallbacks
- Tool parameters preserved as structured JSON

### async-code: No Type System for Claude Messages

**Complete Absence of Claude JSON Types**:
```python
# async-code has NO type definitions for Claude messages
# Instead, it completely ignores Claude's JSON output:

# utils/code_task_v2.py - Primitive text parsing
for line in lines:
    if line.startswith('COMMIT_HASH='):
        commit_hash = line.split('=', 1)[1]
    elif line == '=== PATCH START ===':
        capturing_patch = True
    # No JSON parsing whatsoever
```

**What async-code Ignores**:
```python
# These Claude JSON messages are completely discarded:
{"type": "assistant", "message": {"content": [{"type": "text", "text": "..."}]}}
{"type": "assistant", "message": {"content": [{"type": "tool_use", "name": "Edit", ...}]}}
{"type": "user", "message": {"content": [{"type": "tool_result", ...}]}}
{"type": "result", "duration_ms": 1234}

# async-code only captures bash script output markers
```

**Consequences**:
- No visibility into Claude's reasoning
- Tool usage completely hidden from users
- No structured error information
- Can't debug why Claude failed
- Loses all Claude metadata (timing, tool IDs, etc.)

## Claude Message Handling Comparison

### swe-swe: Real-time Streaming with Full Visibility

**Message Processing Pipeline**:
```elm
-- Every Claude JSON line is processed
ChatClaudeJSON jsonStr ->
    case Decode.decodeString claudeMessageDecoder jsonStr of
        Ok claudeMsg ->
            -- Parse into typed messages
            let parseResult = parseClaudeMessage model claudeMsg
            -- Track tool uses for correlation
            -- Update UI with structured content
        Err _ ->
            -- Graceful fallback to raw text
```

**What Users See**:
- Real-time text as Claude thinks
- Individual tool uses with parameters
- Tool results correlated with uses
- Execution timing and status
- Complete transparency

### async-code: Black Box Execution

**No Message Processing**:
```python
# Claude's output is run through bash script markers
# All JSON is thrown away
echo "=== PATCH START ==="
cat /tmp/changes.patch
echo "=== PATCH END ==="
```

**What Users See**:
- Task status: "running" → "completed"
- Final git diff (if successful)
- Error message (if failed)
- No insight into Claude's process
- No tool visibility

## Data Structure Comparison

### swe-swe: Rich Message Types

```elm
type ChatItem
    = ChatUser String
    | ChatBot String
    | ChatContent String
    | ChatClaudeJSON String          -- Raw JSON for parsing
    | ChatToolResult String          -- Formatted tool results
    | ChatTodoWrite (List Todo)      -- Structured todo lists
    | ChatExecStart                  -- Process lifecycle events
    | ChatExecEnd
    | ChatToolUse ClaudeContent      -- Tool invocation details
    | ChatToolUseWithResult ClaudeContent String  -- Correlated pairs
```

This enables:
- Rich UI rendering based on message type
- Tool use visualization with collapsible details
- Special handling for specific tools (TodoWrite)
- Process lifecycle indicators

### async-code: Minimal Task Status

```typescript
// Only basic task metadata - no Claude message types
interface Task {
    id: number
    status: string
    prompt: string
    repo_url: string
    git_diff?: string
    error?: string
}
```

This limits UI to:
- Basic status display
- Final diff rendering
- Error messages
- No intermediate feedback

## Performance Characteristics

### swe-swe: Optimized for Responsiveness
- **Latency**: Sub-second response to user input
- **Throughput**: Line-by-line streaming, no batching delays
- **Memory**: Minimal - streams not buffered
- **Concurrency**: Each client gets dedicated goroutine

### async-code: Optimized for Scale
- **Latency**: 2-second polling intervals
- **Throughput**: Batch processing after completion
- **Memory**: Higher - full container overhead
- **Concurrency**: Docker container isolation

## Security Models

### swe-swe: Trust-Based Security
**Assumptions**:
- Single user or trusted environment
- Local development context
- User understands risks of `--dangerously-skip-permissions`

**Benefits**:
- Maximum Claude capability
- No artificial restrictions
- Fast iteration

**Risks**:
- Claude can modify any host files
- Full network access
- No audit trail

### async-code: Defense-in-Depth Security
**Assumptions**:
- Multi-user production environment
- Untrusted user input
- Need for audit and compliance

**Benefits**:
- Container isolation
- Limited tool access
- Persistent audit logs
- GitHub integration with PR workflow

**Limitations**:
- Significantly reduced Claude capability
- No user control over restrictions
- Harder to debug tool failures

## Use Case Suitability

### swe-swe is Better For:
- **Local Development**: When you want maximum Claude capability
- **Prototyping**: Fast iteration with immediate feedback
- **Learning**: See exactly how Claude uses tools
- **Complex Tasks**: When Claude needs full tool access
- **Interactive Sessions**: Real-time collaboration with Claude

### async-code is Better For:
- **Production Services**: Multi-user environments with security needs
- **Team Collaboration**: Persistent task history and PR integration
- **Audit Requirements**: When you need logs of all AI actions
- **Scalability**: When serving many users concurrently
- **Simple Tasks**: When `Edit` and `Bash` tools are sufficient

## Recommendations

### For Developers:
- **Use swe-swe** for daily development work where you want full Claude capability
- **Use async-code** when you need to share Claude access with a team safely

### For async-code Improvements:
1. **Add User Tool Selection**: Allow users to choose allowed tools per task
2. **Implement Claude JSON Parsing**: Use structured message types instead of text markers
3. **Add Tool Failure Recovery**: Detect and handle tool permission errors gracefully
4. **Provide Tool Usage Visibility**: Show users what tools Claude is trying to use
5. **Add Progressive Permissions**: Start restrictive, escalate on user approval

### For swe-swe Improvements:
1. **Add Optional Tool Restrictions**: Allow users to limit tools if desired
2. **Add Task Persistence**: Option to save conversation history
3. **Add Multi-User Support**: For team environments that want real-time streaming

## Conclusion

Both approaches have merit for different use cases:

- **swe-swe** maximizes Claude's capability at the cost of security isolation
- **async-code** maximizes security at the cost of Claude's functionality

The ideal solution would combine swe-swe's sophisticated Claude integration with async-code's security model and user management, while providing users meaningful control over the capability/security trade-offs.