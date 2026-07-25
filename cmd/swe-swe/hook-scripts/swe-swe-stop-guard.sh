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
# Slice the transcript from the last GENUINE user message. A typed message is
# a type:user line that is not a tool_result. An agent-chat reply arrives as a
# tool_result carrying "User said:"/"User responded:" and also starts a turn --
# without that anchor, back-to-back chat replies share one turn start and an
# earlier send covers the newer turn.
n=$(awk '/"type":"user"/ {
  if (!/"tool_result"/ || /User said:|User responded:/) last = NR
} END { if (last) print last }' "$tp")
[ -n "$n" ] || exit 0
turn=$(tail -n +"$n" "$tp")
# A user-visible send already happened this turn. Read the transcript as
# structured records, not text: only a real tool_use counts, so log text that
# merely names the tool cannot spoof it. Malformed lines are skipped, not fatal.
sent=$(printf '%s\n' "$turn" | jq -r -R '
  fromjson? // empty
  | select(.type == "assistant")
  | .message.content[]?
  | select(.type == "tool_use")
  | select(
      (.name | test("agent[-_]chat__(send_message|send_progress|send_verbal_reply|send_verbal_progress|draw)$"))
      or (.name == "Bash"
          and ((.input.command // "")
               | test("(^|[;&|]|\\n)[ \t]*(mcp[ \t]+)?agent-chat[ \t]+(send_message|send_progress|send_verbal_reply|send_verbal_progress|draw)([ \t]|$)")))
    )
  | "sent"' 2>/dev/null | head -n 1)
[ -n "$sent" ] && exit 0
# A check_messages that found an empty queue is an allowed silent turn.
# (Escaped-JSON gap between the words is 5 chars: \":\" -- allow slack.)
printf '%s' "$turn" | grep -q 'queue.\{0,8\}empty' && exit 0
echo 'BLOCKED: this turn ends with no user-visible message, and the user sees only agent-chat -- your TUI responses are invisible to them. Deliver your result now via send_message (or send_progress for a non-blocking status if work continues). Note: this Stop hook is active unless AGENT_CHAT_DISABLE=1 is set.' >&2
exit 2
