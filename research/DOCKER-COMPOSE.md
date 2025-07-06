# Docker Compose Setup for swe-swe

This Docker Compose setup provides multiple coding assistant services that share the same workspace directory.

## Services

### 1. swe-swe-claude (Port 7001)
- **Status**: ✅ Running
- **Description**: Runs swe-swe with claude agent
- **Access**: http://localhost:7001
- **Dockerfile**: `Dockerfile.claude`

### 2. swe-swe-goose (Port 7002)
- **Status**: ✅ Running
- **Description**: Runs swe-swe with goose agent
- **Access**: http://localhost:7002
- **Dockerfile**: `Dockerfile.goose`

### 3. goose (Port 9000)
- **Status**: ✅ Running
- **Description**: Native goose web interface
- **Access**: http://localhost:9000
- **Dockerfile**: `Dockerfile.goose-web`

### 4. claude-code-webui (Port 8080)
- **Status**: ✅ Running
- **Description**: Web UI for Claude coding assistant
- **Access**: http://localhost:8080
- **Dockerfile**: `Dockerfile.claude-code-webui`

## Key Features

- **Shared Volume**: All services mount the current directory to `/workspace`
- **File Synchronization**: Changes made by one service are immediately visible to others
- **Isolation**: Each service runs in its own container while sharing the filesystem
- **Network**: All services communicate on the `swe-swe-network` bridge network

## Usage

### Start all services
```bash
docker-compose up -d
```

### Check service status
```bash
docker-compose ps
```

### View logs for a specific service
```bash
docker-compose logs <service-name>
```

### Stop all services
```bash
docker-compose down
```

### Rebuild a specific service
```bash
docker-compose build <service-name>
docker-compose up -d <service-name>
```

## Environment Variables

- `CLAUDE_API_KEY`: Required for claude-code-webui service
- `GOOSE_MODEL`: Set to `claude-3-5-sonnet-20241022` for goose service

## File Structure

```
.
├── docker-compose.yml          # Main compose configuration
├── Dockerfile                  # Base swe-swe dockerfile
├── Dockerfile.claude           # swe-swe with claude setup
├── Dockerfile.goose            # swe-swe with goose CLI
├── Dockerfile.goose-web        # Native goose web
├── Dockerfile.claude-code-webui # Claude code web UI
└── Dockerfile.minimal          # Minimal base image (unused)
```

## Notes

- The services work with the same current directory mounted as `/workspace`
- Any file changes (like editing README.md) are visible to all services
- Services can be accessed independently through their respective ports
- Some services may require API keys to be set in environment variables