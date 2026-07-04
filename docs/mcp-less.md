# MCP-less mode

This document introduces **MCP-less mode** to an agent running inside swe-swe.
If you are an AI agent and someone pointed you here, read all of it: it is the
only way you can talk to the user and reach your tools in this environment.

## The one-paragraph version

Normally an agent is its own MCP host: it reads a native MCP config
(`~/.codex/config.toml`, `~/.gemini/settings.json`, a Claude `.mcp.json`, etc.),
spawns each MCP server over stdio, and owns those pipes. In some environments
**MCP is gated but the CLI agent is not** -- the native MCP client is blocked or
absent. MCP-less mode keeps every MCP server working anyway by moving the host
out of the agent: a small daemon fronts each server behind a unix socket, and a
CLI named `mcp` (already on your PATH) lets you call any tool through your Bash
tool. **You reach tools by running `mcp ...` in a shell, not through a native
MCP client.**

## Why you must care immediately

The entire human<->agent conversation runs over one MCP server,
`swe-swe-agent-chat`. If you do not use `mcp` to reach it, the user sees a blank
chat and assumes you crashed. There is no other channel. Everything below about
`send_message` is not optional.

## The architecture (what is running)

- **`mcp-cli-proxy`** -- one daemon per MCP server. swe-swe-server launches the
  fleet per session with the exact env the agent gets. Each proxy execs its
  server verbatim, performs the MCP `initialize` handshake, holds the child's
  stdio, and exposes a single unix socket. It multiplexes concurrent callers, so
  a blocking call (like `send_message`) never stalls other calls.
- **`mcp`** -- the agent-facing client CLI, on your PATH. It discovers servers by
  listing the socket directory (the directory IS the registry), synthesizes CLI
  flags from each tool's JSON Schema, calls the tool over the socket, and prints
  the result to stdout.
- Your native MCP config is intentionally **not written** in this mode. Do not
  look for it, do not try to create one -- nothing reads it here.

The socket directory is per session, pointed to by `SWE_MCP_DIR` (falls back to
`/workspace/.swe-swe/run/mcp`). You do not need to know the path; `mcp` reads the
env var for you.

## How to reach tools

The command surface mirrors the tool id `mcp__<server>__<tool>`:

```
mcp                              # list servers (the socket dir is the registry)
mcp <server>                     # list a server's tools, each with a description
mcp <server> <tool> -h           # show a tool's flags (synthesized from its JSON Schema)
mcp <server> <tool> [--flags]    # call the tool; its result prints to stdout
```

So `mcp__swe-swe-agent-chat__send_message` becomes:

```
mcp swe-swe-agent-chat send_message --text "..." --first_quick_reply "..."
```

**Discover, do not memorize.** Server names, tool names, and flags all come from
the live sockets. Run `mcp` to see which servers this session actually has (a
terminal session has fewer than a chat session), then `mcp <server>` and
`mcp <server> <tool> -h` to learn the exact flags before calling. Do not assume a
tool exists from memory -- ask the sockets.

Flag conventions (from `-h`):
- `--flag value` or `--flag=value`.
- A boolean flag with no value means `true`.
- `array` flags are repeated: `--foo a --foo b`.
- Required flags are enforced; omitting one is an error.
- A tool that returns an image/audio result has it decoded to a file; the path is
  printed (override the dir with `SWE_MCP_IMAGE_DIR`).

## The agent-chat contract (read twice)

`swe-swe-agent-chat` is the only channel the user sees. In this environment:

- **Start each turn** by draining the queue:
  `mcp swe-swe-agent-chat check_messages`. If it returns `{"queue":"empty"}`, do
  not send a message just to say so -- resume your work or wait.
- **Every user-visible message** -- questions, status, final answers, errors,
  acknowledgments -- MUST go through
  `mcp swe-swe-agent-chat send_message --text "..." --first_quick_reply "..."`.
  Text you print to your own stdout is invisible to the user.
- **`send_message` BLOCKS** until the user replies, and the reply is RETURNED as
  the command's stdout (`User responded: ...`). That returned text IS the user's
  next message -- do not call `check_messages` again to fetch it. Never run
  `send_message` in the background; end every turn on it.
- For non-blocking status during long work, use
  `mcp swe-swe-agent-chat send_progress --text "..."`. Any barge-in the user
  sends while you work is appended to the next `send_progress` return after a
  `---BARGE-IN---` sentinel -- you do not need to poll for it.
- **Never end a turn silently.** Going quiet looks like a crash.

## Setting up your own steering

swe-swe ships first-class steering only for the `claude` agent (an appended
system prompt plus a backup rules file). If you are a different agent
(codex, gemini, goose, opencode, or something new), give yourself the same
steering by putting a block like the following into whatever persistent
instruction channel you have -- a system prompt, an `AGENTS.md`, a rules file,
a project preamble. Adapt the wording to your own idioms; keep the rules intact:

```
# MCP-less mode

This environment has NO MCP client. Reach every tool through the `mcp` CLI,
which mirrors the tool id mcp__<server>__<tool>:

    mcp                           # list servers (the socket dir is the registry)
    mcp <server>                  # list a server's tools
    mcp <server> <tool> -h        # show a tool's flags (from its JSON Schema)
    mcp <server> <tool> [--flags] # call the tool; its result prints to stdout

Talk to the user through agent-chat -- it is the ONLY channel the user sees:

- Start each turn with `mcp swe-swe-agent-chat check_messages`.
- EVERY user-visible message MUST go through
  `mcp swe-swe-agent-chat send_message --text "..." --first_quick_reply "..."`.
- send_message BLOCKS until the user replies; the reply is RETURNED as the
  command's stdout. Never background it; end every turn on it.
- Non-blocking status: `mcp swe-swe-agent-chat send_progress --text "..."`.
```

That is the whole contract. Once it is in place, bootstrap by running
`mcp swe-swe-agent-chat check_messages` and then discovering the rest of the
fleet with `mcp`.

## What not to do

- Do not write or hunt for a native MCP config -- nothing reads it here.
- Do not print user-facing text to your own stdout and assume the user saw it.
- Do not memorize a tool list; the sockets are the source of truth and vary by
  session type.
- Do not run `send_message` in the background or leave a turn without it.

## Pointers

- `cmd/mcp-cli-proxy/` -- the per-server daemon (canonical source + tests).
- `cmd/mcp/` -- the client CLI (canonical source + tests).
- `tasks/2026-07-01-mcp-less-cli-proxy.md` -- original design and rationale.
- `tasks/2026-07-03-mcp-less-default-server-launched-fleet.md` -- the decision to
  make MCP-less the default and have swe-swe-server own the per-session fleet.
