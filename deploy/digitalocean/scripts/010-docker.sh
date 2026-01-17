#!/bin/bash
# Install Docker Engine and Docker Compose plugin
set -euo pipefail

echo "==> Installing Docker Engine..."

# Install Docker using the convenience script
curl -fsSL https://get.docker.com | sh

# Verify Docker installation
docker --version

echo "==> Installing Docker Compose plugin..."

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  COMPOSE_ARCH="x86_64" ;;
    aarch64) COMPOSE_ARCH="aarch64" ;;
    *)       echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Install Docker Compose plugin
COMPOSE_VERSION=$(curl -fsSL https://api.github.com/repos/docker/compose/releases/latest | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
mkdir -p /usr/local/lib/docker/cli-plugins
curl -fsSL "https://github.com/docker/compose/releases/download/${COMPOSE_VERSION}/docker-compose-linux-${COMPOSE_ARCH}" -o /usr/local/lib/docker/cli-plugins/docker-compose
chmod +x /usr/local/lib/docker/cli-plugins/docker-compose

# Verify Docker Compose installation
docker compose version

echo "==> Docker installation complete"
