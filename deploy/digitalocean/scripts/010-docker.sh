#!/bin/bash
# Install Docker Engine and Docker Compose plugin with recent versions
set -euo pipefail

echo "==> Installing Docker Engine..."

# Update apt cache
apt-get update

# Install Docker using the convenience script (installs latest version)
curl -fsSL https://get.docker.com | sh

# Upgrade Docker to ensure we have API version 1.44+ (required by traefik v3)
echo "==> Upgrading Docker to latest version..."
apt-get install -y --only-upgrade docker-ce docker-ce-cli containerd.io

# Verify Docker installation
docker --version

# Ensure Docker daemon is started and enabled
systemctl start docker
systemctl enable docker

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
