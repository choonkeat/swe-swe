# ADR-008: ForwardAuth unified authentication

**Status**: Accepted
**Date**: 2026-01-01
**Research**: [tasks/2026-01-01-traefik-forwardauth.md](../../tasks/2026-01-01-traefik-forwardauth.md)

## Context
Multiple services (terminal, VSCode, Chrome VNC) need authentication. Per-service auth is inconsistent and requires multiple logins.

## Decision
Use Traefik's ForwardAuth middleware with a dedicated auth service:
- Single `/auth/login` endpoint handles all authentication
- ForwardAuth middleware on all protected routes
- Session cookie shared across all services
- Auth service validates credentials against `SWE_SWE_PASSWORD` env var

## Exceptions

Since v2.12.1, **public ports** (default 5000-5019, configured via `--public-ports`) are routed through Traefik *without* `forwardAuth` middleware. This enables webhooks, public APIs, and shareable preview URLs without login. Each session's `PUBLIC_PORT` env var provides the allocated port. See ADR-025 for the port architecture.

## Consequences
Good: Single login for all services, consistent UX, centralized auth logic.
Bad: Auth service is single point of failure, requires cookie-based sessions.
