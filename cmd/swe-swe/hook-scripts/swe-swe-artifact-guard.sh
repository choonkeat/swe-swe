#!/bin/sh
# swe-swe Artifact guard: the built-in Artifact tool and the claude.ai Docs
# MCP connector (mcp__claude_ai_Claude_Docs__*) publish pages to claude.ai --
# an external surface the swe-swe user is not looking at. A swe-swe session
# already has its own viewer: write the HTML into the workspace, serve it on
# $PORT, and hand the user a localhost link, which the chat UI intercepts and
# loads in the App Preview pane. Blocking here (exit 2 feeds stderr back to
# the agent) redirects it there instead of shipping workspace content
# off-box. Sessions without an agent-chat channel (terminal TUI, plain claude
# runs) are exempt, as is AGENT_CHAT_DISABLE=1 or SWE_ALLOW_ARTIFACTS=1.
[ "$AGENT_CHAT_DISABLE" = "1" ] && exit 0
[ "$SWE_ALLOW_ARTIFACTS" = "1" ] && exit 0
# Enforce only where this session actually has an agent-chat channel.
if [ -n "$SWE_MCP_DIR" ]; then
  [ -S "$SWE_MCP_DIR/swe-swe-agent-chat.sock" ] || exit 0
else
  [ -n "$AGENT_CHAT_PORT" ] || exit 0
fi
cat >&2 <<'GUARDEOF'
BLOCKED: do not publish to claude.ai -- not with the built-in Artifact tool, and not with the claude.ai Docs tools (mcp__claude_ai_Claude_Docs__*). claude.ai is not this user's surface, and publishing there sends workspace content off-box. This session has its own viewer. Instead:

1. Write the page to `mockups/<name>.html` in the workspace (create `mockups/` if needed).
2. Serve that directory on the session's $PORT with whatever static server suits the project (background it, or add a Procfile entry -- see /swe-swe:procfile).
3. Give the user a clickable link in your send_message text: `http://localhost:$PORT/<name>.html` with $PORT substituted for the real number. The chat UI intercepts localhost links and opens them in the App Preview pane.

To allow these tools, set SWE_ALLOW_ARTIFACTS=1.
GUARDEOF
exit 2
