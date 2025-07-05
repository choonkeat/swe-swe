# How swe-swe Integrates with Claude CLI

This document provides a comprehensive technical analysis of how the `swe-swe` application integrates with the `claude` CLI tool to provide a web-based chat interface with real-time streaming capabilities.

## Architecture Overview

The system implements a real-time streaming architecture with three main components:

1. **Backend (Go)**: `cmd/swe-swe/` - WebSocket server with process management
2. **Frontend (Elm)**: `elm/src/Main.elm` - Functional reactive UI with JSON stream parsing
3. **Claude CLI**: External command-line tool with streaming JSON output support

### System Characteristics

- **Real-time Communication**: WebSocket-based bidirectional streaming
- **Process Management**: Go-based subprocess execution with context cancellation
- **Stream Processing**: Line-by-line JSON parsing with error recovery
- **State Management**: Elm's functional architecture for predictable UI updates
- **Session Continuity**: `--continue` flag maintains conversation context across messages

## Backend Implementation

### Main Components

1. **main.go**: Entry point that configures the server and command templates
2. **websocket.go**: Handles WebSocket connections and command execution
3. **handlers.go**: HTTP handlers for serving the web interface

### Claude CLI Configuration

When using the `claude` preset (`--agent claude`), the backend configures:

```go
// cmd/swe-swe/main.go:73-77
config.AgentCLI1st = "claude --output-format stream-json --verbose --dangerously-skip-permissions --print ?"
config.AgentCLINth = "claude --continue --output-format stream-json --verbose --dangerously-skip-permissions --print ?"
config.DeferStdinClose = false
config.JSONOutput = true
```

Key flags:
- `--output-format stream-json`: Outputs JSON messages line by line
- `--print` (or `-p`): Print mode - outputs response and exits
- `--verbose`: Includes detailed information in the output
- `--dangerously-skip-permissions`: Bypasses permission checks
- `--continue`: Continues a previous conversation
- `?`: Placeholder replaced with the user's message

### WebSocket Protocol

The WebSocket communication follows a structured protocol:

```go
// Client to Server message format
type ClientMessage struct {
    Type         string `json:"type,omitempty"`      // "stop" for cancellation
    Sender       string `json:"sender,omitempty"`    // "USER"
    Content      string `json:"content,omitempty"`   // User's message
    FirstMessage bool   `json:"firstMessage,omitempty"` // Session tracking
}

// Server to Client message format
type ChatItem struct {
    Type    string `json:"type"`              // Message type identifier
    Sender  string `json:"sender,omitempty"`  // For "user"/"bot" types
    Content string `json:"content,omitempty"` // Message content
}
```

### WebSocket Connection Lifecycle

1. **Connection Establishment**:
   - Client connects to `/ws` endpoint
   - Server creates `Client` struct with connection reference
   - Welcome message sent via broadcast mechanism
   - Connection added to active clients map

2. **Message Flow**:
   ```go
   // websocket.go:379-431
   for {
       var clientMsg ClientMessage
       if err := websocket.JSON.Receive(ws, &clientMsg); err != nil {
           break // Connection closed
       }
       
       // Handle stop command (process cancellation)
       if clientMsg.Type == "stop" {
           client.cancelFunc() // Cancel running process
           continue
       }
       
       // Broadcast user message, then execute agent
       executeAgentCommand(svc, client, clientMsg.Content, clientMsg.FirstMessage)
   }
   ```

3. **Process Management**:
   - Each client maintains a `cancelFunc` for process termination
   - Mutex protection prevents race conditions
   - Previous processes terminated before starting new ones
   - Context cancellation propagates to child processes

### Command Execution Architecture

The command execution involves several sophisticated mechanisms:

1. **Command Preparation**:
   ```go
   // websocket.go:169-189
   // Dynamic command selection based on message count
   if isFirstMessage {
       cmdArgs = make([]string, len(svc.agentCLI1st))
       copy(cmdArgs, svc.agentCLI1st)
   } else {
       cmdArgs = parseAgentCLI(svc.agentCLINth) // Uses --continue
   }
   
   // Placeholder substitution
   for i, arg := range cmdArgs {
       if arg == "?" {
           cmdArgs[i] = prompt
       }
   }
   ```

2. **Process Execution**:
   ```go
   // websocket.go:196-228
   cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
   
   // Stdin handling based on placeholder presence
   if !hasPlaceholder {
       go func() {
           defer stdin.Close()
           stdin.Write([]byte(prompt + "\n"))
       }()
   } else {
       // Claude flag: immediate stdin close signals EOF
       if !svc.deferStdinClose {
           stdin.Close()
       }
   }
   ```

3. **Stream Processing**:
   ```go
   // websocket.go:279-315
   scanner := bufio.NewScanner(stdout)
   for scanner.Scan() {
       select {
       case <-ctx.Done():
           // Handle cancellation
           return
       default:
           line := scanner.Text()
           if svc.jsonOutput {
               // Send raw JSON for Elm parsing
               svc.BroadcastItem(ChatItem{
                   Type:    "claudejson",
                   Content: line,
               })
           }
       }
   }
   ```

### Broadcast Mechanism

The server implements a fan-out broadcast pattern:

```go
// websocket.go:64-92
func (s *ChatService) Run(ctx context.Context) error {
    for {
        select {
        case item := <-s.broadcast:
            s.mutex.Lock()
            for client := range s.clients {
                if err := websocket.JSON.Send(client.conn, item); err != nil {
                    // Remove failed clients
                    delete(s.clients, client)
                    client.conn.Close()
                }
            }
            s.mutex.Unlock()
        }
    }
}
```

This ensures:
- Thread-safe client management
- Automatic cleanup of disconnected clients
- Non-blocking message distribution

## Frontend Implementation

### Elm Architecture

The Elm frontend implements a sophisticated stream processing system:

1. **Port Communication**:
   ```elm
   -- Outbound ports
   port sendMessage : String -> Cmd msg
   port scrollToBottom : () -> Cmd msg
   
   -- Inbound ports
   port messageReceiver : (String -> msg) -> Sub msg
   port connectionStatusReceiver : (Bool -> msg) -> Sub msg
   port systemThemeChanged : (String -> msg) -> Sub msg
   ```

2. **State Management**:
   ```elm
   type alias Model =
       { input : String
       , messages : List ChatItem
       , currentSender : Maybe String
       , theme : Theme
       , isConnected : Bool
       , systemTheme : Theme
       , isTyping : Bool
       , isFirstUserMessage : Bool
       , pendingToolUses : Dict String ClaudeContent
       }
   ```

3. **Message Processing Pipeline**:
   - Raw JSON received via WebSocket
   - Decoded into `ChatItem` variants
   - Claude JSON parsed into structured messages
   - Tool uses tracked for result correlation
   - UI updated with proper formatting

### Message Types

```elm
-- elm/src/Main.elm:93-104
type ChatItem
    = ChatUser String
    = ChatBot String
    = ChatContent String
    = ChatClaudeJSON String  -- Raw Claude JSON to be parsed
    = ChatToolResult String
    = ChatTodoWrite (List Todo)
    = ChatExecStart
    = ChatExecEnd
    = ChatToolUse ClaudeContent
    = ChatToolUseWithResult ClaudeContent String
```

### Claude JSON Stream Parsing

The frontend implements a sophisticated JSON parsing system with error recovery:

1. **Message Decoding Pipeline**:
   ```elm
   -- elm/src/Main.elm:250-277
   ChatClaudeJSON jsonStr ->
       case Decode.decodeString claudeMessageDecoder jsonStr of
           Ok claudeMsg ->
               let
                   parseResult = parseClaudeMessage model claudeMsg
                   newPendingToolUses = -- Track tool uses for correlation
               in
               ( { model
                   | messages = model.messages ++ parseResult.messages
                   , pendingToolUses = newPendingToolUses
                 }
               , scrollToBottom ()
               )
           Err _ ->
               -- Fallback: display raw JSON as content
               ( { model | messages = model.messages ++ [ ChatContent jsonStr ] }
               , scrollToBottom ()
               )
   ```

2. **Claude Message Structure**:
   ```elm
   type alias ClaudeMessage =
       { type_ : String            -- "assistant", "user", "result"
       , subtype : Maybe String    -- Additional type info
       , durationMs : Maybe Int    -- Execution time
       , result : Maybe String     -- Result status
       , message : Maybe ClaudeMessageContent
       }
   
   type alias ClaudeContent =
       { type_ : String            -- "text", "tool_use", "tool_result"
       , text : Maybe String       -- Text content
       , name : Maybe String       -- Tool name
       , input : Maybe Decode.Value -- Tool input as JSON
       , content : Maybe String    -- Tool result content
       , id : Maybe String         -- Tool use ID
       , toolUseId : Maybe String  -- Reference to tool use
       }
   ```

3. **Message Type Handling**:
   ```elm
   -- elm/src/Main.elm:659-744
   parseClaudeMessage : Model -> ClaudeMessage -> ParseResult
   parseClaudeMessage model msg =
       case msg.type_ of
           "assistant" ->
               -- Parse AI messages with tool uses
               parseClaudeContentList messageContent.content
           
           "result" ->
               -- Format completion message with duration
               { messages = [ ChatContent formattedResult ]
               , toolUses = []
               }
           
           "user" ->
               -- Correlate tool results with pending uses
               -- Creates ChatToolUseWithResult for matched pairs
   ```

4. **Tool Use Correlation**:
   The system tracks tool uses by ID to match them with results:
   - Tool uses stored in `pendingToolUses` dictionary
   - Results matched by `toolUseId`
   - Combined into `ChatToolUseWithResult` for display
   - Handles orphaned results gracefully

### WebSocket Client Implementation

The JavaScript glue code implements a robust WebSocket client with advanced features:

1. **Connection Management**:
   ```javascript
   // index.html.tmpl:43-106
   let socket = null;
   let reconnectAttempts = 0;
   let maxReconnectAttempts = 10;
   let messageQueue = [];
   
   function getReconnectDelay() {
       // Exponential backoff: 1s, 2s, 4s, 8s, 16s, max 30s
       return Math.min(1000 * Math.pow(2, reconnectAttempts), 30000);
   }
   ```

2. **Auto-Reconnection Logic**:
   ```javascript
   socket.onclose = function(event) {
       updateConnectionStatus(false);
       
       if (reconnectAttempts < maxReconnectAttempts) {
           const delay = getReconnectDelay();
           reconnectInterval = setTimeout(() => {
               reconnectAttempts++;
               connectWebSocket();
           }, delay);
       }
   };
   ```

3. **Message Queueing**:
   ```javascript
   app.ports.sendMessage.subscribe(function(message) {
       if (socket && socket.readyState === WebSocket.OPEN) {
           socket.send(message);
       } else {
           // Queue message if not connected
           messageQueue.push(message);
           console.log('Message queued - WebSocket not connected');
       }
   });
   
   // Flush queue on connection
   socket.onopen = function() {
       while (messageQueue.length > 0) {
           const message = messageQueue.shift();
           socket.send(message);
       }
   };
   ```

4. **UI Integration Features**:
   - Auto-resize textarea based on content
   - Smooth scrolling to new messages
   - Enter key handling (send vs newline)
   - MutationObserver for reactive updates

## Data Flow

1. **User Input** → Elm sends JSON via `sendMessage` port
2. **WebSocket** → Backend receives message
3. **Claude CLI** → Executes with user's prompt
4. **JSON Stream** → Each line sent as `claudejson` message
5. **Elm Parser** → Decodes JSON and creates appropriate `ChatItem`s
6. **UI Update** → Renders messages with proper formatting

## Claude JSON Stream Format

Claude's `--output-format stream-json` produces newline-delimited JSON with specific message types:

### Message Types and Structure

1. **Text Response**:
   ```json
   {
     "type": "assistant",
     "message": {
       "role": "assistant",
       "content": [{
         "type": "text",
         "text": "I'll help you with that. Let me analyze the code..."
       }]
     }
   }
   ```

2. **Tool Use**:
   ```json
   {
     "type": "assistant",
     "message": {
       "role": "assistant",
       "content": [{
         "type": "tool_use",
         "name": "Read",
         "input": {
           "file_path": "/src/main.go",
           "offset": 100,
           "limit": 50
         },
         "id": "toolu_01ABC123def456"
       }]
     }
   }
   ```

3. **Tool Result**:
   ```json
   {
     "type": "user",
     "message": {
       "role": "user",
       "content": [{
         "type": "tool_result",
         "tool_use_id": "toolu_01ABC123def456",
         "content": "File content here..."
       }]
     }
   }
   ```

4. **Completion Status**:
   ```json
   {
     "type": "result",
     "subtype": "success",
     "duration_ms": 1234,
     "result": "success"
   }
   ```

### Special Cases

1. **TodoWrite Tool**:
   - Parsed specially to create visual todo lists
   - Input contains `{"todos": [{"id": "1", "content": "...", "status": "...", "priority": "..."}]}`

2. **MultiEdit Tool**:
   - Truncated display for large edit arrays
   - Shows edit count instead of full content

3. **Mixed Content**:
   - Single message can contain multiple content items
   - Tool uses and text can be interleaved

## Advanced Features

### 1. Real-time Streaming
- **Line-buffered processing**: Each JSON line processed immediately
- **Non-blocking I/O**: Scanner-based reading prevents blocking
- **Concurrent handling**: Goroutines for stdin/stdout/stderr management

### 2. Process Lifecycle Management
```go
// Context-based cancellation
ctx, cancel := context.WithCancel(context.Background())
client.cancelFunc = cancel

// Graceful shutdown on context cancellation
select {
case <-ctx.Done():
    log.Printf("[EXEC] Process cancelled by context")
    return
```

### 3. Tool Use Visualization
- **Collapsible UI**: Details/summary tags for tool results
- **Smart truncation**: Large inputs (MultiEdit) show counts
- **Result correlation**: Tool uses matched with results by ID
- **ANSI rendering**: Terminal output preserved with color support

### 4. Error Recovery
- **JSON parse failures**: Fallback to raw text display
- **Connection drops**: Automatic reconnection with backoff
- **Process failures**: Error capture and user notification
- **Message queueing**: No message loss during disconnection

### 5. Theme System
- **Multiple themes**: Dark, Light, Solarized, System-aware
- **Live switching**: No page reload required
- **System integration**: Responds to OS theme changes
- **CSS variables**: Dynamic style application

## Performance Characteristics

### Backend Performance
- **Concurrent clients**: Each client in separate goroutine
- **Broadcast efficiency**: Single write, multiple sends
- **Process isolation**: Each command in new process
- **Memory usage**: Minimal - streams not buffered

### Frontend Performance
- **Virtual DOM**: Elm's efficient diff algorithm
- **Lazy rendering**: Only visible messages rendered
- **Batch updates**: Multiple messages processed together
- **Smooth scrolling**: RequestAnimationFrame-based

## Security Considerations

1. **Command Injection**: 
   - Prompts escaped for shell safety
   - No direct shell execution (exec.Command)
   - Argument array prevents injection

2. **Process Isolation**:
   - Each client has separate process
   - Context cancellation kills child processes
   - No shared state between clients

3. **WebSocket Security**:
   - Origin checking (if configured)
   - Message size limits
   - Connection timeouts

## Configuration Options

```bash
# CLI Flags
--port              # WebSocket server port (default: 7000)
--timeout           # Server timeout duration
--agent             # Preset: "claude" or "goose"
--agent-cli-1st     # First message command template
--agent-cli-nth     # Subsequent message command template
--prefix-path       # URL prefix for reverse proxy setups
--defer-stdin-close # Stdin handling mode
--json-output       # Enable JSON parsing mode
```

## Deployment Considerations

1. **Reverse Proxy Setup**:
   ```nginx
   location /myapp/ {
       proxy_pass http://localhost:7000/;
       proxy_http_version 1.1;
       proxy_set_header Upgrade $http_upgrade;
       proxy_set_header Connection "upgrade";
   }
   ```

2. **Process Limits**:
   - Set ulimits for max processes
   - Configure timeouts appropriately
   - Monitor memory usage

3. **Logging**:
   - Structured logging with context
   - Process lifecycle tracking
   - Error aggregation

## Comparison with Traditional Chat UIs

| Feature | Traditional | swe-swe |
|---------|------------|---------|
| Updates | Request/Response | Real-time streaming |
| Tool Visibility | Hidden | Full transparency |
| Process Control | Limited | Full (stop/cancel) |
| Session Management | Server-side | CLI-based (--continue) |
| Output Format | Plain text | Structured JSON |
| Error Handling | Modal/Alert | Inline with recovery |

## Running the Application

```bash
# Quick start with Claude
./swe-swe --agent claude --port 7000

# Custom configuration
./swe-swe \
  --agent-cli-1st "claude --output-format stream-json --verbose --print ?" \
  --agent-cli-nth "claude --continue --output-format stream-json --print ?" \
  --json-output \
  --port 8080 \
  --timeout 60s

# With reverse proxy prefix
./swe-swe --agent claude --prefix-path /chat --port 7000
```

The application provides a modern, responsive interface for interacting with Claude's powerful code analysis and generation capabilities, with full visibility into the AI's tool usage and thinking process.