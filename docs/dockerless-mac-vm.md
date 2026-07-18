# swe-swe on a Mac: Linux VM + browser-backend container

> Press-release-driven doc, companion to `dockerless.md`. It describes the
> end state after `tasks/2026-07-18-swe-npx-node-free-helpers.md` and
> `tasks/2026-07-18-agent-view-reverse-tunnel.md` land. Sections marked
> **works today** need neither task; the rest says which task it waits on.

## Topology

The Mac runs no swe-swe code directly (mac-native is Phase 6, separately).
Instead:

```
Mac browser (you)
  |  http://localhost:1977          (Lima auto-forwards VM listeners)
  v
Linux VM (Lima/Multipass) ---- swe-swe, dockerless: server + agents + your repos
  |  outbound only: allocation API, CDP, VNC, reverse tunnel
  v
browser-backend container (Docker Desktop / OrbStack on the Mac)
      chromium + Xvfb + x11vnc + websockify   <- the Agent View heavy stack
```

Everything heavy-and-Linuxy (the display stack) lives in a container the Mac
already knows how to run; everything stateful (your code, sessions, agents)
lives in a plain Ubuntu VM on the fully-verified Linux path; the Mac stays
clean.

## 1. Build the browser-backend image (works today)

On the Mac, from a checkout of this repo (needs Go 1.24+ and Docker):

```sh
make browser-backend-image
# = make dockerless-payload DOCKERLESS_OS=linux
#   docker build -f docker/browser-backend/Dockerfile -t swe-swe/browser-backend .
```

Apple Silicon note: the payload arch follows your host (`arm64`); the image
runs as linux/arm64, which is what Docker Desktop prefers anyway.

## 2. Create the Linux VM (works today)

Lima is the smoothest because it auto-forwards every listener the VM opens
(including loopback-bound ones) to the Mac's 127.0.0.1 -- which is exactly
what a loopback-binding dockerless server wants:

```sh
limactl start --name swe template://ubuntu-lts
limactl shell swe
```

(Multipass/UTM work too; you then manage port-forwards for 1977 and any
preview ports yourself.)

## 3. Install swe-swe in the VM (works today)

```sh
sudo apt-get update && sudo apt-get install -y git
# node: needed today for npx-launched helpers; after the swe-npx task it is
# only needed if you use a node-based agent CLI (claude) or Agent View's
# @playwright/mcp driver. npm is also how swe-swe itself installs.
curl -fsSL https://deb.nodesource.com/setup_22.x | sudo bash - && sudo apt-get install -y nodejs
npm i -g swe-swe @anthropic-ai/claude-code
```

## 4. Start the backend on the Mac (works today)

```sh
docker run --rm --name swe-browser \
    -p 9333:9333 -p 6000-6019:6000-6019 -p 7000-7039:7000-7039 \
    -e SWE_BROWSER_BACKEND_TOKEN=pick-a-shared-secret \
    swe-swe/browser-backend
```

## 5. Init + up in the VM

```sh
cd ~/your-project
swe-swe init --dockerless
SWE_BROWSER_BACKEND_TOKEN=pick-a-shared-secret \
    swe-swe up --agent-view=http://host.lima.internal:9333
```

`host.lima.internal` is Lima's name for the Mac; the VM dials out to the
backend's API and to the CDP/VNC ports it returns. The choice is remembered
for later `swe-swe up`.

Then on the Mac: open <http://localhost:1977>. Lima has already forwarded
the VM's loopback listener. Preview ports your sessions open (3000, 8080,
...) get the same automatic forwarding, so `http://myapp.lvh.me:3000` in
your Mac browser resolves to Mac-loopback and lands in the VM. That's the
user-facing half done -- all six tabs.

### 5a. Agent View page traffic, direct mode (works today, one override)

In direct mode, chromium *in the backend container* must reach dev servers
*in the VM*. The default guess (allocation source address) is a Docker NAT
IP that does not route back, so override it with Docker's name for the Mac,
and let the Mac's Lima forwards complete the chain
(container -> host.docker.internal -> Mac loopback -> VM):

```sh
SWE_AGENT_VIEW_LOCALHOST=host.docker.internal \
SWE_BROWSER_BACKEND_TOKEN=pick-a-shared-secret \
    swe-swe up --agent-view=http://host.lima.internal:9333
```

Sanity check if a page won't load in Agent View:
`docker exec swe-browser curl -sI http://host.docker.internal:1977` should
return an HTTP status. If it does, the chain is intact.

### 5b. Agent View, tunnel mode (after the reverse-tunnel task)

The override dance above disappears:

```sh
SWE_BROWSER_BACKEND_TOKEN=pick-a-shared-secret \
    swe-swe up --agent-view=http://host.lima.internal:9333 --agent-view-tunnel
```

The VM dials out; the backend binds VM ports on its own loopback and
shuffles traffic back over that connection. No inbound path to the VM is
needed at all, no `SWE_AGENT_VIEW_LOCALHOST`, no NAT reasoning. This is the
recommended mode once available, and the only workable one if the VM ever
moves somewhere the backend cannot reach (another machine, a firewalled
cloud box reached via swe-swe-tunnel).

## 6. Daily use

```sh
limactl shell swe -- swe-swe up      # morning
limactl shell swe -- swe-swe down    # evening; docker stop swe-browser too
```

Upgrades: `npm update -g swe-swe` in the VM, re-run `swe-swe init
--dockerless`; rebuild the backend image from a fresh repo checkout when it
changes.

## What waits on what

| Piece | Status |
|---|---|
| VM + dockerless server, 5 tabs, previews on lvh.me | works today |
| Agent View via backend container, direct mode + `SWE_AGENT_VIEW_LOCALHOST` | works today |
| Agent View with zero VM-inbound (`--agent-view-tunnel`) | reverse-tunnel task |
| node-free VM (non-node agent, no Agent View) | swe-npx task |
| swe-swe natively on macOS, no VM | dockerless Phase 6 (code-complete, unverified) |
