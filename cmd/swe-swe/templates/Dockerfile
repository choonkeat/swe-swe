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

# Install AI assistant CLIs (best-effort - don't fail if unavailable)
RUN npm install -g \
    @anthropic-ai/claude-code \
    @openai/codex \
    @google/gemini-cli \
    2>/dev/null || true

# Install MCP Playwright for browser automation
# This enables the system to control browsers via Playwright CDP connections
RUN npm install -g @anthropic-ai/mcp-playwright 2>/dev/null || echo "Warning: MCP Playwright installation failed"

RUN pip3 install --break-system-packages \
    aider-chat

# Install Goose (AI pair programmer from Block)
# Note: Goose installation can fail due to:
#   - GitHub CDN issues (HTTP 504 errors - intermittent)
#   - SSL/certificate issues (especially if behind corporate proxy)
# The installation is optional - if it fails, aider and npm-based assistants will still work
#
# If you're behind a corporate firewall and curl fails with SSL errors:
# 1. Extract the swe-swe project and modify this Dockerfile
# 2. Change curl to: curl --cacert /swe-swe/certs/ca.pem -fsSL ... (if you have certs)
# 3. Or add --insecure flag (less secure): curl -k -fsSL ...
# 4. Or install goose manually after swe-swe up completes
RUN curl -fsSL https://github.com/block/goose/releases/download/stable/download_cli.sh | CONFIGURE=false bash || echo "Warning: Goose installation failed (may be GitHub CDN issue or SSL certificate issue - try again later)"

# Enterprise certificate support
# If you're behind a corporate firewall or VPN (Cloudflare Warp, etc):
# 1. swe-swe init automatically detects NODE_EXTRA_CA_CERTS and SSL_CERT_FILE env vars
# 2. Certificates are copied to .swe-swe/certs/ directory
# 3. Certificates are mounted into this container at /swe-swe/certs (read-only)
# 4. The entrypoint.sh script automatically installs certificates into the system
#    trust store (/etc/ssl/certs) when the container starts
# 5. This enables tools like curl, npm, Node.js, and apt-get to trust custom CAs
#
# The entrypoint.sh script:
# - Checks if /swe-swe/certs contains any PEM certificates
# - Copies them to /usr/local/share/ca-certificates/
# - Runs update-ca-certificates to install them into the system trust store
# - Then executes the original command (swe-swe-server)

# To add more tools, uncomment or add below:
# Install Python
# RUN apt-get update && apt-get install -y python3 python3-pip && rm -rf /var/lib/apt/lists/*

# Install Rust (optional - uncomment to enable)
# RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
# ENV PATH="${PATH}:/root/.cargo/bin"

# Install Erlang (optional - uncomment to enable)
# RUN apt-get update && apt-get install -y erlang && rm -rf /var/lib/apt/lists/*

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
