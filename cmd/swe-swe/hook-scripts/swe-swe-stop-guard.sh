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
