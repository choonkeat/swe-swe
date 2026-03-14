# ADR-0036: VS Code (code-server) as Opt-In Flag

**Status**: Accepted
**Date**: 2026-03-14

## Context

The swe-swe container image includes code-server (VS Code in the browser), which adds significant weight:

- **~500 MB** added to the container image
- **~200 MB RSS** when running
- **Slower container startup** due to code-server initialization

Most users interact with agents through the terminal and agent-chat interface — they never open VS Code. Including it by default penalizes the common case for the benefit of the minority.

Additionally, on-demand browser startup (ADR-0035) established a precedent: expensive optional components should be deferred or opt-in rather than always-on.

## Decision

Make code-server installation opt-in via the `--with-vscode` flag on `swe-swe init` / `swe-swe up`.

### How it works

1. **Default (no flag)**: code-server is not installed in the container. The VS Code tab does not appear in the UI.

2. **`--with-vscode`**: code-server is installed and configured in the Dockerfile. The VS Code tab appears in the UI with the existing iframe-based integration.

3. **Flag persistence**: Like other init flags, `--with-vscode` is stored in `init.json` and reused on subsequent `swe-swe up` invocations unless overridden.

## Consequences

### Positive
- **Smaller default image**: Faster pulls and less disk usage for the common case
- **Lower default memory**: ~200 MB less RSS when VS Code is not needed
- **Faster startup**: No code-server initialization delay
- **Consistent with ADR-0035**: Follows the "opt-in for expensive components" pattern

### Negative
- Users who want VS Code must remember to pass `--with-vscode`
- Existing users upgrading from pre-0036 may need to add the flag if they relied on VS Code

### Neutral
- No change to the VS Code experience when opted in — same iframe integration
- Flag is persisted in `init.json`, so it only needs to be specified once
