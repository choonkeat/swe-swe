# swe-swe, dockerless

> Press-release-driven doc. This describes the **end developer
> experience** of the dockerless distribution proposed in
> `tasks/2026-06-27-dockerless-single-binary.md`. It is written as if
> shipped. Not all of it works yet.

## TL;DR

swe-swe now runs on any Linux host with no Docker daemon, no compose
stack, no Go toolchain. If you already run the `claude` CLI on your
machine, you already have almost everything swe-swe needs.

```sh
npm i -g swe-swe          # or run ad hoc: npx -y swe-swe <cmd>
swe-swe init --dockerless # writes config into ./.swe-swe, nothing else
swe-swe up                # starts swe-swe on http://localhost:1977
swe-swe up --open         # ...and opens your browser
```

`swe-swe up` notices this is a dockerless init and runs everything
directly -- same command you would use for a Docker setup, it just does
the right thing. Stop it with `swe-swe down`.

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

On the browser box:

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

The choice is remembered, so later `swe-swe up` reuses it. If the backend
is unreachable, Agent View shows "unavailable" and the other tabs are
unaffected.
