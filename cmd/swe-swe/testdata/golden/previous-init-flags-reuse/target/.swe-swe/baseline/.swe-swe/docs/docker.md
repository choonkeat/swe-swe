# Docker Access

## Prefer a Procfile for multi-service apps

If you are building an app that needs more than one process (a web server plus a
worker, or a web app plus a database), you almost certainly do **not** need
Docker. Write a `Procfile` and run it with `swe-run`:

```
web: node server.js
worker: node worker.js
db: postgres -D ./pgdata -p $PORT_DB -k /tmp
```

```bash
swe-run
```

`swe-run` starts each service as an ordinary child in this session's process
group, assigns each a collision-free port derived from `$PORT`, and publishes
`$PORT_<NAME>` so services discover each other on `localhost`. Because the
services are plain children, **they die with the session -- nothing leaks**. See
`.swe-swe/docs/multi-service.md` for the full guide.

**Why prefer this over `docker compose`:**

- The Docker socket is **host-root-equivalent** (ADR-0013). It is only present
  when the project was initialized with `--with-docker`, and handing it to an
  agent session is the single biggest hole in the trust model.
- Compose-started containers are **not tied to the session lifecycle**, so they
  leak: the session ends, the containers keep running, and the host accumulates
  remnants nobody cleans up.

Reach for Docker only when you genuinely need container networking, image
builds, or an existing compose stack.

## Docker CLI (only with `--with-docker`)

When the project was initialized with `--with-docker`, the Docker CLI and Docker
Compose are available, connected to the host's Docker daemon via
`/var/run/docker.sock`.

You can `docker ps` to see the exact name of this `swe-swe` container we're in.

### Common Commands

```bash
# List running containers
docker ps

# Build an image
docker build -t myapp .

# Run a container
docker run --rm myapp

# Use docker compose
docker compose up -d
docker compose down
```

### Notes

- The Docker socket is only available if the project was initialized with
  `--with-docker`. Mounting it grants host-root-equivalent access (ADR-0013).
- Commands run against the **host's** Docker daemon, not a nested one.
- Network access between containers uses Docker's internal DNS (service names
  from docker-compose.yml).
- Containers you start are **not** cleaned up when the session ends -- you are
  responsible for `docker compose down` / `docker rm`. The Procfile path above
  avoids this entirely.
