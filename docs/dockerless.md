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
npm i -g swe-swe          # or run ad hoc: npx -y swe-swe <cmd>
swe-swe init --dockerless # writes config + binaries into ./.swe-swe, nothing else
swe-swe up                # starts swe-swe on http://localhost:1977
swe-swe up --open         # ...and opens your browser
```

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
`@choonkeat/agent-chat`, `@choonkeat/agent-whiteboard`,
`@choonkeat/agent-reverse-proxy`) are static Go binaries; the bundled
`swe-npx` helper resolves each one straight from the npm registry
(honoring `dist-tags.latest` with a 15m memo) and caches the platform
binary under the user-level `~/.swe-swe/npx-cache/`, shared across
sessions and projects. First use downloads once; after that spawns are
instant and never consult the project cwd -- so a session working inside
a checkout of one of those very repos cannot shadow the tool.

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

On the browser box (build the image first with `make browser-backend-image`;
see `dockerless-mac-vm.md` for the full invocation):

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
