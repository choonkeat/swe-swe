# swe-swe - Web UI for CLI Coding Agents

A web-based chat interface that provides a GUI frontend for any CLI coding agent. Connect your favorite AI coding tools through a clean, real-time web interface with theme support and embedded assets.

## What is swe-swe?

swe-swe exposes any command-line coding agent through a modern web interface. Instead of interacting with AI coding tools through terminal commands, you get:

- **Real-time chat interface** with streaming responses
- **Multiple theme options** including system preference detection
- **Self-contained binary** with embedded static assets
- **Configurable agent integration** - works with any CLI tool

---

## Using swe-swe to Develop Your Own Project

This is the primary use case: using swe-swe's development environment to build and modify **your own codebase**.

### Quick Start - Develop Your Project

1. **Clone swe-swe repository:**
   ```bash
   git clone https://github.com/choonkeat/swe-swe.git
   cd swe-swe
   ```

2. **Set up your environment variables:**

   Create a `.env` file in the swe-swe directory:
   ```bash
   ANTHROPIC_API_KEY=your_api_key_here
   VSCODE_PASSWORD=your_vscode_password
   ```

3. **Start swe-swe pointing to your project:**
   ```bash
   # Replace with the absolute path to YOUR project directory
   WORKSPACE_DIR=/path/to/your/project make docker-compose-dev-up
   ```

   For example:
   ```bash
   WORKSPACE_DIR=/Users/me/my-app make docker-compose-dev-up
   ```

4. **Access the development environment:**

   All services will work on YOUR project directory:

   - **swe-swe-claude**: http://swe-swe-claude.localhost:7001 - Chat with Claude Code about your project
   - **vscode**: http://vscode.localhost:7001 - VSCode web editor for your project
   - **swe-swe-goose**: http://swe-swe-goose.localhost:7001 - Chat with Goose about your project
   - **goose**: http://goose.localhost:7001 - Native Goose web interface
   - **claude-code-webui**: http://claude-code-webui.localhost:7001 - Alternative Claude interface
   - **claudia**: http://claudia.localhost:7001 - Another Claude interface option
   - **Traefik Dashboard**: http://localhost:7002 - Monitor all services

5. **Authentication:**

   By default, services are NOT password protected. To enable HTTP Basic Authentication, uncomment the middleware lines in `docker/dev/docker-compose.yml` and configure credentials in `docker/dev/traefik-dynamic.yml`.

### How It Works

The key is the `WORKSPACE_DIR` environment variable:

- When you run `WORKSPACE_DIR=/path/to/your/project make docker-compose-dev-up`, the docker-compose configuration mounts your project directory into all agent containers at `/workspace`
- Each agent (Claude, Goose, etc.) and the VSCode container all see and can modify the same directory - YOUR project
- Changes made through any interface (chat agents or VSCode) are immediately reflected in your actual project directory on your host machine
- The default value (if `WORKSPACE_DIR` is not set) is `../../helloworld`, which is an example project in the swe-swe repo

### Working with the Environment

**Start development session:**
```bash
cd /path/to/swe-swe
WORKSPACE_DIR=/path/to/your/project make docker-compose-dev-up
```

**Stop all services:**
```bash
make docker-compose-dev-up-down
```

**Rebuild services (after updating swe-swe itself):**
```bash
make docker-compose-dev-up-build
```

**View logs:**
```bash
make docker-compose-dev-up-logs
```

### Using Alternative Domains

If `*.localhost` domains don't work on your system, you can use `*.lvh.me` which also resolves to 127.0.0.1:

- http://swe-swe-claude.lvh.me:7001
- http://vscode.lvh.me:7001
- etc.

### Tips for Development

1. **Start with VSCode**: Open http://vscode.localhost:7001 to browse your project structure and understand the codebase
2. **Use chat agents for coding**: Open the Claude or Goose interfaces to have AI help you write, refactor, or debug code
3. **Multiple agents simultaneously**: You can have multiple browser tabs open - VSCode in one, Claude chat in another - all working on the same project
4. **Real-time sync**: All changes are synchronized immediately between containers and your host machine

---

## Developing swe-swe Itself

If you want to modify or contribute to swe-swe itself, here's how to set up a development environment.

### Prerequisites
- Go 1.21+
- Elm 0.19+
- Make
- Docker & Docker Compose

### Development Setup

1. **Clone the repository:**
   ```bash
   git clone https://github.com/choonkeat/swe-swe.git
   cd swe-swe
   ```

2. **Build the application:**
   ```bash
   make build
   ```

3. **Run locally:**
   ```bash
   make run
   ```

4. **Access at:**
   ```
   http://localhost:7000
   ```

### Build Commands

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

### Development Workflow for swe-swe

1. Make changes to Elm code in `elm/src/Main.elm`
2. Make changes to Go code in `cmd/swe-swe/*.go`
3. Run `make build` to rebuild both frontend and backend
4. Test with `./bin/swe-swe`

### Running swe-swe Development Environment

To develop swe-swe using swe-swe itself (meta!):

```bash
# From within the swe-swe directory
make docker-compose-dev-up
```

This will mount the swe-swe directory as the workspace, and you can use the web interfaces to modify swe-swe's own code.

---

## Configuration

### Command Line Options

When running the `swe-swe` binary directly:

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

---

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

---

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
├── docker/
│   └── dev/
│       ├── docker-compose.yml # Docker compose configuration
│       ├── Dockerfile.claude  # Claude agent container
│       ├── Dockerfile.goose   # Goose agent container
│       ├── Dockerfile.vscode  # VSCode web container
│       └── ...                # Other Dockerfiles
├── helloworld/                # Example project (default WORKSPACE_DIR)
└── bin/
    └── swe-swe               # Compiled binary (generated)
```

---

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
