# ADR-012: Enterprise SSL certificate handling

**Status**: Accepted
**Date**: 2025-12-24
**Research**: [research/2025-12-24-ssl-cert-deep-dive.md](../../research/2025-12-24-ssl-cert-deep-dive.md)

## Context
Users behind corporate proxies (Cloudflare WARP, ZScaler, etc.) have custom CA certificates. Node.js and other tools fail SSL verification without these certs.

## Decision
During `swe-swe init`:
- Detect `NODE_EXTRA_CA_CERTS`, `SSL_CERT_FILE`, `NODE_EXTRA_CA_CERTS_BUNDLE` env vars
- Copy referenced certificate files to `.swe-swe/certs/`
- Generate `.env` file mapping env vars to container paths (`/swe-swe/certs/...`)
- Docker Compose loads `.env` automatically

## Consequences
Good: Transparent cert handling, works with any corporate proxy, no manual configuration.
Bad: Certs duplicated to metadata dir, env vars on host must be set before `swe-swe init`.
