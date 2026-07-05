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

# MCP-less mode: swe-swe-server hosts the MCP servers via mcp-cli-proxy per
# session; skip writing every agent's native MCP config below.
export SWE_MCP_LESS=1





# Create Codex MCP configuration (TOML format)
# Codex sandboxes MCP child processes and only forwards env vars listed in
# `env_vars` -- so we cannot use the `sh -c "exec npx ... $VAR"` wrapper that
# the other agents use, since $VAR would expand to empty inside the sandbox.
# Instead we run npx (or mcp-lazy-init) directly and let Codex substitute
# $VAR references in args from the declared env_vars whitelist.
# mcp-less mode skips native MCP config (swe-swe-server runs the proxy fleet).
if [ -z "$SWE_MCP_LESS" ]; then
mkdir -p /home/app/.codex
cat > /home/app/.codex/config.toml << 'EOF'
[mcp_servers.swe-swe-agent-chat]
command = "npx"
args = ["-y", "@choonkeat/agent-chat", "--theme-cookie", "swe-swe-theme", "--welcome-replies", "What can you help me with?,Give me an overview of this project,What has changed recently?,/swe-swe:recordings-list-orphaned", "--autocomplete-triggers", "/=slash-command", "--autocomplete-url", "http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY"]
env_vars = ["AGENT_CHAT_PORT", "SWE_SERVER_PORT", "SESSION_UUID", "MCP_AUTH_KEY"]

[mcp_servers.swe-swe-playwright]
command = "mcp-lazy-init"
args = ["--init-method", "POST", "--init-url", "http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY", "--", "npx", "-y", "@playwright/mcp@latest", "--cdp-endpoint", "http://localhost:$BROWSER_CDP_PORT"]
env_vars = ["SWE_SERVER_PORT", "SESSION_UUID", "MCP_AUTH_KEY", "BROWSER_CDP_PORT"]

[mcp_servers.swe-swe-preview]
command = "npx"
args = ["-y", "@choonkeat/agent-reverse-proxy", "--bridge", "http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp?key=$MCP_AUTH_KEY"]
env_vars = ["SWE_SERVER_PORT", "SESSION_UUID", "MCP_AUTH_KEY"]

[mcp_servers.swe-swe-whiteboard]
command = "npx"
args = ["-y", "@choonkeat/agent-whiteboard"]

[mcp_servers.swe-swe]
command = "npx"
args = ["-y", "@choonkeat/agent-reverse-proxy", "--bridge", "http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY"]
env_vars = ["SWE_SERVER_PORT", "MCP_AUTH_KEY"]
EOF

echo -e "${GREEN}[ok] Created Codex MCP configuration${NC}"
fi

# Create Gemini MCP configuration
# mcp-less mode skips native MCP config (swe-swe-server runs the proxy fleet).
if [ -z "$SWE_MCP_LESS" ]; then
mkdir -p /home/app/.gemini
cat > /home/app/.gemini/settings.json << 'EOF'
{
  "mcpServers": {
    "swe-swe-agent-chat": {
      "command": "sh",
      "args": ["-c", "exec npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --welcome-replies \"What can you help me with?,Give me an overview of this project,What has changed recently?,/swe-swe:recordings-list-orphaned\" --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY"]
    },
    "swe-swe-playwright": {
      "command": "sh",
      "args": ["-c", "exec mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT"]
    },
    "swe-swe-preview": {
      "command": "sh",
      "args": ["-c", "exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp?key=$MCP_AUTH_KEY"]
    },
    "swe-swe-whiteboard": {
      "command": "npx",
      "args": ["-y", "@choonkeat/agent-whiteboard"]
    },
    "swe-swe": {
      "command": "sh",
      "args": ["-c", "exec npx -y @choonkeat/agent-reverse-proxy --bridge 'http://localhost:$SWE_SERVER_PORT/mcp?key='$MCP_AUTH_KEY"]
    }
  }
}
EOF

echo -e "${GREEN}[ok] Created Gemini MCP configuration${NC}"
fi


# Create Claude MCP configuration (user scope = cross-project)
# Uses claude mcp add which writes to ~/.claude.json
# Always re-create to pick up any flag changes (e.g. --autocomplete-triggers)
# mcp-less mode skips native MCP config (swe-swe-server runs the proxy fleet).
if [ -z "$SWE_MCP_LESS" ]; then
claude_mcp_setup() {
  unset CLAUDECODE
  claude mcp remove --scope user swe-swe-agent-chat 2>/dev/null || true
  claude mcp remove --scope user swe-swe-playwright 2>/dev/null || true
  claude mcp remove --scope user swe-swe-preview 2>/dev/null || true
  claude mcp remove --scope user swe-swe-whiteboard 2>/dev/null || true
  claude mcp remove --scope user swe-swe 2>/dev/null || true
  claude mcp add --scope user --transport stdio swe-swe-agent-chat -- sh -c 'exec npx -y @choonkeat/agent-chat --theme-cookie swe-swe-theme --welcome-replies "What can you help me with?,Give me an overview of this project,What has changed recently?,/swe-swe:recordings-list-orphaned" --autocomplete-triggers /=slash-command --autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?key=$MCP_AUTH_KEY'
  claude mcp add --scope user --transport stdio swe-swe-playwright -- sh -c 'exec mcp-lazy-init --init-method POST --init-url http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY -- npx -y @playwright/mcp@latest --cdp-endpoint http://localhost:$BROWSER_CDP_PORT'
  claude mcp add --scope user --transport stdio swe-swe-preview -- sh -c 'exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/proxy/$SESSION_UUID/preview/mcp?key=$MCP_AUTH_KEY'
  claude mcp add --scope user --transport stdio swe-swe-whiteboard -- npx -y @choonkeat/agent-whiteboard
  claude mcp add --scope user --transport stdio swe-swe -- sh -c 'exec npx -y @choonkeat/agent-reverse-proxy --bridge http://localhost:$SWE_SERVER_PORT/mcp?key=$MCP_AUTH_KEY'
}
claude_mcp_setup
echo -e "${GREEN}[ok] Created Claude MCP configuration${NC}"
fi

# Guard the built-in AskUserQuestion tool. Its multiple-choice menu renders
# only in the local terminal TUI, which is invisible to a user talking through
# the web chat UI (agent-chat) -- calling it there hangs the agent forever on
# input the user can never give. This PreToolUse hook blocks the tool (exit 2,
# which feeds stderr back to the agent so it switches to the send_message MCP
# tool). The guard script self-exempts sessions with no agent-chat channel
# (terminal TUI, plain claude runs) and honors AGENT_CHAT_DISABLE=1, which
# swe-swe-server also sets for non-chat (terminal) sessions where the TUI IS
# the user surface. Fail-safe is block: a wrongly shown menu hard-hangs the
# agent, a wrongly blocked one just nudges it to send_message. Hooks are
# snapshotted at session start, so the env vars (read at tool-call time) are
# the per-session knob; these files are static.
#
# Stop guard (same philosophy at turn-end): in an agent-chat session plain
# response text is invisible, so a turn that ends without any user-visible
# send looks like a crash. The Stop hook blocks the FIRST silent stop of a
# turn (exit 2 feeds the instruction back to the agent); stop_hook_active
# guarantees the second attempt always passes, so it can never loop.
#
# Both script bodies are single-sourced from cmd/swe-swe/hook-scripts/
# (injected at init time); dockerless init writes the same files.
mkdir -p /home/app/.claude/hooks
cat > /home/app/.claude/hooks/swe-swe-stop-guard.sh << 'STOPGUARDEOF'
#!/bin/sh
# swe-swe Stop guard: in agent-chat sessions every turn must end with a
# user-visible message (send_message / send_progress / draw / send_verbal_*).
# Exit 2 blocks the stop once per turn; stderr becomes the agent's instruction.
[ "$AGENT_CHAT_DISABLE" = "1" ] && exit 0
# Enforce only where this session actually has an agent-chat channel.
if [ -n "$SWE_MCP_DIR" ]; then
  [ -S "$SWE_MCP_DIR/swe-swe-agent-chat.sock" ] || exit 0
else
  [ -n "$AGENT_CHAT_PORT" ] || exit 0
fi
command -v jq >/dev/null 2>&1 || exit 0
input=$(cat)
# One nudge per turn: when this stop was already blocked once, let it pass.
[ "$(printf '%s' "$input" | jq -r '.stop_hook_active // false')" = "true" ] && exit 0
tp=$(printf '%s' "$input" | jq -r '.transcript_path // empty')
[ -n "$tp" ] && [ -f "$tp" ] || exit 0
# Slice the transcript from the last real user message. Tool results also
# arrive as type:user lines; excluding them keeps the slice to this turn.
n=$(grep -n '"type":"user"' "$tp" | grep -v tool_result | tail -1 | cut -d: -f1)
[ -n "$n" ] || exit 0
turn=$(tail -n +"$n" "$tp")
# A user-visible send already happened this turn (mcp CLI or native MCP id).
printf '%s' "$turn" | grep -q \
  -e 'agent-chat send_message' -e 'agent-chat__send_message' \
  -e 'agent-chat send_progress' -e 'agent-chat__send_progress' \
  -e 'send_verbal_reply' -e 'send_verbal_progress' \
  -e 'agent-chat draw' -e 'agent-chat__draw' && exit 0
# A check_messages that found an empty queue is an allowed silent turn.
# (Escaped-JSON gap between the words is 5 chars: \":\" -- allow slack.)
printf '%s' "$turn" | grep -q 'queue.\{0,8\}empty' && exit 0
echo 'BLOCKED: this turn ends with no user-visible message, and the user sees only agent-chat -- your TUI responses are invisible to them. Deliver your result now via mcp swe-swe-agent-chat send_message (or send_progress for a non-blocking status if work continues). Note: this Stop hook is active unless AGENT_CHAT_DISABLE=1 is set.' >&2
exit 2
STOPGUARDEOF
chmod +x /home/app/.claude/hooks/swe-swe-stop-guard.sh
cat > /home/app/.claude/hooks/swe-swe-ask-guard.sh << 'ASKGUARDEOF'
#!/bin/sh
# swe-swe AskUserQuestion guard: the built-in question tool's menu renders
# only in the local terminal TUI. In an agent-chat session the user may never
# see it, and the agent hangs forever on input that cannot arrive -- block it
# and point at send_message. Sessions without an agent-chat channel (terminal
# TUI, plain claude runs) are exempt, as is AGENT_CHAT_DISABLE=1.
[ "$AGENT_CHAT_DISABLE" = "1" ] && exit 0
# Enforce only where this session actually has an agent-chat channel.
if [ -n "$SWE_MCP_DIR" ]; then
  [ -S "$SWE_MCP_DIR/swe-swe-agent-chat.sock" ] || exit 0
else
  [ -n "$AGENT_CHAT_PORT" ] || exit 0
fi
echo 'BLOCKED: do not use the built-in AskUserQuestion tool -- its menu renders only in the local TUI, which the user may not see (e.g. an agent-chat session). Ask via the agent-chat send_message tool instead (question -> text, primary option -> first_quick_reply, rest -> more_quick_replies). To allow the built-in tool, set AGENT_CHAT_DISABLE=1.' >&2
exit 2
ASKGUARDEOF
chmod +x /home/app/.claude/hooks/swe-swe-ask-guard.sh
CLAUDE_SETTINGS=/home/app/.claude/settings.json
cat > /tmp/swe-claude-settings.json << 'SETTINGSEOF'
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "AskUserQuestion",
        "hooks": [
          {
            "type": "command",
            "command": "/home/app/.claude/hooks/swe-swe-ask-guard.sh"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "/home/app/.claude/hooks/swe-swe-stop-guard.sh"
          }
        ]
      }
    ]
  }
}
SETTINGSEOF
if [ -s "$CLAUDE_SETTINGS" ] && command -v jq >/dev/null 2>&1; then
  # Merge idempotently into existing settings: drop any prior AskUserQuestion
  # matcher and any prior swe-swe-stop-guard Stop entry, append ours, preserve
  # every other key and hook entry.
  TMP_SETTINGS=$(mktemp)
  if jq --slurpfile add /tmp/swe-claude-settings.json \
       '.hooks.PreToolUse = (((.hooks.PreToolUse // []) | map(select(.matcher != "AskUserQuestion"))) + ($add[0].hooks.PreToolUse)) | .hooks.Stop = (((.hooks.Stop // []) | map(select(((.hooks // []) | map(.command // "") | join(" ")) | contains("swe-swe-stop-guard") | not))) + ($add[0].hooks.Stop))' \
       "$CLAUDE_SETTINGS" > "$TMP_SETTINGS" 2>/dev/null; then
    mv "$TMP_SETTINGS" "$CLAUDE_SETTINGS"
  else
    # Existing file was not valid JSON; overwrite with our fragment.
    rm -f "$TMP_SETTINGS"
    cp /tmp/swe-claude-settings.json "$CLAUDE_SETTINGS"
  fi
elif [ -s "$CLAUDE_SETTINGS" ]; then
  # File exists but jq is unavailable: do not risk clobbering it.
  echo -e "${YELLOW}[warn] jq unavailable; left existing ~/.claude/settings.json untouched (AskUserQuestion guard not installed)${NC}"
else
  cp /tmp/swe-claude-settings.json "$CLAUDE_SETTINGS"
fi
rm -f /tmp/swe-claude-settings.json

echo -e "${GREEN}[ok] Installed AskUserQuestion + silent-stop guard hooks${NC}"

# MCP-less steering: with no native MCP client, the agent must reach every tool
# through the `mcp` CLI (sockets in $SWE_MCP_DIR, one per server). The blocking
# send_message contract is the load-bearing rule -- run it, wait, and treat its
# stdout as the user's reply. Written to ~/.claude/CLAUDE.md (user memory).
if [ -n "$SWE_MCP_LESS" ]; then
mkdir -p /home/app/.claude
cat > /home/app/.claude/CLAUDE.md << 'MCPLESSEOF'
# MCP-less mode

This environment has NO MCP client. Reach every tool through the `mcp` CLI.
Run `mcp -h` FIRST -- and again after any context compaction. It prints the
full documentation for every server and tool (what a native MCP client would
inject into your context automatically). Never guess flags.

Talk to the user through agent-chat -- it is the ONLY channel the user sees:

- Start each turn with `mcp swe-swe-agent-chat check_messages`.
- EVERY user-visible message MUST go through send_message, following its
  documentation from `mcp -h` exactly.
- `send_message` BLOCKS until the user replies; the reply is RETURNED as the
  command's stdout. Never background it; end every turn on it.
- Non-blocking status: `mcp swe-swe-agent-chat send_progress --text "..."`.

Once the task at hand is clear (and when it changes), name this session so the
user can tell sessions apart: see `mcp swe-swe set_session_name -h`.
MCPLESSEOF

echo -e "${GREEN}[ok] Installed MCP-less agent steering (~/.claude/CLAUDE.md)${NC}"
fi


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
