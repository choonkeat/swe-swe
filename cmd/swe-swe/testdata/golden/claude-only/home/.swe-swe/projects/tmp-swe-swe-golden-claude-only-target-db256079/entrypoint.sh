#!/bin/bash
set -e
trap 'echo -e "\n\033[0;31m✗ Entrypoint failed at line $LINENO (exit code $?)\033[0m" >&2' ERR

# Enterprise Certificate Installation Entrypoint
# This script installs enterprise certificates into the system trust store
# (as root) before starting the main swe-swe-server process (as app user).
#
# Usage: This is automatically called when the container starts.
# The original CMD follows after certificate installation completes.

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Install certificates if mounted (must be root for this)
if [ -d /swe-swe/certs ] && [ "$(find /swe-swe/certs -type f -name '*.pem' 2>/dev/null | wc -l)" -gt 0 ]; then
    echo -e "${YELLOW}-> Installing enterprise certificates...${NC}"

    # Copy PEM files to system CA certificate directory
    if cp /swe-swe/certs/*.pem /usr/local/share/ca-certificates/ 2>/dev/null; then
        # Update CA certificate bundle
        if update-ca-certificates; then
            echo -e "${GREEN}[ok] Enterprise certificates installed and trusted${NC}"
        else
            echo -e "${YELLOW}⚠ Warning: update-ca-certificates failed, continuing anyway${NC}"
        fi
    else
        echo -e "${YELLOW}⚠ Warning: No certificates to install${NC}"
    fi
fi







# Create Claude MCP configuration (user scope = cross-project)
# Uses claude mcp add which writes to ~/.claude.json
# Must run as app user so config goes to /home/app/.claude.json (not /root/)
# Always re-create to pick up any flag changes (e.g. --autocomplete-triggers)
su -s /bin/bash app -c '
  unset CLAUDECODE
  claude mcp remove --scope user swe-swe-agent-chat 2>/dev/null || true
  claude mcp remove --scope user swe-swe-playwright 2>/dev/null || true
  claude mcp remove --scope user swe-swe-preview 2>/dev/null || true
  claude mcp remove --scope user swe-swe-whiteboard 2>/dev/null || true
  claude mcp remove --scope user swe-swe 2>/dev/null || true
  claude mcp add --scope user --transport stdio swe-swe-agent-chat -- sh -c '"'"'exec npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY'"'"'
  claude mcp add --scope user --transport stdio swe-swe-playwright -- sh -c '"'"'exec mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT'"'"'
  claude mcp add --scope user --transport stdio swe-swe-preview -- sh -c '"'"'exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp'"'"'
  claude mcp add --scope user --transport stdio swe-swe-whiteboard -- npx -y @choonkeat/agent-whiteboard
  claude mcp add --scope user --transport stdio swe-swe -- sh -c '"'"'exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY'"'"'
'
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
chown -R app: /home/app/.swe-swe/bin
# Prepend .swe-swe/bin to PATH so shims override system commands.
# Uses /etc/profile.d/ so login shells (terminal, codex) pick it up
# after /etc/profile resets PATH.
echo 'export PATH="/home/app/.swe-swe/bin:$PATH"' > /etc/profile.d/swe-swe-path.sh
echo -e "${GREEN}[ok] Created open/xdg-open shims in .swe-swe/bin${NC}"

# Switch to app user and execute the original command
# Use exec to replace this process, preserving signal handling
exec su -s /bin/bash app -c "cd /workspace && exec $*"
