# How claude-code-webui Integrates with Claude CLI

This document provides a comprehensive technical analysis of how `sugyan/claude-code-webui` integrates with the Claude CLI tool, with special focus on its unique interactive permission system.

## Architecture Overview

`claude-code-webui` is a modern web application built with Deno and React that provides a sophisticated UI for Claude CLI with interactive permission handling:

1. **Backend**: Deno server with Hono framework
2. **Frontend**: React with TypeScript and Tailwind CSS
3. **Communication**: HTTP streaming (Server-Sent Events style)
4. **Claude Integration**: Via `@anthropic-ai/claude-code` SDK
5. **Permission System**: Interactive dialog-based approval

### System Characteristics

- **Streaming**: HTTP newline-delimited JSON (ndjson)
- **State Management**: React hooks with custom streaming processors
- **Type Safety**: Shared TypeScript types between frontend/backend
- **Session Support**: Conversation continuity and history
- **Project Awareness**: Directory-based project isolation

## Backend Architecture

### Server Setup (Deno + Hono)

```typescript
// backend/main.ts
import { Hono } from "@hono/hono";
import { cors } from "@hono/hono/cors";
import { serveStatic } from "@hono/hono/deno";

const app = new Hono();

// CORS for development
app.use("/api/*", cors());

// API routes
app.post("/api/chat", chat);
app.get("/api/projects", getProjects);
app.get("/api/projects/:project/histories", getHistories);

// Static file serving for production
app.get("*", serveStatic({ root: "./dist" }));

// Start server
const port = Number(Deno.args[0] ?? Deno.env.get("PORT") ?? 8080);
console.log(`Server running on http://localhost:${port}`);
```

### Claude CLI Integration

The integration uses the official SDK:

```typescript
// backend/handlers/chat.ts
import { query } from "@anthropic-ai/claude-code/query";

// Execute Claude with streaming
const stream = query({
  prompt: body.prompt,
  sessionId: body.sessionId,
  allowedTools: body.allowedTools,  // Key for permissions!
  workingDirectory: projectDir,
  abort: abortController,
});

// Stream the responses
for await (const data of stream) {
  const line = JSON.stringify({ type: "claude_json", data });
  await writer.write(encoder.encode(line + "\n"));
}
```

## Interactive Permission System

This is the **key differentiator** of claude-code-webui. Unlike other implementations that use `--dangerously-skip-permissions`, this system provides interactive control.

### 1. Permission Flow Overview

```
User Input → Claude Attempts Tool → Permission Error → Dialog → User Choice → Continue/Abort
```

### 2. Backend Permission Handling

```typescript
// backend/handlers/chat.ts
try {
  for await (const data of stream) {
    // Stream each Claude response
    await writer.write(encoder.encode(
      JSON.stringify({ type: "claude_json", data }) + "\n"
    ));
  }
} catch (error) {
  // Permission errors are caught here
  if (error.message.includes("permission")) {
    await writer.write(encoder.encode(
      JSON.stringify({ type: "error", error: error.message }) + "\n"
    ));
  }
}
```

### 3. Frontend Permission Detection

```typescript
// frontend/src/utils/permissions.ts
export function isPermissionError(errorMessage: string): boolean {
  return errorMessage.includes("requires explicit permission");
}

export function extractToolFromError(errorMessage: string): string | null {
  // Extract tool pattern from error message
  // e.g., "Tool 'Read' requires explicit permission for pattern: '**/*.ts'"
  const match = errorMessage.match(/Tool '(\w+)' requires.*pattern: '([^']+)'/);
  return match ? `${match[1]}:${match[2]}` : null;
}
```

### 4. Permission Dialog Component

```typescript
// frontend/src/components/PermissionDialog.tsx
interface PermissionDialogProps {
  isOpen: boolean;
  toolName: string;
  pattern: string;
  onAllow: (permanent: boolean) => void;
  onDeny: () => void;
}

export function PermissionDialog({
  isOpen,
  toolName,
  pattern,
  onAllow,
  onDeny,
}: PermissionDialogProps) {
  return (
    <Dialog open={isOpen}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Permission Request</DialogTitle>
          <DialogDescription>
            Claude wants to use tool '{toolName}' with pattern '{pattern}'
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button onClick={() => onAllow(false)}>
            Yes (this time)
          </Button>
          <Button onClick={() => onAllow(true)}>
            Yes (always for this session)
          </Button>
          <Button variant="outline" onClick={onDeny}>
            No
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
```

### 5. Permission State Management

```typescript
// frontend/src/hooks/chat/usePermissions.ts
export function usePermissions() {
  const [allowedTools, setAllowedTools] = useState<string[]>([]);
  const [pendingPermission, setPendingPermission] = useState<{
    tool: string;
    pattern: string;
  } | null>(null);

  const handlePermissionRequest = useCallback((tool: string, pattern: string) => {
    setPendingPermission({ tool, pattern });
  }, []);

  const handlePermissionResponse = useCallback((allowed: boolean, permanent: boolean) => {
    if (!pendingPermission) return;

    if (allowed) {
      const permission = `${pendingPermission.tool}:${pendingPermission.pattern}`;
      
      if (permanent) {
        // Add to permanent allowed tools
        setAllowedTools(prev => [...prev, permission]);
      }
      
      // Continue with permission
      return { 
        temporaryPermission: !permanent ? permission : undefined,
        allowedTools: permanent ? [...allowedTools, permission] : allowedTools
      };
    }
    
    setPendingPermission(null);
    return null;
  }, [allowedTools, pendingPermission]);

  return {
    allowedTools,
    pendingPermission,
    handlePermissionRequest,
    handlePermissionResponse,
  };
}
```

### 6. Continuing After Permission Grant

```typescript
// frontend/src/components/ChatPage.tsx
const handlePermissionAllow = async (permanent: boolean) => {
  const permission = `${pendingPermission.tool}:${pendingPermission.pattern}`;
  
  let updatedAllowedTools = allowedTools;
  if (permanent) {
    updatedAllowedTools = [...allowedTools, permission];
    setAllowedTools(updatedAllowedTools);
  } else {
    // Temporary permission passed only for this request
    updatedAllowedTools = [...allowedTools, permission];
  }

  // Send continue message with updated permissions
  await sendMessage({
    prompt: "continue",
    sessionId: currentSessionId,
    allowedTools: updatedAllowedTools,
    project: selectedProject,
  });
  
  setShowPermissionDialog(false);
};
```

## Streaming Architecture

### HTTP Streaming Protocol

```typescript
// backend/handlers/chat.ts
return c.body(
  new ReadableStream({
    async start(controller) {
      const encoder = new TextEncoder();
      
      try {
        for await (const data of stream) {
          // Each line is a complete JSON object
          const line = JSON.stringify({ type: "claude_json", data });
          controller.enqueue(encoder.encode(line + "\n"));
        }
        
        // Signal completion
        controller.enqueue(encoder.encode(
          JSON.stringify({ type: "done" }) + "\n"
        ));
      } catch (error) {
        // Stream errors
        controller.enqueue(encoder.encode(
          JSON.stringify({ type: "error", error: error.message }) + "\n"
        ));
      }
    },
  }),
  200,
  { "Content-Type": "application/x-ndjson" }
);
```

### Frontend Stream Processing

```typescript
// frontend/src/hooks/streaming/useClaudeStreaming.ts
export function useClaudeStreaming() {
  const processStream = useCallback(async (
    response: Response,
    handlers: StreamHandlers
  ) => {
    const reader = response.body?.getReader();
    const decoder = new TextDecoder();

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      const chunk = decoder.decode(value);
      const lines = chunk.split("\n").filter(line => line.trim());

      for (const line of lines) {
        try {
          const parsed = JSON.parse(line);
          
          switch (parsed.type) {
            case "claude_json":
              await handlers.onClaudeMessage(parsed.data);
              break;
            case "error":
              if (isPermissionError(parsed.error)) {
                await handlers.onPermissionError(parsed.error);
              } else {
                await handlers.onError(parsed.error);
              }
              break;
            case "done":
              await handlers.onComplete();
              break;
          }
        } catch (e) {
          console.error("Failed to parse stream line:", line);
        }
      }
    }
  }, []);

  return { processStream };
}
```

## Message Type System

### Shared Types (Frontend & Backend)

```typescript
// shared/types.ts
export interface ChatRequest {
  prompt: string;
  sessionId?: string;
  allowedTools?: string[];
  project?: string;
}

export interface StreamMessage {
  type: "claude_json" | "error" | "done" | "aborted";
  data?: any;
  error?: string;
}

// Claude SDK message types
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

export interface ToolResultMessage {
  type: "user";
  message: {
    content: Array<{
      type: "tool_result";
      tool_use_id: string;
      content: string;
      is_error?: boolean;
    }>;
  };
}
```

### Frontend UI Message Types

```typescript
// frontend/src/types.ts
export type Message = 
  | ChatMessage 
  | SystemMessage 
  | ToolMessage 
  | ToolResultMessage 
  | ErrorMessage 
  | AbortMessage;

export interface ChatMessage {
  type: "user" | "assistant";
  content: string;
  timestamp: Date;
}

export interface ToolMessage {
  type: "tool";
  name: string;
  input: any;
  id: string;
}

export interface ToolResultMessage {
  type: "tool_result";
  tool_use_id: string;
  content: string;
  is_error: boolean;
}
```

## Advanced Features

### 1. Project Management

```typescript
// backend/handlers/projects.ts
export async function getProjects(c: Context) {
  const projectsDir = Deno.env.get("PROJECTS_DIR") || "./projects";
  const entries = [];
  
  for await (const entry of Deno.readDir(projectsDir)) {
    if (entry.isDirectory) {
      entries.push({
        name: entry.name,
        path: encodeURIComponent(entry.name),
      });
    }
  }
  
  return c.json(entries);
}
```

### 2. Conversation History

```typescript
// backend/handlers/histories.ts
export async function getHistories(c: Context) {
  const project = c.req.param("project");
  const logDir = `.claude/${project}/logs`;
  
  const sessions = [];
  for await (const entry of Deno.readDir(logDir)) {
    if (entry.name.endsWith(".json")) {
      const sessionId = entry.name.replace(".json", "");
      const firstMessage = await extractFirstMessage(
        `${logDir}/${entry.name}`
      );
      sessions.push({ sessionId, firstMessage });
    }
  }
  
  return c.json(sessions);
}
```

### 3. Demo Mode

```typescript
// frontend/src/hooks/useDemo.ts
export function useDemo() {
  const searchParams = new URLSearchParams(window.location.search);
  const isDemoMode = searchParams.get("demo") === "true";
  
  if (isDemoMode) {
    // Automated UI interactions for testing
    simulateUserInput("Show me the project structure");
    simulatePermissionDialog("Read:**/*.ts", true);
    simulateChatResponse("Here's the project structure...");
  }
}
```

## Security Model

### Permission-Based Security

Unlike other implementations, claude-code-webui does **NOT** use `--dangerously-skip-permissions`:

1. **Default Deny**: No tools are allowed by default
2. **Explicit Approval**: Each tool/pattern requires user consent
3. **Granular Control**: Permissions are tool+pattern specific
4. **Session Scope**: Permissions last for the session only
5. **Temporary Option**: One-time permissions for sensitive operations

### Example Permission Patterns

```typescript
// Common permission patterns
const permissions = [
  "Read:**/*.ts",      // Read all TypeScript files
  "Edit:src/**/*.js",  // Edit JavaScript in src
  "Bash:npm *",        // Run npm commands
  "Write:tests/**",    // Write files in tests directory
];
```

## Comparison with Other Implementations

### Permission Handling

| Implementation | Permission Model |
|----------------|-----------------|
| swe-swe | `--dangerously-skip-permissions` (bypass all) |
| async-code | Hardcoded `--allowedTools "Edit,Bash"` only |
| claude-code-chat | `--dangerously-skip-permissions` (bypass all) |
| **claude-code-webui** | **Interactive approval dialog** |

### Key Advantages

1. **Security**: User maintains control over tool usage
2. **Transparency**: See exactly what Claude wants to do
3. **Flexibility**: Grant temporary or permanent permissions
4. **Granularity**: Tool+pattern specific permissions
5. **User Trust**: No blanket permissions required

## Performance Characteristics

### Streaming Efficiency
- Line-by-line processing
- No buffering of entire response
- Immediate UI updates
- Abort capability

### Permission Overhead
- Minimal - only on first tool use
- Cached for session duration
- No performance impact after approval

## Running the Application

```bash
# Development
cd backend
deno run --allow-all main.ts

cd frontend
npm run dev

# Production build
cd frontend
npm run build
cd ../backend
deno run --allow-all main.ts

# With custom port
deno run --allow-all main.ts 3000

# With projects directory
PROJECTS_DIR=/path/to/projects deno run --allow-all main.ts
```

## Key Insights

The interactive permission system in claude-code-webui represents a significant advancement in Claude CLI integration:

1. **No --dangerously-skip-permissions flag** - True permission control
2. **User empowerment** - Decide what Claude can access
3. **Security by default** - Nothing allowed without consent
4. **Practical UX** - "Don't ask again" option for convenience
5. **Audit trail** - Know exactly what was permitted

This approach balances security with usability, making it suitable for both development and more sensitive environments where blanket permissions are unacceptable.