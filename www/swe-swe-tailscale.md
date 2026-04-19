# Using swe-swe with Tailscale

## TL;DR

swe-swe already works with Tailscale out of the box. **No code changes needed.**
This doc explains *why* it works, the operational steps, and the gotchas.

(Scope note: ignoring `PUBLIC_PORT` support — separate question.)

---

## Why it just works (audit of swe-swe's networking)

| Concern | Default behavior | Tailscale impact |
|---|---|---|
| Server bind address | Listens on all interfaces — compose mode binds container `:9898` (Traefik fronts it on container `:7000`); dockerfile-only mode binds `0.0.0.0:${SWE_PORT:-1977}` directly. | Reachable on the Tailscale interface immediately. |
| Docker port mapping | Compose: `"${SWE_PORT:-1977}:7000"`. Dockerfile-only: `"${SWE_PORT:-1977}:${SWE_PORT:-1977}"`. Preview/agent-chat/VNC ports: `"%d:%d"`. No IP prefix anywhere. | Docker binds to `0.0.0.0` by default → tailnet peers reach the host port directly. |
| WebSocket Origin check | `CheckOrigin: func(r *http.Request) bool { return true }` (`main.go:85`). | Any hostname (including `*.ts.net`) is accepted. |
| Frontend URL building | Uses `window.location.host` for WS and port-based proxy URLs (`terminal-ui.js:776`, etc.). | Browser's hostname is reused — Tailscale FQDN works. |
| Auth cookie flags | `SameSite=Lax`; `Secure` auto-set from `X-Forwarded-Proto` when present, falling back to `SWE_COOKIE_SECURE` only when no proxy header arrives (`resolveCookieSecure` in `auth.go`). | Direct HTTP to swe-swe-server over Tailscale (bypassing Traefik) → header absent, env-var fallback applies; on compose-SSL that fallback is `true`, so unset `SWE_COOKIE_SECURE` for the container or front the port with `tailscale serve` so the header is set and cookies become Secure naturally. |
| Per-session proxy ports | `SWE_PREVIEW_PORTS=3000-3019`, `SWE_AGENT_CHAT_PORTS=4000-4019`, `SWE_PUBLIC_PORTS=5000-5019`, `SWE_VNC_PORTS=7000-7019`. All bound on the host. | Each is reachable as `tailnet-host:<port>`. |

Net result: **the only work is operational.** No code, no env-var additions, no allowlist edits.

---

## Setup

### 1. Install Tailscale on the swe-swe host

```sh
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up
```

Note the hostname Tailscale assigns, e.g. `mybox.tail-scale.ts.net`.

### 2. Start swe-swe as usual

```sh
swe-swe init      # one-time
docker compose up -d
```

The user-facing host port is **`SWE_PORT` (default `1977`) in both compose mode and dockerfile-only mode** — same env var, same URL. What differs is what's actually listening inside the container:

| Mode | Container internals |
|---|---|
| Compose (default) | Traefik on container `:7000` → forwards to `swe-swe-server` on container `:9898`. Host `SWE_PORT` → Traefik. |
| Dockerfile-only | `swe-swe-server` listens directly on `${SWE_PORT}`, no Traefik. |

From a Tailscale client the difference is invisible.

### 3. Visit from your laptop / phone

| What | URL |
|---|---|
| Main UI | `http://mybox.tail-scale.ts.net:1977` |
| Per-session app preview | `http://mybox.tail-scale.ts.net:<3000+N>` |
| Per-session agent chat | `http://mybox.tail-scale.ts.net:<4000+N>` |
| Per-session VNC | `http://mybox.tail-scale.ts.net:<7000+N>` |

The UI itself constructs the right URLs based on whatever host you used to load it, so links inside the page work transparently.

---

## HTTPS (optional)

Tailscale issues Let's Encrypt certs for your tailnet hostname.

### Option A — `tailscale serve` (per port)

```sh
tailscale serve --bg --https=443 http://localhost:1977
```

Browser to `https://mybox.tail-scale.ts.net` → proxied to swe-swe.

**Caveat:** `tailscale serve` only terminates on ports you list. Per-session preview ports stay HTTP. If you load the main UI over HTTPS and it iframes a `http://...:3000` preview, the browser will block mixed content. Either:

- Keep the main UI on HTTP too (simplest), or
- Run `tailscale serve` for *every* preview/agent-chat port you actually use.

### Option B — stay on HTTP (recommended)

Tailscale already encrypts the link with WireGuard. HTTP-over-Tailscale is end-to-end encrypted. TLS on top adds friction without much benefit on a private mesh.

If you front swe-swe with your own HTTPS reverse proxy on the tailnet, set:

```sh
SWE_COOKIE_SECURE=true
```

so the auth cookie is marked Secure. Don't set it otherwise — the cookie won't be sent over plain HTTP and login will appear to silently fail.

---

## Public exposure via Tailscale Funnel (if needed)

If you want the swe-swe UI reachable by clients **not** on your tailnet:

```sh
tailscale funnel --bg --https=443 http://localhost:1977
```

⚠️ **Set a strong `SWE_SWE_PASSWORD` first.** Funnel publishes to the public internet via Tailscale's edge. Same per-port caveat as `tailscale serve`.

---

## Mobile

Install the Tailscale iOS / Android app, sign into the same tailnet, browse the same URLs. No special config.

---

## Cost

Tailscale's free **Personal** tier covers this entirely: up to 100 devices, 3 users. A solo dev or a 2–3 person team pays $0.

---

## Self-hosted control plane (Headscale)

Prefer not to depend on Tailscale Inc. for the coordination server? [Headscale](https://github.com/juanfont/headscale) is an open-source reimplementation. You run it; the official Tailscale clients connect to it.

### What you get

- Same client binaries and apps (iOS, Android, macOS, Windows, Linux) — point them at your Headscale URL.
- MagicDNS.
- ACLs (HuJSON policy, syntax-compatible with Tailscale's).
- Pre-auth keys, device approval, tags.
- DERP — either use Tailscale's public DERPs (free, operated by Tailscale Inc.) or run your own.
- OIDC login (optional) against your IdP.

### What you give up

- **Funnel.** No public-internet exposure via the coordination server; use a separate reverse proxy / tunnel for that.
- **`tailscale cert` / automatic HTTPS issuance.** Community plugins exist; otherwise terminate TLS yourself (Traefik + LE) on the tailnet.
- **Bundled admin UI.** Community options exist (`headscale-ui`, etc.); vanilla Headscale is CLI-only.
- Ops burden: a Go server + SQLite/Postgres + a reachable hostname to keep running.

### Swe-swe impact

Zero additional code changes. Once clients are joined to your Headscale tailnet, everything in this doc applies unchanged — same host URLs (under whatever suffix you configure), same cookie behavior, same port layout.

### Choosing

| You want... | Use |
|---|---|
| Zero infra; free personal use (≤100 devices, ≤3 users) | **Tailscale** personal tier |
| Public swe-swe URL via Funnel without running a proxy | **Tailscale** |
| Mobile apps with zero fuss | **Tailscale** (Headscale works but needs login-server config per device) |
| Data stays on your infra; air-gapped-capable; no vendor dependency | **Headscale** |
| Team-scale SSO with SCIM/billing out of the box | **Tailscale** paid |
| Self-host everything end-to-end (including DERP and TLS) | **Headscale** + own DERP + own cert story |

---

## Things to know

- **Cookies are per-hostname.** Hitting both `localhost:1977` and `mybox.tail-scale.ts.net:1977` means logging in separately on each. Normal browser behavior.
- **Per-session port proxies bypass Tailscale Serve / Funnel.** They're host-bound directly. For tailnet-only use that's fine; for Funnel it means previews stay private even if you Funnel the main UI.
- **`SWE_PORT` default is 1977** — same env var in both compose and dockerfile-only modes. Change it if 1977 collides with something on the host.

---

## Firewall hardening (important)

Tailscale makes the host reachable on the tailnet, but it does **not** restrict who else can reach it. Docker publishes ports on `0.0.0.0`, so any client that can route to the host — LAN neighbour, public internet if the host has a routable IP — can still hit `SWE_PORT` and the per-session ranges directly, bypassing the tailnet entirely.

If the goal is "tailnet-only," drop non-tailnet inbound at the host firewall.

### Quickest: block the public interface from Docker's forward chain

Docker inserts its own rules ahead of `INPUT`; the `DOCKER-USER` chain is the supported place to add overrides that affect published container ports.

```sh
# Find your public-facing interface (the one used to reach the internet)
PUBLIC_IFACE=$(ip -4 route get 1.1.1.1 | awk '{print $5; exit}')

# Block anything arriving on the public interface from reaching Docker-published ports
sudo iptables -I DOCKER-USER -i "$PUBLIC_IFACE" -j DROP

# Persist across reboots (Debian/Ubuntu)
sudo apt-get install -y netfilter-persistent iptables-persistent
sudo netfilter-persistent save
```

Tailscale traffic (on `tailscale0`) is unaffected; only the public-facing interface is blocked from reaching published container ports.

### Broader: default-deny with ufw

```sh
sudo ufw default deny incoming
sudo ufw allow in on tailscale0   # tailnet peers: allow everything
sudo ufw allow 22/tcp             # keep SSH access (restrict source if you like)
sudo ufw enable
```

`ufw` rules don't naturally apply to Docker's `FORWARD` chain — published container ports bypass `INPUT`. Add the `DOCKER-USER` rule from the previous snippet alongside `ufw`, or install [`ufw-docker`](https://github.com/chaifeng/ufw-docker) for a unified setup.

### Verify

From **outside** the tailnet (phone on cellular, laptop with Tailscale off):

```sh
curl -v --max-time 5 http://<public-hostname>:1977    # should hang or connection-refused
```

From **inside** the tailnet:

```sh
curl http://mybox.tail-scale.ts.net:1977              # should return the swe-swe login page
```

---

## Single-container PaaS deployment

The goal: bake everything into one Docker image (swe-swe + Tailscale) and deploy a single container to a PaaS (Fly, Railway, Render, etc.) so it's reachable via your tailnet — without giving up the PaaS public URL.

### Topology

```
                              ┌────────────────── Container ───────────────────┐
                              │                                                 │
   PaaS public URL  ──────►   │  swe-swe-server (PID 1, owns lifecycle)         │
   ($PORT, e.g. 8080)         │  ├── http listener on $PORT  → landing page     │
                              │  │       (health + "use Tailscale" notice)     │
                              │  ├── http listener on $SWE_PORT → real swe-swe  │
                              │  └── child: tailscaled --tun=userspace-networking
   tailnet peer ────────────► │       │                                         │
   (mybox.ts.net:$SWE_PORT)   │       └── advertises container's loopback       │
                              │                                                 │
                              └─────────────────────────────────────────────────┘
```

### Two changes to `swe-swe-server`

**1. Manage `tailscaled` as a child process** when `--tailscale-authkey` (or `TS_AUTHKEY`) is set.

- `cmd.Exec` the installed `tailscaled` binary in `--tun=userspace-networking` mode.
- Wait for the daemon socket, then run `tailscale up --authkey ... --hostname ...`.
- Log PID + exit status (per the CLAUDE.md "no silent goroutine `Wait`" rule — that bug was added after exactly this kind of slip).
- Tear down `tailscaled` on `swe-swe-server` shutdown (existing `serverCtx` cancel path).

Why exec, not [`tsnet`](https://pkg.go.dev/tailscale.com/tsnet): `tsnet` only exposes ports that the embedding Go process listens on. swe-swe's per-session preview / agent-chat / VNC ports are bound by **child agent processes**, so `tsnet` would force us to add proxy listeners for every range. `tailscaled --tun=userspace-networking` transparently bridges the entire container loopback — anything bound on `0.0.0.0:*` inside the container is reachable from the tailnet, no extra code.

New flags / env:
- `--tailscale-authkey` / `TS_AUTHKEY`
- `--tailscale-hostname` / `TS_HOSTNAME`
- `--tailscale-state-dir` / `TS_STATE_DIR` (default `/var/lib/tailscale`)
- `--tailscale-disable` / `TS_DISABLE=1` (escape hatch)

If `TS_AUTHKEY` is unset, the new code path is dormant — single-container behavior is unchanged.

**2. Landing-page / health server on `$PORT`** when `$PORT` is set and differs from the swe-swe listen port.

- A tiny secondary `http.Server` on `:$PORT` returning:
  - `GET /health` → `200 OK`
  - everything else → minimal HTML: *"swe-swe is running. Reach it via Tailscale at `<tailnet-hostname>:<SWE_PORT>`. Learn more: https://swe-swe.netlify.app"*
- Default behavior is **secure**: PaaS public URL doesn't expose the swe-swe login form to the internet — only a placeholder.
- Users who *do* want public access on `$PORT` set `SWE_PORT=$PORT` (or unset `SWE_PORT` and let `$PORT` win), and the landing server doesn't start.

Decision rule for which port `swe-swe-server` itself binds:
1. `--addr` explicit → use it; if `$PORT` is set and different, landing server starts on `$PORT`.
2. `--addr` unset, `SWE_PORT` set → bind `:$SWE_PORT`; if `$PORT` set and different, landing server on `$PORT`.
3. `--addr` unset, `SWE_PORT` unset, `$PORT` set → bind `:$PORT` directly (current PaaS expectation, no landing).
4. Nothing set → default `:9898`.

Optional knobs: `SWE_LANDING_URL` (override link target), `SWE_LANDING_DISABLE=1` (just respond `200` to all paths on `$PORT`).

### Dockerfile changes

Two lines added to the dockerfile-only template:

```dockerfile
# Install Tailscale (static binaries, no apt repo)
RUN curl -fsSL https://tailscale.com/install.sh | sh
```

Entrypoint stays one-line: `swe-swe-server` (which now manages `tailscaled` itself).

### PaaS persistence

- **Fly Volumes / Railway Volumes**: mount at `/var/lib/tailscale` so the device identity survives redeploys.
- **No volume**: use **ephemeral** auth keys (Tailscale admin → Settings → Keys → "Ephemeral"). Old devices auto-prune on disconnect; trade-off is the tailnet hostname/IP can change across restarts.

### What this is NOT

- Not a replacement for the existing compose-mode deployment — that stays unchanged.
- Not a way to expose per-session preview ports through the PaaS public URL (PaaS only routes `$PORT`). Per-session ports remain Tailscale-only — which is the right answer for security anyway.

---

## Summary

What's needed for swe-swe to support Tailscale:

- **Code:** nothing for tailnet-on-host usage. PaaS-single-container usage is supported directly by `swe-swe-server`: set `TS_AUTHKEY` and it spawns `tailscaled`; set `$PORT` alongside `SWE_PORT` and a landing-page / health server covers the PaaS public URL.
- **Config:** nothing (optionally `SWE_COOKIE_SECURE=true` if you front it with HTTPS).
- **Ops:** install Tailscale on the host (or bake into the image for PaaS), hit the tailnet hostname. For tailnet-only access on a host, also apply a `DOCKER-USER` firewall rule (see **Firewall hardening** above).
