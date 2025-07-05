# swe-swe vs claude-code-webui: Architectural Comparison

This document compares `swe-swe` with `sugyan/claude-code-webui`, focusing on their different approaches to Claude CLI integration and permission handling.

## Architecture Overview

### swe-swe: Direct Process Execution
- **Technology**: Go backend + Elm frontend
- **Communication**: WebSocket real-time streaming
- **Claude Integration**: Direct CLI process execution
- **Permission Model**: `--dangerously-skip-permissions` (bypass all)
- **Target**: Maximum capability local development

### claude-code-webui: SDK-Based Integration
- **Technology**: Deno backend + React frontend
- **Communication**: HTTP streaming (ndjson)
- **Claude Integration**: Via `@anthropic-ai/claude-code` SDK
- **Permission Model**: Interactive approval dialogs
- **Target**: Secure multi-user development

## Communication Protocols

### swe-swe: WebSocket Streaming

**Real-time bidirectional protocol**:
```go
// WebSocket message types
type ChatItem struct {
    Type    string `json:"type"`
    Sender  string `json:"sender,omitempty"`
    Content string `json:"content,omitempty"`
}

// Stream each JSON line from Claude
svc.BroadcastItem(ChatItem{
    Type:    "claudejson",
    Content: line,
})
```

**Connection management**:
- Persistent WebSocket connection
- Auto-reconnection with exponential backoff
- Message queueing during disconnection
- Stop/cancel commands via WebSocket

### claude-code-webui: HTTP Streaming

**Server-Sent Events style protocol**:
```typescript
// Stream newline-delimited JSON
return c.body(
  new ReadableStream({
    async start(controller) {
      for await (const data of stream) {
        const line = JSON.stringify({ type: "claude_json", data });
        controller.enqueue(encoder.encode(line + "\n"));
      }
    },
  }),
  { "Content-Type": "application/x-ndjson" }
);
```

**Request-response pattern**:
- Each conversation turn is a new HTTP request
- Stream completes when Claude finishes
- Abort via AbortController
- No persistent connection

## Claude CLI Integration

### swe-swe: Direct Process Execution

**Direct subprocess management**:
```go
// Execute Claude CLI directly
cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)

// Command template with permissions bypassed
config.AgentCLI1st = "claude --output-format stream-json --verbose --dangerously-skip-permissions --print ?"
```

**Benefits**:
- Maximum Claude capability
- All tools available
- No permission interruptions
- Direct control over CLI flags

**Risks**:
- No safety guardrails
- Full filesystem access
- All permissions granted implicitly

### claude-code-webui: SDK Integration

**Official SDK with permission control**:
```typescript
// Use official Claude SDK
import { query } from "@anthropic-ai/claude-code/query";

const stream = query({
  prompt: body.prompt,
  sessionId: body.sessionId,
  allowedTools: body.allowedTools,  // Granular control!
  workingDirectory: projectDir,
});
```

**Benefits**:
- Official support and updates
- Built-in permission system
- Safer execution environment
- Project-scoped operations

**Trade-offs**:
- Some features may be restricted
- Depends on SDK releases
- Permission dialogs interrupt flow

## Permission System Comparison

### swe-swe: Trust-Based Model

**Complete permission bypass**:
```bash
claude --dangerously-skip-permissions
```

**Characteristics**:
- ✅ No interruptions to workflow
- ✅ All Claude capabilities available
- ✅ Fast iteration and development
- ❌ No security controls
- ❌ Cannot limit Claude's access
- ❌ Full trust model required

### claude-code-webui: Interactive Permission Model

**Permission dialog system**:
```typescript
// Permission flow
Tool Request → Permission Error → User Dialog → Decision → Continue/Abort

// Granular permissions
allowedTools: [
  "Read:**/*.ts",      // Read TypeScript files
  "Edit:src/**/*.js",  // Edit specific directories
  "Bash:npm *"         // Limited shell commands
]
```

**Permission Dialog Options**:
1. **"Yes (this time)"** - Temporary permission for current request
2. **"Yes (always for this session)"** - Permanent for session
3. **"No"** - Deny the permission request

**Characteristics**:
- ✅ User maintains control
- ✅ Audit trail of permissions
- ✅ Granular tool+pattern control
- ✅ Suitable for sensitive environments
- ❌ Workflow interruptions
- ❌ Requires user attention

## Message Type Handling

### swe-swe: Strongly Typed Elm

**Complete type system for Claude messages**:
```elm
type alias ClaudeMessage =
    { type_ : String
    , subtype : Maybe String
    , durationMs : Maybe Int
    , result : Maybe String
    , message : Maybe ClaudeMessageContent
    }

-- Tool use correlation with IDs
type ChatItem
    = ChatToolUse ClaudeContent
    | ChatToolUseWithResult ClaudeContent String
    | ChatContent String
```

**Type safety benefits**:
- Compile-time guarantees
- Exhaustive pattern matching
- Tool use correlation by ID
- Graceful error handling

### claude-code-webui: TypeScript with Runtime Processing

**Shared types between frontend/backend**:
```typescript
export interface AssistantMessage {
  type: "assistant";
  message: {
    content: Array<{
      type: "text" | "tool_use";
      text?: string;
      name?: string;
      input?: any;
      id?: string;
    }>;
  };
}

// Stream message wrapper
export interface StreamMessage {
  type: "claude_json" | "error" | "done" | "aborted";
  data?: any;
  error?: string;
}
```

**Processing pipeline**:
```typescript
// Stream handlers
const handlers = {
  onClaudeMessage: processMessage,
  onPermissionError: showPermissionDialog,
  onError: handleError,
  onComplete: finishStream,
};
```

## Feature Comparison

### Session Management

**swe-swe**:
- In-memory only
- Lost on restart
- No conversation history
- Uses `--continue` flag

**claude-code-webui**:
- File-based persistence via Claude logs
- Session history browsing
- Project-scoped conversations
- Full conversation restoration

### User Interface

**swe-swe**:
```elm
-- Functional reactive UI
type Theme = DarkTerminal | ClassicTerminal | SoftDark | LightModern | Solarized
-- Custom theme system with ANSI support
```

**claude-code-webui**:
```typescript
// Modern React with Tailwind
import { Dialog, DialogContent, DialogHeader } from "@/components/ui/dialog";
// Shadcn/ui component system
```

### Advanced Features

| Feature | swe-swe | claude-code-webui |
|---------|---------|-------------------|
| **Permission Control** | ❌ Bypassed | ✅ Interactive dialogs |
| **Session Persistence** | ❌ | ✅ File-based |
| **Project Management** | ❌ | ✅ Directory-based |
| **Conversation History** | ❌ | ✅ Browsable history |
| **Tool Correlation** | ✅ By ID | ✅ By ID |
| **Multi-Theme Support** | ✅ 5 themes | ❌ Single theme |
| **Real-time Streaming** | ✅ WebSocket | ✅ HTTP streaming |
| **Type Safety** | ✅ Elm types | ✅ TypeScript |
| **Error Recovery** | ✅ Graceful | ✅ Stream error handling |
| **Stop/Cancel** | ✅ WebSocket | ✅ AbortController |

## Security Models

### swe-swe: Maximum Trust

**Security approach**:
```bash
# Complete trust in Claude
--dangerously-skip-permissions
```

**Implications**:
- Claude can read any file
- Claude can execute any command
- Claude can modify any file
- User must trust the codebase completely

**Suitable for**:
- Personal development environments
- Trusted codebases
- Maximum productivity scenarios

### claude-code-webui: Zero Trust with User Control

**Security approach**:
```typescript
// Default deny, explicit allow
allowedTools: [] // Start with no permissions

// User explicitly grants each permission
"Read:**/*.ts"     // Specific file patterns
"Edit:src/**"      // Limited directories
"Bash:npm test"    // Specific commands
```

**Implications**:
- User sees every tool request
- Granular control over access
- Audit trail of permissions
- Can deny risky operations

**Suitable for**:
- Shared development environments
- Sensitive codebases
- Security-conscious teams
- Production-adjacent environments

## Performance Characteristics

### Latency Comparison

**swe-swe**:
- WebSocket overhead: ~1-5ms
- Direct process execution: minimal
- First response: ~100ms
- Stream processing: immediate

**claude-code-webui**:
- HTTP request overhead: ~10-50ms
- SDK overhead: minimal
- First response: ~200ms
- Permission dialog: +2-10 seconds (user dependent)

### Resource Usage

**swe-swe**:
- Go binary: ~20MB
- Elm runtime: ~2MB
- Claude process: ~100MB-1GB
- Total: ~120MB-1GB

**claude-code-webui**:
- Deno runtime: ~50MB
- React bundle: ~5MB
- Claude SDK: ~30MB
- Total: ~85MB + Claude process

## Use Case Analysis

### swe-swe Excels When:

1. **Maximum Capability Needed**: Want all Claude tools available
2. **Fast Iteration**: No permission dialogs to slow down
3. **Trusted Environment**: Working on personal/trusted projects
4. **Type Safety Critical**: Elm's compile-time guarantees important
5. **Custom Deployment**: Need standalone web application

### claude-code-webui Excels When:

1. **Security Important**: Need control over what Claude accesses
2. **Team Environment**: Multiple developers using the system
3. **Sensitive Codebases**: Working with proprietary/sensitive code
4. **Project Organization**: Need project-scoped conversations
5. **Audit Requirements**: Need to track what was permitted

## Technical Trade-offs

### swe-swe Trade-offs:
- ✅ **Maximum capability** vs ❌ **No security controls**
- ✅ **Fast workflow** vs ❌ **Requires complete trust**
- ✅ **Type safety** vs ❌ **No permission granularity**

### claude-code-webui Trade-offs:
- ✅ **Security control** vs ❌ **Workflow interruptions**
- ✅ **User empowerment** vs ❌ **More complex UX**
- ✅ **Official SDK** vs ❌ **Dependency on SDK releases**

## Interactive Permission System Deep Dive

### How Permissions Work in claude-code-webui

1. **Tool Request**: Claude attempts to use a tool (e.g., Read file)
2. **Permission Check**: SDK checks if pattern is in allowedTools
3. **Permission Error**: If not allowed, SDK throws permission error
4. **Stream Interrupt**: Backend catches error, streams error message
5. **Frontend Detection**: Frontend detects permission error pattern
6. **Dialog Display**: Permission dialog shown to user
7. **User Decision**: User chooses temporary/permanent/deny
8. **Request Continuation**: If allowed, new request sent with updated permissions

### Permission Pattern Examples

```typescript
// File access patterns
"Read:**/*.ts"           // All TypeScript files
"Edit:src/**"            // Edit anything in src/
"Write:tests/**/*.test.js" // Write test files only

// Command patterns  
"Bash:npm *"             // Any npm command
"Bash:git status"        // Specific git command
"Bash:ls -la"            // Specific listing command

// Tool-specific patterns
"MultiEdit:**/*.js"      // Multi-file edits on JS
"Grep:**/package.json"   // Search package.json files
```

This granular control allows users to:
- Grant broad permissions (e.g., `Read:**/*`) for trusted operations
- Restrict to specific directories (e.g., `Edit:src/**`)  
- Allow only safe commands (e.g., `Bash:npm test`)
- Deny risky operations entirely

## Recommendations

### Choose swe-swe When:
- Working on personal projects
- Need maximum Claude capability
- Trust the codebase completely
- Want fastest possible iteration
- Type safety is critical

### Choose claude-code-webui When:
- Working in team environments
- Security and control are important
- Need project organization
- Want conversation history
- Working with sensitive code

### Best of Both Worlds:
An ideal system would combine:
- swe-swe's type safety and real-time streaming
- claude-code-webui's permission system and project management
- Configurable security levels (trust mode vs permission mode)
- Both WebSocket and HTTP streaming options

## Conclusion

These represent two fundamentally different philosophies:

- **swe-swe**: "Trust Claude completely for maximum capability"
- **claude-code-webui**: "Trust but verify - user maintains control"

Both are valid approaches serving different needs in the development workflow spectrum.