# swe-swe - Web UI for CLI Coding Agents

A web-based chat interface that provides a GUI frontend for any CLI coding agent. Connect your favorite AI coding tools through a clean, real-time web interface with theme support and embedded assets.

## What is swe-swe?

swe-swe exposes any command-line coding agent through a modern web interface. Instead of interacting with AI coding tools through terminal commands, you get:

- **Real-time chat interface** with streaming responses
- **Multiple theme options** including system preference detection
- **Self-contained binary** with embedded static assets
- **Configurable agent integration** - works with any CLI tool

## Quick Start

### Using Docker Compose (Recommended)

1. **Start all services:**
   ```bash
   docker-compose up -d
   ```

2. **Access the services:**
   - **swe-swe-claude**: http://swe-swe-claude.localhost:7000
   - **swe-swe-goose**: http://swe-swe-goose.localhost:7000
   - **goose**: http://goose.localhost:7000
   - **claude-code-webui**: http://claude-code-webui.localhost:7000

3. **Authentication:**
   All services are protected with HTTP Basic Authentication:
   - Username: `admin`
   - Password: `password`

   To customize credentials:
   ```bash
   # Generate new password hash
   docker run --rm httpd:alpine htpasswd -nbB admin yourpassword
   
   # Set via environment variable
   BASIC_AUTH_USERS='admin:$2y$05$...' docker-compose up -d
   ```

4. **Alternative domains:**
   You can also use `*.lvh.me` domains (e.g., http://swe-swe-claude.lvh.me:7000) or any other domain that resolves to localhost.

## Configuration

### Command Line Options

```bash
./bin/swe-swe [options]
```

**Available flags:**
- `-port int` - Port to listen on (default: 7000)
- `-timeout duration` - Server timeout (default: 30s)
- `-agent string` - Agent CLI command template (default: "goose run --resume --debug --text ?")
- `-prefix-path string` - URL prefix path for serving assets (e.g., "/myapp")

### Agent Integration

The `-agent` flag configures which coding agent to use:

```bash
# Use Claude Code
./bin/swe-swe -agent claude

# Use goose (uses native goose web interface)
./bin/swe-swe -agent goose
```

For custom agents, use the `-agent-cli-1st` and `-agent-cli-nth` flags with `?` as a placeholder for user input:

```bash
# Use a custom agent with specific parameters
./bin/swe-swe -agent-cli-1st "myagent --param value --text ?" -agent-cli-nth "myagent --continue --text ?"
```

**Note:** When using `-agent goose`, the application directly executes `goose web --port <PORT>`, replacing the current process.

## Architecture

### Frontend (Elm)
- **Technology**: Elm 0.19 with WebSocket ports
- **Themes**: CSS-in-JS with system preference detection
- **Message rendering**: Streams content chunks into coherent messages
- **Responsive design**: Mobile-friendly chat interface

### Backend (Go)
- **HTTP Server**: Uses `github.com/alvinchoong/go-httphandler` pipeline APIs
- **WebSocket**: Real-time bidirectional communication
- **Agent Integration**: Configurable CLI command execution with streaming
- **Asset Serving**: Embedded static files with content hashing
- **Graceful Shutdown**: Context-based cleanup with errgroup

### Message Protocol

**Client → Server:**
```json
{
  "sender": "USER",
  "content": "implement user authentication"
}
```

**Server → Client:**
```json
{"type": "user", "sender": "USER"}
{"type": "content", "content": "implement user authentication"}
{"type": "bot", "sender": "swe-swe"}
{"type": "content", "content": "I'll help you implement user authentication..."}
```

## Development

### Running Locally

1. **Build the application:**
   ```bash
   make build
   ```

2. **Run with default settings (uses goose):**
   ```bash
   ./bin/swe-swe
   ```

3. **Open your browser:**
   ```
   http://localhost:7000
   ```

### Building from Source

#### Prerequisites
- Go 1.21+
- Elm 0.19+
- Make

#### Build Commands
```bash
# Build everything (Elm + Go)
make build

# Build only Elm frontend
make build-elm

# Build only Go backend
make build-go

# Run tests
make test

# Clean build artifacts
make clean

# Run development server
make run
```

#### Development Workflow
1. Make changes to Elm code in `elm/src/Main.elm`
2. Make changes to Go code in `cmd/swe-swe/*.go`
3. Run `make build` to rebuild both frontend and backend
4. Test with `./bin/swe-swe`

## File Structure

```
swe-swe/
├── Makefile                    # Build automation
├── go.mod                      # Go module dependencies
├── elm/
│   ├── elm.json               # Elm package configuration
│   ├── src/
│   │   ├── Main.elm           # Elm frontend application
│   │   └── Ansi.elm           # ANSI color code parser
│   └── tests/
│       ├── AnsiTest.elm       # ANSI parser tests
│       └── ClaudeJSONTest.elm # Claude JSON parsing tests
├── cmd/swe-swe/
│   ├── main.go                # HTTP server and CLI flags
│   ├── websocket.go           # WebSocket handler and agent integration
│   ├── handlers.go            # HTTP request handlers
│   ├── embed.go               # Asset embedding and hashing
│   ├── index.html.tmpl        # HTML template with theme variables
│   └── static/
│       ├── css/styles.css     # Application styles and themes
│       └── js/app.js          # Compiled Elm (generated)
└── bin/
    └── swe-swe               # Compiled binary (generated)
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
