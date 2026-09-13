# swe-swe, dockerless

> Shipped. Design notes and the remaining phases live in
> `tasks/2026-06-27-dockerless-single-binary.md`; the Mac-VM path is
> verified end-to-end (see `dockerless-mac-vm.md`). swe-swe running
> natively on macOS with no VM is still pending (Phase 6).

## TL;DR

swe-swe runs on any Linux host with no Docker daemon, no compose
stack, no Go toolchain. If you already run the `claude` CLI on your
machine, you already have almost everything swe-swe needs.

```sh
npm i -g swe-swe             # or run ad hoc: npx -y swe-swe <cmd>
swe-swe init --runtime=host  # writes ./.swe-swe + project integration files
swe-swe up                   # starts swe-swe on http://localhost:1977
swe-swe up --open            # ...and opens your browser
```

Init writes its runtime payload (the server and helper binaries) under
`./.swe-swe`, plus two project integration files so the agent is wired up
the way the container image wires it: a project-scoped `.mcp.json`, and
`.claude/settings.local.json` with the hook guards. Nothing lands in your
global `~/.claude`.

`swe-swe up` notices this is a dockerless init and runs everything
directly -- same command you would use for a Docker setup, it just does
the right thing. It runs in the **foreground**: stop it with Ctrl-C.
(`swe-swe down` only prints that reminder, and the other compose
pass-through commands -- `build`, `ps`, `logs`, `exec` -- are rejected.)

Any extra arguments to `swe-swe up` are passed straight through to
`swe-swe-server`, which is how the `--agent-view` options below reach it.

On a Mac? See `dockerless-mac-vm.md` for the Linux-VM + browser-backend
-container recipe.

## Dependencies

swe-swe detects what is on your PATH at startup and uses it -- there is
nothing to declare at init. Provide:

- **git**
- **at least one coding-agent CLI** -- one or more of `claude`,
  `gemini`, `codex`, `goose`, `aider`, `opencode`, `pi`. Install the one
  you want; swe-swe offers whatever it finds.
- **node/npx** (optional) -- only for the Agent View tab
  (`@playwright/mcp` is real JS) and for agent CLIs that are themselves
  node programs (`claude`, `gemini`). A non-node agent CLI on a box with
  no node at all still gets Agent Terminal, Terminal, Preview, Files,
  and Agent Chat.
- **browser stack** (optional) -- only for the Agent View tab; see below.

swe-swe's own npm-published tools (`@choonkeat/md-serve`,
`@choonkeat/agent-chat`, `@choonkeat/agent-reverse-proxy`) are static Go
binaries; the bundled
`swe-npx` helper resolves each one straight from the npm registry
(honoring `dist-tags.latest` with a 15m memo) and caches the platform
binary under the user-level `~/.swe-swe/npx-cache/`, shared across
sessions and projects. First use downloads once; after that spawns are
instant and never consult the project cwd -- so a session working inside
a checkout of one of those very repos cannot shadow the tool.

## MCP-less mode

Some hosts let you run the `claude` CLI but not its MCP client: a
managed Claude Code that ignores project `.mcp.json`, a sandbox that
only allows an allowlisted set of servers. Every swe-swe pane (Agent
Chat above all) is an MCP server, so on such a host a session boots but
the agent can never reach the user. Init with `--without-mcp`:

```sh
swe-swe init --runtime=host --without-mcp
swe-swe up
```

No `.mcp.json` is written (one from an earlier init is retired). Instead
`swe-swe up` exports `SWE_MCP_LESS=1` and, per session, `swe-swe-server`
launches one `mcp-cli-proxy` per MCP server (agent-chat, playwright,
preview, swe-swe), each serving a unix socket under
`$TMPDIR/swe-swe-<uid>/mcp/<session>/` (short on purpose: unix socket
paths cap at 108 bytes). The agent reaches them through the bundled
`mcp` CLI on its PATH (`SWE_MCP_DIR` names the session's socket dir):

```sh
mcp -h                                            # full docs, every server and tool
mcp swe-swe-agent-chat check_messages
mcp swe-swe-agent-chat send_message --text "..."  # blocks until the user replies
```

Claude is told the contract through a fenced block the server maintains
in the session workDir's `CLAUDE.local.md` (Claude Code's machine-local
project memory; never committed), and the Stop / AskUserQuestion /
Artifact hook guards recognise the CLI form, so the agent-chat
discipline is the same as with native MCP. Re-init without the flag and
the block is removed on the next session. Only Claude gets the steering
today; the proxies serve every agent.

## Ephemeral host: no secrets at boot

A sandbox that is rebuilt from an init script and cannot hold secrets can
still run swe-swe behind a tunnel. Boot with no tunnel config at all:

```sh
npx -y swe-swe init --runtime=host --without-mcp
SWE_SWE_PASSWORD=... SWE_AGENT_VIEW=off npx -y swe-swe up
```

Open the homepage through whatever single-port door the sandbox gives you,
open Settings, and paste into **Tunnel secrets**:

```
SWE_TUNNEL_SERVER_URL=https://tunnel.example.com
SWE_TUNNEL_UNIQUE=my-box
SWE_TUNNEL_IDENTITY_KEY=<base64 -w0 < identity.key>
```

Apply: the dot on the settings gear turns amber while the tunnel
connects and green once it is up; the stable
`https://1977.my-box-tunnel.<suffix>/` link then appears in the same
Settings pane with a Copy button -- the same URL on every rebuild, since
it depends only on the unique. Switch to it; every
pane works there. The secrets never leave the server process, and no
session inherits them. The **Session environment** pane next to it is the
place for values sessions should inherit. See
[configuration.md](configuration.md#homepage-settings-tunnel-secrets-and-session-environment).

## Browser stack (Agent View)

Agent View shows a live, agent-drivable Chromium over VNC. It is the one
feature with a heavy, non-bundleable dependency, so you pick where it
runs.

### Option A -- co-located (default)

Install the browser stack on the same host and Agent View just works:

```sh
# Debian/Ubuntu
sudo apt-get install -y chromium xvfb x11vnc novnc websockify
swe-swe up
```

swe-swe starts a per-session Chromium on demand the first time Agent View
is opened. No browser processes run until then.

### Option B -- browser on another box

If your host has no display stack (a slim VM, a laptop you would rather
keep clean), run the browser backend somewhere else and point swe-swe at
it. The other tabs stay fully local; only the browser is offloaded.

On the browser box, with no Docker and no source checkout (Linux x86-64 or
arm64; the display stack is X11):

```sh
curl -fsSL https://raw.githubusercontent.com/choonkeat/swe-swe/main/install.sh | sh
sudo apt-get install -y chromium xvfb x11vnc novnc websockify
SWE_BROWSER_BACKEND_TOKEN=some-shared-secret swe-swe browser-backend
```

`swe-swe browser-backend` extracts `swe-swe-server` to
`~/.swe-swe/browser-backend/bin/` and execs it with `-mode browser-backend`,
forwarding every other argument. It listens on `:9333`; pass `-bind`/`-addr`
to change that. `SWE_BIND`, `SWE_PORT` and `PORT` are deliberately ignored
here, so a variable the box exports for something else cannot move or break
the service. It refuses to start, naming the missing packages, if the display
stack is absent. This machine runs no sessions, so it is not a project and
never appears in `swe-swe list`.

Or with Docker (build the image first with `make browser-backend-image`; see
`dockerless-mac-vm.md` for the full invocation):

```sh
docker run -p 9333:9333 swe-swe/browser-backend
```

On your host:

```sh
swe-swe up --agent-view=https://browser-box.internal:9333
```

`--agent-view` accepts:

| Value                | Meaning                                        |
|----------------------|------------------------------------------------|
| `local` (default)    | use the host browser stack                     |
| `<url>`              | offload to a remote browser backend at `<url>` |
| `off`               | disable the Agent View pane                     |

`SWE_AGENT_VIEW` sets the same thing via the environment, so you can export
it once instead of repeating the flag. If the backend is unreachable, Agent
View shows "unavailable" and the other tabs are unaffected.

#### Tunnel variant -- your box can stay fully firewalled

Direct mode (above) needs the browser box to reach your host's ports:
chromium there is told, via `--host-resolver-rules`, to resolve
`localhost` / `*.lvh.me` / `*.localtest.me` back to your host's IP. If
your box sits behind NAT or a strict firewall, add `--agent-view-tunnel`
(env `SWE_AGENT_VIEW_TUNNEL=1`):

```sh
swe-swe up --agent-view=https://browser-box.internal:9333 --agent-view-tunnel
```

Now **swe-swe connects out** -- the same trust direction as swe-swe-tunnel
-- and the browser box needs zero inbound route to you. Per session,
swe-swe dials a WebSocket to the backend and keeps a declarative set of
ports synced; the backend binds real listeners for them on ITS OWN
loopback, so chromium there needs no resolver rules at all: `localhost`
and `*.lvh.me` resolve natively, the Host header arrives intact (vhost
previews route normally), and every accepted connection is shuttled down
the tunnel and replayed against your host's `127.0.0.1:<same port>`.

Which ports follow the session automatically:

- **Static**: the swe-swe server port and the session's preview port --
  always bound.
- **Procfile**: ports of services declared in your `Procfile` (the
  `swe-run` assignments) -- pre-bound at tunnel start, so a declared
  service is reachable before it even starts listening.
- **Auto-mirror**: any ad-hoc listener on your host's loopback (an
  `npm run dev` you just started) is discovered from `/proc/net/tcp`
  within ~2s and appears on the backend automatically; when it exits, the
  port is dropped. `SWE_AGENT_VIEW_TUNNEL_EXCLUDE_PORTS` (CSV of ports /
  `lo-hi` ranges) overrides the default exclusion of swe-swe's own
  internal per-session pools.

Collisions are refused loudly, never silently: the backend's own service
and CDP/VNC ports are reserved, and across sessions the first bind wins
(losers get a warning in the server log). On the backend, accepted
connections are peer-checked (Linux `/proc` ancestry) so only that
session's chromium can use the tunnel's listeners. If the tunnel drops,
the session keeps running -- Agent View pages fail until the automatic
reconnect (capped backoff) restores it. `SWE_AGENT_VIEW_LOCALHOST` /
`SWE_AGENT_VIEW_LOOPBACK_DOMAINS` are resolver-rule knobs and are ignored
in tunnel mode.

## Reaching it from a browser

Three shapes, in order of how much work they are. `swe-swe up` with nothing
extra is the first one.

### Option 1 -- every port is reachable (default)

Your own laptop, a machine on your network, or a private network such as
Tailscale. Each session gets its own port for the app preview and they all just
work. Nothing to configure.

### Option 2 -- one fixed port, with wildcard subdomains

For a box you reach on exactly one port, where `anything.<its hostname>` also
reaches it. Then swe-swe serves every pane through that ONE port, naming the
internal port in the leftmost label -- what tunnel mode does, minus the tunnel:

```
http://1977.example.com:1977/    the swe-swe UI
http://23000.example.com:1977/   that session's app preview
http://27000.example.com:1977/   that session's Agent View
```

```sh
swe-swe up --public-hostname=example.com     # env: SWE_PUBLIC_HOSTNAME
```

The connection port never changes; only the leftmost label does, and it names
the local port to reach. Running locally, `lvh.me` already behaves this way
(`anything.lvh.me` resolves to `127.0.0.1`), which is handy for trying it.

Checking whether your box qualifies: in a browser **on the machine you will be
using**, open `anything.<the box's hostname>`. If it reaches the box, it does.

This works with Docker too -- see [ADR-0046](adr/0046-wildcard-host-demux.md)
for why that took a re-test to establish. Under compose it also collapses the
~100 published per-session ports into the one.

Two things to know:

1. **Plain http.** swe-swe-server never terminates TLS. For a padlock,
   whatever does terminate it in front needs a certificate covering every
   subdomain, and a normal single-name certificate will not do.
2. **Mutually exclusive with tunnel mode**, because both decide the public
   hostname. If a stale `SWE_TUNNEL_SERVER_URL` is sitting in the box's
   environment, an explicit `--public-hostname` on the command line wins and
   says so in the log.

Only swe-swe's own per-session proxy ports are routable this way
(`proxyPortOffset` plus each configured range). Any other port is a 404, logged
once, so a public hostname cannot become a door into everything else on the
box.

### Option 3 -- one fixed port, no wildcard subdomains

A tunnel is then the only route: see [tunnel-explained.md](tunnel-explained.md).
It supplies the wildcard subdomains option 2 needs, at the cost of a tunnel
server you have to run.
