# Terminal Status Bar Fix - Path-Based Routing Migration

## Overview
Remove outdated subdomain precondition from terminal status bar and clean up unused subdomain URL construction logic. The status bar should show [vscode] | [browser] links for all access patterns now that we've migrated to path-based routing.

## Current State
- **File**: `cmd/swe-swe-server/static/terminal-ui.js`
- **Method**: `renderServiceLinks()` (lines 634-688)
- **Problem**: Line 637 has precondition check `if (!hostname.startsWith('swe-swe.')) return;` that blocks rendering unless accessed via subdomain
- **Dead Code**: Lines 648-656 contain unused subdomain URL builder function that's no longer needed

## What Changed
- Migration from subdomain-based routing (e.g., `vscode.example.com`) to path-based routing (e.g., `example.com/vscode`)
- Status bar was updated to build path-based URLs (lines 658-663) but precondition check was never removed
- Recent commits: 237d538, 199eab4, 49596a5, 3804f18

## Tasks

### 1. Remove subdomain precondition check
- [x] Delete lines 635-637 in `terminal-ui.js`
  ```javascript
  // Only show service links when hostname starts with "swe-swe."
  const hostname = window.location.hostname;
  if (!hostname.startsWith('swe-swe.')) return;
  ```
- This allows function to execute for all hostnames
- **COMPLETED**: Removed the subdomain precondition check

### 2. Remove unused subdomain URL builder
- [x] Delete lines 648-656 in `terminal-ui.js` (the `buildUrl` function and baseDomain logic)
  ```javascript
  // Build URLs by swapping subdomain
  const baseDomain = hostname.substring('swe-swe.'.length);

  const buildUrl = (subdomain) => {
      const host = `${subdomain}.${baseDomain}`;
      return port ? `${protocol}//${host}:${port}` : `${protocol}//${host}`;
  };
  ```
- **COMPLETED**: Removed unused buildUrl function and baseDomain logic

### 3. Verify path-based URLs remain correct and add trailing slashes
- [x] Updated URLs to include trailing slashes (required by routing configuration)
  ```javascript
  // All services use path-based routing
  const baseUrl = port ? `${protocol}//${window.location.hostname}:${port}` : `${protocol}//${window.location.hostname}`;
  const services = [
      { name: 'vscode', url: `${baseUrl}/vscode/` },
      { name: 'browser', url: `${baseUrl}/chrome/` }
  ];
  ```
- **VERIFIED**: Traefik routing requires trailing slashes
  - `/chrome/` is explicitly redirected from `/chrome` (docker-compose.yml line 30-31)
  - `/vscode/` is required by nginx location matching (nginx-vscode.conf)
  - These are intentional design decisions for proper path handling across proxy layers

### 4. Test the fix
- [ ] Verify status bar displays [vscode] | [browser] links when accessing terminal via path-based URL
- [ ] Verify links navigate to correct paths (`/vscode/` and `/chrome/` with trailing slashes)
- [ ] Verify links work on different hostnames/environments

## Expected Outcome
Status bar will show [vscode] | [browser] service links for all access patterns, not just subdomain-based access. The terminal UI will be consistent with the new path-based routing architecture.

## Implementation Summary
✅ **COMPLETED** - All code changes implemented in commit `0f491c5`
- Removed subdomain precondition check that was blocking status bar rendering
- Removed unused `buildUrl()` function and subdomain-swapping logic
- Verified path-based URL construction remains correct
- Status bar will now display service links on all hostnames

## Related Files
- Source: `cmd/swe-swe-server/static/terminal-ui.js`
- Template: `cmd/swe-swe-server/static/index.html` (uses `<terminal-ui>` element)
