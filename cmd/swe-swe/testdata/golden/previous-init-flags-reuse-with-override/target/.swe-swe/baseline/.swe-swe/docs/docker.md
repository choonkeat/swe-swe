# Docker Access

We're in a docker container. Docker CLI and Docker Compose are available, connected to the host's Docker daemon via `/var/run/docker.sock`.

You can `docker ps` to see exact name of this `swe-swe` container we're in.

## Common Commands

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

## Notes

- The Docker socket is only available if the project was initialized with `--with-docker`.
- Commands run against the **host's** Docker daemon, not a nested one.
- Network access between containers uses Docker's internal DNS (service names from docker-compose.yml).
