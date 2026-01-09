# Pending: Track code-server file open via URL query param

## Status: PENDING - waiting for upstream support

## Context

swe-swe's clickable terminal links currently open VS Code at the folder level using `?folder=`.
Once code-server supports opening specific files via query params, we can enhance the link click behavior.

## GitHub Issues to Monitor

1. **[Ability to open a file directly using a URL · Issue #1964](https://github.com/coder/code-server/issues/1964)**
   - Request for `?file=` or `?path=/dir/file.ext:line:col` support
   - Status: Backlog (acknowledged but not prioritized)

2. **[Support hyperlinks to local/project files · Issue #6785](https://github.com/coder/code-server/issues/6785)**
   - Request for `vscode://` / `code-oss://` scheme support
   - Related to RelayURLService not being loaded in code-server

## Current Workaround

Using `?folder=/path/to/directory` opens VS Code at the file's parent directory.
User can then navigate to the specific file.

## When to Update

When either issue is resolved, update `link-provider.js` to use the new query param
to open files directly (with line number support if available).
