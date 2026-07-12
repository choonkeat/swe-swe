# Multi-service preview (vhost apps, no Docker required)

The App Preview tab can front more than one backend service per session. This
guide covers running several services and reaching each from the browser --
with or without Docker.

See [ADR-0045](adr/0045-preview-host-demux.md) for the design and rationale.

## The two ways to address a service

The per-session preview listener (`<previewPort>` = `20000 + PORT`) demuxes the
leftmost label of the browser-facing hostname:

| You type in the URL bar | Reaches | Upstream `Host` sent |
|-------------------------|---------|----------------------|
| `5000` (bare port)      | `127.0.0.1:5000` | `localhost:5000` |
| `app1.lvh.me:5000`      | `127.0.0.1:5000` | `app1.lvh.me:5000` |
| `app1.lvh.me` (no port) | `127.0.0.1:<PORT>` (primary) | `app1.lvh.me:<PORT>` |

- Use the **bare port** form when your service does not care about the `Host`
  header (most apps).
- Use the **`{name}.{suffix}:{port}`** form when your stack has its own
  Host-based router (traefik/nginx) that dispatches on `Host` -- the listener
  rewrites the upstream `Host` to `{name}.{suffix}:{port}` so that router
  matches exactly as it would on your laptop. The suffix defaults to `lvh.me`
  (`SWE_PREVIEW_VHOST_SUFFIX`).

Ports must be in 1024-65535; targets are always loopback (`127.0.0.1`).

## No Docker required

The demux targets `127.0.0.1:{port}` and does not care how the service is run.
Any of these are equivalent:

```bash
# Plain backgrounded processes
python3 -m http.server 5000 &
PORT=5001 node server.js &

# Procfile / foreman
npx -y foreman start

# process-compose (declarative, no daemon)
process-compose up
```

None of this needs `swe-swe init --with-docker`. swe-swe does **not** start,
stop, or supervise your services -- that is your process runner's job (this is
deliberate; there is no "mini compose" runtime).

If you *do* use `docker compose` (because your stack genuinely needs it), note
that `--with-docker` mounts the host Docker socket, which is host-root-equivalent
(see ADR-0013). Prefer the docker-free path above when you can.

## Wildcard vs pinned mode

The browser cannot always resolve wildcard subdomains of the reach domain
(corporate DNS, `/etc/hosts`, air-gapped LAN, or the tunnel). The frontend
probes and shows the active mode next to the URL bar:

- **wildcard**: `{label}.{reach}` resolves to swe-swe. Multiple vhosts work at
  once, each on its own origin.
- **pinned**: no wildcard reachable. One vhost at a time; switching hosts
  re-pins and reloads. The indicator reads `pinned` (amber). Over the tunnel,
  sessions are always pinned (a follow-up will lift this).

Set `SWE_PREVIEW_REACH_DOMAIN` to an explicit wildcard domain that resolves to
the swe-swe machine from the user's browser (e.g. `<ip>.sslip.io`) to force a
specific reach.

**Password note**: pinned mode works under `SWE_SWE_PASSWORD`. Wildcard mode
under a password currently requires a same-host reach (the login cookie is
host-only); see the limitation in ADR-0045.

## Named routes (optional)

Instead of encoding the port in every label, a session can register named
aliases (e.g. `auth` -> `127.0.0.1:5000`, Host `auth.lvh.me`) so `auth.lvh.me`
resolves without the `-5000` suffix. Declarative registration reads
`.swe-swe/services.yml`:

```yaml
# .swe-swe/services.yml
auth:
  port: 5000
  host: auth.lvh.me
web:
  port: 3000
  host: web.lvh.me
```

Entries are *seeds* read at session start; the runtime registration API wins on
conflict. This is a registration source only -- swe-swe still never starts or
supervises the listed services. (Named routes / this file are delivered by a
follow-up phase; the port-label and bare-port forms above work today.)
