#!/bin/bash
set -e
trap 'echo -e "\n\033[0;31m✗ Entrypoint failed at line $LINENO (exit code $?)\033[0m" >&2' ERR

# Container Entrypoint
# Configures MCP servers and agent tools, then starts swe-swe-server.
# In DOCKER mode, runs as root for socket permissions, then drops to app user.
# In non-DOCKER mode, runs directly as app user.

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color



echo -e "${GREEN}[ok] Created OpenCode MCP configuration${NC}"

echo -e "${GREEN}[ok] Created Codex MCP configuration${NC}"

echo -e "${GREEN}[ok] Created Gemini MCP configuration${NC}"

echo -e "${GREEN}[ok] Created Goose MCP configuration${NC}"
# Wrapper: auto-run 'goose configure' if no provider is configured
mkdir -p /home/app/.swe-swe/bin
cat > /home/app/.swe-swe/bin/goose << 'GOOSE_WRAPPER'
#!/bin/bash
GOOSE=/usr/local/bin/goose
$GOOSE "$@" || ($GOOSE configure && $GOOSE "$@")
GOOSE_WRAPPER
chmod +x /home/app/.swe-swe/bin/goose
echo -e "${GREEN}[ok] Created Goose wrapper script${NC}"

claude_mcp_setup
echo -e "${GREEN}[ok] Created Claude MCP configuration${NC}"

# Resolve internal server port (SWE_PORT for dockerfile-only mode, 9898 for compose mode)
SWE_SERVER_PORT="${SWE_PORT:-9898}"
export SWE_SERVER_PORT

# Create open/xdg-open shims that route URLs to the Preview pane
mkdir -p /home/app/.swe-swe/bin
cat > /home/app/.swe-swe/bin/swe-swe-open << 'SHIM'
#!/bin/sh
URL="${1:-}"
[ -z "$URL" ] && exit 0
curl -sf "http://localhost:$SWE_SERVER_PORT/proxy/${SESSION_UUID}/preview/__agent-reverse-proxy-debug__/open?url=$(printf '%s' "$URL" | jq -sRr @uri)" >/dev/null 2>&1 &
echo "-> Preview: $URL" >&2
SHIM
chmod +x /home/app/.swe-swe/bin/swe-swe-open
for name in xdg-open open x-www-browser www-browser sensible-browser; do
    ln -sf swe-swe-open /home/app/.swe-swe/bin/$name
done
echo -e "${GREEN}[ok] Created open/xdg-open shims in .swe-swe/bin${NC}"

# Execute the original command directly (already running as app user)
cd /workspace
exec "$@"
