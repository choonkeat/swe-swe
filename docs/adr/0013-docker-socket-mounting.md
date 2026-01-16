# ADR-013: Docker socket mounting with --with-docker flag

**Status**: Accepted
**Date**: 2026-01-02

## Context
AI agents performing integration testing, building Docker images, or managing containers need access to Docker. Without host Docker access, agents must use alternative approaches (Docker-in-Docker, remote Docker hosts) that add complexity.

## Decision
Add `--with-docker` flag to `swe-swe init` that:
1. Installs Docker CLI (static binary) in the container
2. Mounts `/var/run/docker.sock` from host to container
3. Configures runtime GID detection in entrypoint.sh to grant socket access to the `app` user

The feature is opt-in only. Users must explicitly request `--with-docker`.

### Implementation details
- Docker CLI installed via static binary download (architecture-aware: amd64/arm64)
- Socket permissions handled at runtime by detecting the socket's GID and adding the `app` user to a matching group
- Works cross-platform (Linux, macOS with Docker Desktop) because all conditional code runs inside the Linux container

## Consequences
**Good**:
- Simple, native Docker access for AI agents
- No Docker-in-Docker complexity or storage driver issues
- Agents can build images, run containers, execute integration tests
- Cross-platform compatible (Linux + macOS)

**Bad**:
- Security risk: Docker socket access is effectively root access to host
- Container can mount host filesystem, run privileged containers, access other containers
- Must only be used with trusted code

## Security mitigations
- Opt-in only (explicit `--with-docker` flag required)
- Documentation warns about security implications
- Socket mounted only for the main swe-swe container, not other services
