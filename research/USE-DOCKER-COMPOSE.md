# Using swe-swe Docker Compose in External Projects

## Overview

The current `docker-compose.yml` in this repository is designed for development and includes multiple services with build contexts. For external projects wanting to use swe-swe services, a simplified standalone version is needed.

## Current Setup Analysis

The existing docker-compose.yml includes:
- **Traefik reverse proxy** with basic auth
- **4 services**: swe-swe-claude, swe-swe-goose, goose, claude-code-webui
- **Custom Dockerfiles** for each service
- **Volume mounts** to current directory as `/workspace`
- **Environment variables** for API keys

## Standalone Requirements

For external projects, you need:

### Required Files
1. **docker-compose.yml** - Simplified version using pre-built images
2. **traefik-dynamic.yml** - Authentication configuration
3. **.env** - Environment variables (API keys, auth)

### Pre-built Images Option
The ideal approach would be to publish pre-built images to Docker Hub:
- `swe-swe/claude:latest`
- `swe-swe/goose:latest`
- `swe-swe/claude-code-webui:latest`
- `swe-swe/goose-web:latest`

## Recommended Standalone docker-compose.yml Structure

```yaml
services:
  # Traefik reverse proxy with basic auth
  traefik:
    image: traefik:v3.0
    container_name: traefik
    command:
      - "--api.insecure=true"
      - "--providers.docker=true"
      - "--providers.docker.exposedbydefault=false"
      - "--providers.file.filename=/etc/traefik/dynamic.yml"
      - "--entrypoints.web.address=:7000"
    ports:
      - "7000:7000"
      - "8081:8080"  # Traefik dashboard
    volumes:
      - "/var/run/docker.sock:/var/run/docker.sock:ro"
      - "./traefik-dynamic.yml:/etc/traefik/dynamic.yml:ro"
    networks:
      - swe-swe-network

  # Service running swe-swe with claude CLI
  swe-swe-claude:
    image: swe-swe/claude:latest  # Pre-built image
    container_name: swe-swe-claude
    command: ["-agent", "claude", "-host", "0.0.0.0", "-port", "7001"]
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.swe-claude.rule=HostRegexp(`swe-swe-claude\\..+`)"
      - "traefik.http.routers.swe-claude.middlewares=auth@file"
      - "traefik.http.services.swe-claude.loadbalancer.server.port=7001"
    volumes:
      - .:/workspace  # Mount project directory
    working_dir: /workspace
    networks:
      - swe-swe-network
    environment:
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}

networks:
  swe-swe-network:
    driver: bridge
```

## Alternative: Build from Source

If pre-built images aren't available, external projects would need:

1. **Git submodule or copy** of the swe-swe repository
2. **All Dockerfiles** and build context
3. **Build command**: `docker-compose build`

## Minimal Setup for Claude Only

For projects only wanting swe-swe-claude:

```yaml
services:
  swe-swe-claude:
    image: swe-swe/claude:latest
    container_name: swe-swe-claude
    ports:
      - "7000:7000"
    volumes:
      - .:/workspace
    working_dir: /workspace
    environment:
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
    command: ["-agent", "claude", "-host", "0.0.0.0", "-port", "7000"]
```

## Required Environment Variables

External projects need a `.env` file:

```env
# Required for Claude services
ANTHROPIC_API_KEY=your_anthropic_api_key_here

# Optional: Custom basic auth (default: admin/password)
BASIC_AUTH_USERS=admin:$2y$05$pE.39zvni14nH8PioXXEzuaH2qxcpk.22Nnh2LAz3WymNMPEy3uXa
```

## Usage Instructions

1. **Create project directory**
2. **Copy required files**:
   - `docker-compose.yml` (standalone version)
   - `traefik-dynamic.yml` (if using Traefik)
   - `.env` (with API keys)
3. **Run**: `docker-compose up -d`
4. **Access**: http://swe-swe-claude.localhost:7000

## Recommendations

1. **Publish pre-built images** to Docker Hub for easier consumption
2. **Create installation script** that sets up required files
3. **Document environment variables** clearly
4. **Provide minimal examples** for common use cases
5. **Consider single-service containers** for simpler deployment