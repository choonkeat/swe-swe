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


# Copy slash commands to agent directories
if [ -d "/home/app/.swe-swe/commands/md/ck/.git" ]; then
    # Try to pull updates (best effort)
    git config --global --add safe.directory /home/app/.swe-swe/commands/md/ck 2>/dev/null || true
    (cd /home/app/.swe-swe/commands/md/ck && git pull) 2>/dev/null && \
        echo -e "${GREEN}[ok] Updated slash commands: ck (swe-swe store)${NC}" || \
        echo -e "${YELLOW}⚠ Could not update slash commands: ck (swe-swe store)${NC}"
elif [ -d "/tmp/slash-commands/ck" ]; then
    mkdir -p "$(dirname "/home/app/.swe-swe/commands/md/ck")"
    cp -r /tmp/slash-commands/ck /home/app/.swe-swe/commands/md/ck
    echo -e "${GREEN}[ok] Installed slash commands: ck (swe-swe store)${NC}"
fi
if [ -d "/home/app/.swe-swe/commands/md/ck" ]; then
    mkdir -p "/home/app/.pi/agent/prompts"
    # Prune links from an earlier bundle before re-linking, so a command this
    # repo renamed or removed does not linger in pi's autocomplete.
    find "/home/app/.pi/agent/prompts" -maxdepth 1 -type l -name 'ck-*.md' -delete 2>/dev/null || true
    # The pre-flat design linked the repo in as one directory, which pi never
    # read. Drop it so it cannot double every command if pi learns to recurse.
    if [ -L "/home/app/.pi/agent/prompts/ck" ]; then rm -f "/home/app/.pi/agent/prompts/ck"; fi
    for f in "/home/app/.swe-swe/commands/md/ck"/*.md; do
        [ -e "$f" ] || continue
        piLink="/home/app/.pi/agent/prompts/ck-$(basename "$f")"
        if [ -e "$piLink" ] && [ ! -L "$piLink" ]; then
            echo -e "${YELLOW}⚠ Slash command target exists and is not a symlink, leaving unchanged: $piLink (pi)${NC}"
        else
            ln -sfn "$f" "$piLink"
        fi
    done
    echo -e "${GREEN}[ok] Linked slash commands: ck (pi)${NC}"
fi







# Install Pi mcp-bridge extension into the global Pi config dir so every
# session in every workspace gets the swe-swe / agent-chat / playwright /
# preview / whiteboard MCPs without per-workspace setup. Pi prefers a
# project-local .pi/extensions/ override, so /workspace can still drop a
# custom mcp-bridge.ts to hack on it.
mkdir -p /home/app/.pi/agent/extensions
cp /tmp/pi-mcp-bridge.ts /home/app/.pi/agent/extensions/mcp-bridge.ts

echo -e "${GREEN}[ok] Installed Pi mcp-bridge extension${NC}"

# Resolve internal server port. SWE_PORT is set by both compose (via the
# swe-swe service environment block) and dockerfile-only mode (via ENV in
# the generated Dockerfile), so the default is the same in either mode.
SWE_SERVER_PORT="${SWE_PORT:-1977}"
export SWE_SERVER_PORT

# Create open/xdg-open shims that route URLs to the Preview pane
mkdir -p /home/app/.swe-swe/bin
cat > /home/app/.swe-swe/bin/swe-swe-open << 'SHIM'
#!/bin/sh
URL="${1:-}"
[ -z "$URL" ] && exit 0
curl -sf "http://localhost:$SWE_SERVER_PORT/proxy/${SESSION_UUID}/preview/__agent-reverse-proxy-debug__/open?url=$(printf '%s' "$URL" | jq -sRr @uri)&key=$MCP_AUTH_KEY" >/dev/null 2>&1 &
echo "-> Preview: $URL" >&2
SHIM
chmod +x /home/app/.swe-swe/bin/swe-swe-open
for name in xdg-open open x-www-browser www-browser sensible-browser; do
    ln -sf swe-swe-open /home/app/.swe-swe/bin/$name
done
echo -e "${GREEN}[ok] Created open/xdg-open shims in .swe-swe/bin${NC}"

# Execute the original command directly (already running as app user)
# Use sh -c to expand shell variables in CMD arguments (e.g. ${SWE_PORT:-1977})
cd /workspace
exec sh -c "$*"
