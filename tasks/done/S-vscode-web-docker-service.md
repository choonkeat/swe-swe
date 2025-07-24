# Feature: Add VS Code Web UI Service to Docker Compose

## Overview
Integrate a VS Code web-based editor service into the docker-compose.yml configuration, enabling users to edit files directly through a browser interface alongside the main application.

## Key Features

### 1. Service Configuration
- Add code-server or VS Code Server container
- Mount project volumes for file access
- Configure appropriate ports for web access
- Set up authentication/security

### 2. Integration Points
- Share volumes with main application container
- Network configuration for inter-service communication
- Consistent user permissions across containers
- Workspace settings synchronization

### 3. Container Options
**Option A: code-server (Open Source)**
```yaml
code-server:
  image: codercom/code-server:latest
  volumes:
    - .:/home/coder/project
    - ~/.config:/home/coder/.config
  ports:
    - "8443:8080"
  environment:
    - PASSWORD=changeme
```

**Option B: Official VS Code Server**
```yaml
vscode:
  image: gitpod/openvscode-server:latest
  volumes:
    - .:/workspace
  ports:
    - "3000:3000"
  command: --host 0.0.0.0
```

### 4. Security Considerations
- Password/token authentication
- HTTPS configuration option
- Network isolation options
- Read-only mode option
- IP whitelist configuration

### 5. Developer Experience
- Auto-open browser to VS Code on startup
- Pre-installed extensions configuration
- Theme and settings persistence
- Git integration preservation
- Terminal access to other containers

## Technical Requirements

### Volume Mapping
- Project root directory
- User configuration directory
- Extensions directory
- Git credentials (optional)

### Network Configuration
- Internal network for service communication
- External port exposure for browser access
- Optional reverse proxy setup

### Resource Allocation
```yaml
deploy:
  resources:
    limits:
      cpus: '2'
      memory: 2G
    reservations:
      cpus: '1'
      memory: 1G
```

## Configuration Options
```yaml
# .env file
VSCODE_PORT=8443
VSCODE_PASSWORD=your-secure-password
VSCODE_EXTENSIONS=ms-python.python,dbaeumer.vscode-eslint
VSCODE_THEME=dark
VSCODE_AUTH_METHOD=password # or 'none' for local dev
```

## Example Docker Compose Addition
```yaml
services:
  # ... existing services ...
  
  vscode:
    image: codercom/code-server:latest
    container_name: ${PROJECT_NAME:-app}_vscode
    restart: unless-stopped
    ports:
      - "${VSCODE_PORT:-8443}:8080"
    volumes:
      - .:/home/coder/project
      - vscode-config:/home/coder/.config
      - vscode-extensions:/home/coder/.local/share/code-server
    environment:
      - PASSWORD=${VSCODE_PASSWORD:-changeme}
      - TZ=${TZ:-UTC}
    networks:
      - app-network
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.vscode.rule=Host(`code.${DOMAIN:-localhost}`)"

volumes:
  vscode-config:
  vscode-extensions:
```

## Benefits
- Full IDE features in browser
- No local VS Code installation required
- Consistent development environment
- Easy collaboration/sharing
- Works on any device with a browser

## Considerations
- Additional memory/CPU overhead
- Security implications of web-exposed IDE
- Extension compatibility limitations
- Performance vs native VS Code
- File watcher limits in containers

## Estimation

### T-Shirt Size: S (Small)

### Breakdown
- **Docker Config**: XS
  - Add service definition
  - Configure volumes and ports
  
- **Security Setup**: S
  - Authentication configuration
  - HTTPS setup (optional)
  
- **Integration Testing**: S
  - Volume permissions
  - Network connectivity
  - Extension functionality

### Impact Analysis
- **User Experience**: Medium positive impact
- **Codebase Changes**: Minimal - docker-compose.yml only
- **Architecture**: No impact on main application
- **Performance Risk**: Low - isolated service

### Agent-Era Estimation Notes
This is "Small" because:
- Well-documented container images available
- Standard Docker Compose patterns
- No custom code required
- Clear configuration examples exist
- Isolated from main application complexity