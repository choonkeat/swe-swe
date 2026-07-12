# Multi-service apps (Procfile, docker-free)

Building an app that needs more than one process -- a web server plus a worker,
or a web app plus a database -- does not require Docker in swe-swe. The blessed
path is a **Procfile** run by swe-swe's own runner, `swe-run`.

`swe-run` starts each service as an ordinary child process in your session's
process group. Because they are plain children (no Docker socket, no root),
**they die with the session** -- nothing leaks onto the host. This is the key
difference from `docker compose`, whose containers outlive the session and
accumulate as remnants.

## Quickstart

Write a `Procfile` (one `name: command` per line) in your project root:

```
web: node server.js
worker: node worker.js
db: postgres -D ./pgdata -p $PORT_DB -k /tmp
```

Then run it in the Agent Terminal:

```bash
swe-run
```

`swe-run` prints the port each service was assigned and starts them all,
multiplexing their output into the terminal with an aligned `name |` prefix:

```
swe-run | assigning ports for 3 service(s):
swe-run |   web    -> 3000  (primary; PORT, Preview tab)
swe-run |   worker -> 8000  (PORT_WORKER)
swe-run |   db     -> 8020  (PORT_DB)
web    | listening on :3000
worker | ready
db     | database system is ready to accept connections
```

Flags:

- `swe-run -f Procfile.dev` -- use a different Procfile.
- `swe-run -primary worker` -- make a service other than `web` the primary.

## Port assignment (zero hardcoded numbers)

Every service gets a **session-unique** port derived from your session's base
`PORT`, so two sessions running the same Procfile never collide (the isolation
`docker compose` used to give you for free).

- The **primary** service gets the session base `PORT` so the default **Preview**
  tab shows it with no configuration. Primary = the service named `web` if
  present, otherwise the first line (override with `-primary <name>`).
- Every other service gets a distinct port from a free band.

You never write a port number in your Procfile. Services find each other through
discovery env vars (below).

## Service discovery

`swe-run` exports these to **every** service before starting them:

- **Host is always `localhost` / `127.0.0.1`.** There are no container networks,
  so there is never another hostname to resolve.
- **The port of service `foo` is `$PORT_FOO`** (`<NAME>` = the service name
  uppercased, with any non-alphanumeric character replaced by `_`). A `db:` line
  publishes `PORT_DB`; a `back-end:` line publishes `PORT_BACK_END`.
- Each service **also** sees its own port as plain `$PORT` (foreman parity), so a
  single-service app that reads `$PORT` keeps working unchanged.

Your app builds its own connection URL from these, e.g.:

```
postgres://localhost:${PORT_DB}/mydb
redis://localhost:${PORT_CACHE}
```

`swe-run` deliberately does **not** synthesize `DATABASE_URL` for you -- it does
not know your user, password, or database name. But you can set it once in
`.env` or `.swe-swe/env` referencing `$PORT_DB`, and it becomes fully automatic:

```
DATABASE_URL=postgres://localhost:${PORT_DB}/mydb
```

## Environment files

`swe-run` loads env vars into every service in this order (later wins):

1. The inherited session environment (`PATH`, base `PORT`, ...).
2. `.swe-swe/env` -- the per-workspace convention.
3. `.env` in the working directory (foreman parity).
4. The runner-assigned `PORT` / `PORT_<NAME>` discovery values -- these always
   win, so discovery is authoritative.

Both files are simple `KEY=value` lines with `#` comments; surrounding quotes on
a value are stripped.

## Common daemons cheat sheet

Off-the-shelf daemons take a port flag -- point each at its assigned env var:

| Service  | Procfile line                                        |
|----------|------------------------------------------------------|
| Postgres | `db: postgres -D ./pgdata -p $PORT_DB -k /tmp`       |
| Redis    | `cache: redis-server --port $PORT_CACHE`             |
| MySQL    | `db: mysqld --port=$PORT_DB --datadir=./mysql-data`  |
| Mongo    | `db: mongod --port $PORT_DB --dbpath ./mongo-data`   |

Your app then connects to `localhost:$PORT_DB` etc.

## Lifecycle and teardown

- **Any service exiting triggers a graceful shutdown of the rest** (foreman
  semantics): `swe-run` logs `name exited (code N)`, tears down every remaining
  service, and exits with that code. A half-running stack is worse than a clean
  stop -- you see the failure immediately.
- **`Ctrl-C` (SIGINT/SIGTERM)** sends `SIGTERM` to every service group, waits a
  short grace period, then `SIGKILL`s any survivor.
- **Ending the session** kills the whole process group, so even if you never
  press `Ctrl-C`, nothing is left running on the host.
- There is **no auto-restart** of a crashed service in v1.

## Preview and cookies

Once the preview vhost support lands, every assigned port is reachable in the
browser as a bare-port subdomain (`8000.<reach>`) or a named vhost
(`db-8000.<reach>`). Because sub-apps live on distinct hostnames, cookies are
isolated by default; set `Domain=.<reach>` to share one cookie across them. That
behavior comes from the preview layer, not `swe-run` -- this runner's job is to
make the ports exist and be discoverable.

## When you still need Docker

`swe-run` covers the common case: several processes on one host talking over
`localhost`. If you genuinely need container networking, image builds, or a
compose stack, `swe-swe init --with-docker` is still available -- but prefer the
Procfile path. `--with-docker` bind-mounts the host Docker socket, which is
**host-root-equivalent** (ADR-0013) and whose containers are not tied to the
session lifecycle, so they can leak. See `.swe-swe/docs/docker.md`.
