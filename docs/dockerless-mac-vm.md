# swe-swe on a Mac: Linux VM + browser-backend container

> Press-release-driven doc, companion to `dockerless.md`. Both tasks it
> waited on (`tasks/2026-07-18-swe-npx-node-free-helpers.md` and
> `tasks/2026-07-18-agent-view-reverse-tunnel.md`) have landed on main;
> everything below works from a current build. Only mac-native (no VM) is
> still pending, as dockerless Phase 6.

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

## 2. Start the backend on the Mac (works today)

Same track as step 1 -- this is the whole browser side done:

```sh
docker run --rm --name swe-browser \
    -p 9333:9333 -p 6000-6019:6000-6019 -p 7000-7039:7000-7039 \
    -e SWE_BROWSER_BACKEND_TOKEN=pick-a-shared-secret \
    swe-swe/browser-backend
```

If your Mac sets `NODE_EXTRA_CA_CERTS` (corporate TLS interception -- see
3a), add this line to the `docker run` so chromium inside the container
trusts the proxy too (the entrypoint imports it into the system bundle and
chromium's NSS store):

```sh
    -v "$NODE_EXTRA_CA_CERTS:/corp-ca.crt:ro" \
```

## 3. Create the Linux VM (works today)

Lima is the smoothest because it auto-forwards every listener the VM opens
(including loopback-bound ones) to the Mac's 127.0.0.1 -- which is exactly
what a loopback-binding dockerless server wants:

```sh
limactl start --name swe --mount-writable template:ubuntu-lts
limactl shell swe
```

`--mount-writable` matters: Lima mounts your Mac home into the VM read-only
by default, and `swe-swe init` must write into your project directory
(`.mcp.json`, `swe-swe/`, ...). For an already-created VM:

```sh
limactl stop swe
limactl edit --mount-writable swe
limactl start swe
```

(Multipass/UTM work too; you then manage port-forwards for 1977 and any
preview ports yourself.)

### 3a. Corporate TLS interception (only if the Mac sets NODE_EXTRA_CA_CERTS)

Skip this section unless `echo $NODE_EXTRA_CA_CERTS` on the Mac prints a
path. Machines behind a TLS-inspecting proxy (Netskope, Zscaler, ...)
usually have it set to the proxy's root CA; the fresh VM does not trust
that CA yet, so every HTTPS fetch inside it fails with
`curl: (60) SSL certificate problem: self-signed certificate`.

On the Mac:

```sh
limactl copy "$NODE_EXTRA_CA_CERTS" swe:/tmp/corp-ca.crt
```

In the VM (`limactl shell swe`):

```sh
sudo cp /tmp/corp-ca.crt /usr/local/share/ca-certificates/corp-ca.crt
sudo update-ca-certificates    # curl and apt now trust the proxy
# node/npm keep their own CA bundle, so point them at the system one:
echo 'export NODE_EXTRA_CA_CERTS=/etc/ssl/certs/ca-certificates.crt' >> ~/.bashrc
. ~/.bashrc
```

## 4. Install swe-swe in the VM (works today)

```sh
sudo apt-get update && sudo apt-get install -y git
# node: swe-swe's own helpers no longer need it (swe-npx launches them as
# static binaries); it is only needed for a node-based agent CLI (claude) or
# Agent View's @playwright/mcp driver. npm is also how swe-swe itself installs.
curl -fsSL https://deb.nodesource.com/setup_24.x | sudo bash - && sudo apt-get install -y nodejs
# user-level npm prefix: no sudo for -g installs
mkdir -p ~/.npm-global && npm config set prefix ~/.npm-global
echo 'export PATH=$HOME/.npm-global/bin:$PATH' >> ~/.bashrc && . ~/.bashrc
npm i -g swe-swe
# claude-code: native installer (recommended; self-updates without sudo).
# npm works too (npm i -g @anthropic-ai/claude-code) with the prefix above.
curl -fsSL https://claude.ai/install.sh | bash
```

`npm i -g swe-swe` needs a published version that (a) carries the embedded
dockerless payload for your platform and (b) is >= the release containing
`--agent-view-tunnel` and swe-npx. Until then (or any time you want current
main), build from source in the VM instead -- the payload is compiled for
the machine that runs `make`, which is exactly what dockerless needs:

```sh
sudo snap install go --classic     # or Go >= 1.24 from go.dev/dl
git clone https://github.com/choonkeat/swe-swe.git ~/swe-swe-src
cd ~/swe-swe-src
make dockerless-payload
go build -o ~/.npm-global/bin/swe-swe ./cmd/swe-swe
```

(Clone inside the VM rather than building in a Lima-mounted `/Users` path;
the default home mount is read-only.)

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

### 5a. Agent View, tunnel mode (recommended)

```sh
SWE_BROWSER_BACKEND_TOKEN=pick-a-shared-secret \
    swe-swe up --agent-view=http://host.lima.internal:9333 --agent-view-tunnel
```

The VM dials out; the backend binds VM ports on its own loopback and
shuffles traffic back over that connection. No inbound path to the VM is
needed at all, no `SWE_AGENT_VIEW_LOCALHOST`, no NAT reasoning. It is also
the only workable mode if the VM ever moves somewhere the backend cannot
reach (another machine, a firewalled cloud box reached via swe-swe-tunnel).

### 5b. Agent View, direct mode (fallback, one override)

Without `--agent-view-tunnel`, chromium *in the backend container* must
reach dev servers *in the VM*, and the default guess (allocation source
address) is a Docker NAT IP that does not route back. Override it with
Docker's name for the Mac (`SWE_AGENT_VIEW_LOCALHOST=host.docker.internal`
on the `swe-swe up` line) and Lima's forwards complete the chain
(container -> host.docker.internal -> Mac loopback -> VM). Sanity check:
`docker exec swe-browser curl -sI http://host.docker.internal:1977` should
return an HTTP status.

## 6. Daily use

```sh
limactl shell swe -- swe-swe up      # morning
limactl shell swe -- swe-swe down    # evening; docker stop swe-browser too
```

Upgrades: `npm update -g swe-swe` in the VM, re-run `swe-swe init
--dockerless`; rebuild the backend image from a fresh repo checkout when it
changes.

## Status

| Piece | Status |
|---|---|
| VM + dockerless server, 5 tabs, previews on lvh.me | works |
| Agent View with zero VM-inbound (`--agent-view-tunnel`) | works |
| Agent View direct mode + `SWE_AGENT_VIEW_LOCALHOST` | works (fallback) |
| node-free VM (non-node agent, no Agent View) | works |
| swe-swe natively on macOS, no VM | dockerless Phase 6 (code-complete, unverified) |
