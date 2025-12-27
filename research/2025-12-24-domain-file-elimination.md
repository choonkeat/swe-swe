# Research: Eliminating `.swe-swe/.domain` File

**Date**: 2025-12-24
**Context**: Discussion about simplifying domain configuration and VSCode redirect logic

## Current State

The `.swe-swe/.domain` file is created during `swe-swe init` and stores the domain (e.g., "lvh.me").

**Dependencies:**
1. Creation: `cmd/swe-swe/main.go:142-147` during `handleInit()`
2. CLI output: `cmd/swe-swe/main.go:258-260` to display URLs to user
3. Docker env: `cmd/swe-swe/main.go:295` passes `SWE_DOMAIN` to docker-compose
4. Server-side VSCode redirect: `cmd/swe-swe-server/main.go:964-972` reads `SWE_DOMAIN` env var to construct redirect URLs
5. Documentation: `README.md:64` references the file

## Proposal: Eliminate `.domain` File

### Changes

1. **Skip storing `.domain` file** - no need to persist it
2. **Simplify CLI output** - show `0.0.0.0:{port}` instead of all 3 URLs (traefik, swe-swe, vscode)
3. **Skip docker env var** - don't pass `SWE_DOMAIN` through docker-compose
4. **Extract domain from incoming request** - in VSCode redirect, derive domain from request hostname
   - Algorithm: `request.Host` = `{service}.{domain}` → extract `{domain}` → redirect to `vscode.{domain}`
5. **Skip updating documentation**

### Pros

- ✅ Eliminates `.domain` file entirely - reduces stored state
- ✅ Simpler initialization - no need to capture domain during `swe-swe init`
- ✅ Cleaner CLI output that works universally
- ✅ Server dynamically adapts based on incoming request - no environment variables needed
- ✅ Reduces dependencies and state management

### Cons / Edge Cases

**Subdomain-based access** (e.g., `swe-swe.lvh.me` → `vscode.lvh.me`):
- ✅ Works perfectly - extracting domain from hostname is straightforward

**IP address access** (e.g., `127.0.0.1:9899` → redirect to `vscode.127.0.0.1`):
- ❌ Won't work cleanly, but users accessing via IP probably don't need `/vscode` redirect anyway
- Already accessing services directly via IP, no Traefik routing needed

**Localhost access** (e.g., `localhost:9899`):
- ❌ Won't have a proper domain to extract, but same situation
- Users would just open VSCode directly, no redirect needed

### Key Observation

Traefik routing already assumes subdomain structure (`HostRegexp('vscode\..+')` requires a dot), so anyone using the Traefik setup is already using subdomains. The IP/localhost users wouldn't be hitting the VSCode redirect anyway.

## Conclusion

**Recommendation: Proceed with elimination**

The proposal makes sense because:
1. Request hostname contains all the information we need
2. The `.domain` file is only useful if users access via subdomains (which Traefik requires anyway)
3. Reduces initialization complexity and stored state
4. Simple implementation: parse incoming `request.Host` to extract domain component

## Next Steps

When ready to implement:
1. Remove `.domain` file creation from `handleInit()`
2. Update CLI output to show `0.0.0.0:{port}`
3. Remove `SWE_DOMAIN` env var passing to docker
4. Update VSCode redirect logic to extract domain from `request.Host`
5. Remove `.domain` file read logic
6. Update documentation
