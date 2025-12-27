# swe-swe development environment
# Based on golang image with Node.js and AI assistant tools

FROM golang:1.23-bookworm

# Install base dependencies
RUN apt-get update && apt-get install -y \
    curl \
    wget \
    git \
    build-essential \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install Node.js from Debian repositories (simpler than nvm in Docker)
RUN apt-get update && apt-get install -y \
    nodejs \
    npm \
    && rm -rf /var/lib/apt/lists/*

# Install Python and pip for AI assistants
RUN apt-get update && apt-get install -y \
    python3 \
    python3-pip \
    && rm -rf /var/lib/apt/lists/*

# Ensure npm global packages are in PATH
ENV PATH="/usr/local/lib/node_modules/.bin:${PATH}"

# Install AI assistant CLIs
RUN npm install -g @anthropic-ai/claude-code

# Install MCP Playwright for browser automation
# This enables the system to control browsers via Playwright CDP connections
RUN npm install -g @playwright/mcp

# Install aider
RUN pip3 install --break-system-packages aider-chat

# Install Goose (AI pair programmer from Block)
RUN curl -fsSL https://github.com/block/goose/releases/download/stable/download_cli.sh | GOOSE_BIN_DIR=/usr/local/bin CONFIGURE=false bash

# Enterprise certificate support
# swe-swe init detects NODE_EXTRA_CA_CERTS/SSL_CERT_FILE env vars and copies certs
# to .swe-swe/certs/. The entrypoint.sh installs them into the system trust store.

# Copy entrypoint script (binary is volume-mounted from host)
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

# Create non-root user for running swe-swe-server
RUN useradd -m -s /bin/bash app

# Entrypoint runs as root to install certificates,
# then switches to app user to run swe-swe-server
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

# Default working directory (entrypoint will cd to /workspace for the server)
WORKDIR /workspace

# Default command: run swe-swe-server pointing to /workspace
CMD ["/usr/local/bin/swe-swe-server", "-working-directory", "/workspace", "-addr", "0.0.0.0:9898"]
