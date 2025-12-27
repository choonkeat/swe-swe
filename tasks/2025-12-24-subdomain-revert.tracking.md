# Subdomain Routing Revert: Implementation Tracking

**Date Started**: 2025-12-24 21:05
**Status**: Planning

## Task Breakdown

### Phase 1: Revert Commits
- [ ] Revert commit 8a10351 (docker-compose syntax fix)
- [ ] Revert commit 59ea241 (documentation update)
- [ ] Revert commit 737daa7 (main migration commit)

### Phase 2: Restore Subdomain Configuration
- [ ] Restore Traefik routing rules with HostRegexp back to subdomains
- [ ] Restore docker-compose.yml subdomain service configuration
- [ ] Restore code-server configuration without basepath
- [ ] Restore --domain CLI flag to swe-swe init
- [ ] Restore .swe-swe/.domain file handling in code

### Phase 3: Update Web Components
- [ ] Update terminal-ui links attribute to use subdomain URLs instead of /vscode paths
- [ ] Verify chat-related commits still work with subdomain approach

### Phase 4: Documentation & Testing
- [ ] Update README.md URLs back to subdomain format
- [ ] Update Makefile test assertions for subdomain routing
- [ ] Run swe-swe-integration-test to verify all tests pass
- [ ] Update any other docs referencing /vscode path

### Phase 5: Verification
- [ ] Verify init command works with --domain flag
- [ ] Verify docker-compose builds successfully
- [ ] Verify Traefik routing works for both / and subdomain
- [ ] Test with actual ngrok subdomain setup if possible
- [ ] Full integration test suite passes

## Key Files to Modify
- cmd/swe-swe/templates/docker-compose.yml
- cmd/swe-swe/templates/traefik-dynamic.yml
- cmd/swe-swe/main.go
- cmd/swe-swe-server/static/terminal-with-chat.html (or relevant links config)
- Makefile (test assertions)
- README.md
- Go server code (if domain handling changed)

## Notes
- VSCode password features (5aaabe4, e6e4f9e, 2d2c5fe, 9747d1e) are kept
- Chat history refactoring (34c0391, d20e609, 4013a95, etc.) is kept
- Only path-based routing approach is being removed
