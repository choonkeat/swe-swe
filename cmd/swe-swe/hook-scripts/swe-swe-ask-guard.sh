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
