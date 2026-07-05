# Fix #4 -- git clone runs an attacker-controlled URL (ext:: RCE / file:// read)

## Status

**Draft for discussion.** Not started. From CTF.md finding #4.

## Problem

Repo-prepare passes the raw, user-supplied URL straight to `git clone`.
`sanitizeRepoURL` only rewrites characters to build a *directory name*; it does
not gate what git is asked to fetch, and it returns non-empty for almost any
input. Two sinks share the bug:

- HTTP API: `handleRepoPrepareClone` -> `exec.Command("git", "clone", url, repoPath)`
  (main.go:3519). Cookie-authenticated.
- MCP orchestration tool `clone`: `exec.Command("git", "clone", args.URL, repoPath)`
  (main.go:8249). Authenticated by `mcpAuthKey`.

Git's `ext::` transport executes a shell command, so a URL like
`ext::sh -c <cmd>` is remote code execution; `file://` turns it into arbitrary
local-repo read. `sanitizeRepoURL` (main.go:3202) leaves both intact (it only
strips known prefixes and swaps filesystem-unsafe chars), so neither sink is
protected.

```
POST /api/repo/prepare
{"mode":"clone","url":"ext::sh -c touch$IFS/tmp/pwned"}
```

This is post-auth, but the "clone a URL" feature is meant to be a constrained
action, not an arbitrary-command sink; it also widens the blast radius of a
stolen cookie or a leaked `mcpAuthKey`.

## Proposed fix

A single shared validator used by both sinks:

```go
// validateCloneURL returns nil if u is a clone URL we are willing to hand to
// `git clone`. Allowed: https://, http://, ssh://, and scp-style git@host:path.
// Rejected: ext::, file://, anything with a leading "-", and unknown schemes.
func validateCloneURL(u string) error { ... }
```

Rules:
- Accept `https://`, `http://`, `ssh://`, and scp-style `user@host:path`
  (e.g. `git@github.com:org/repo.git`).
- Reject `ext::`, `file://`, `fd::`, and any `<transport>::` remote-helper form.
- Reject a leading `-` (argument injection) and pass `--` before the URL in
  both `exec.Command` calls as defense in depth.
- Optionally set `GIT_ALLOW_PROTOCOL=https:http:ssh:git` on the clone command's
  env as a belt-and-suspenders backstop.

Both call sites reject with the existing error shape (JSON `{"error": ...}` for
HTTP, `fmt.Errorf` for MCP) before spawning git.

## Operational impact on deployed instances

**LOW.** The only inputs that stop working are the attack forms (`ext::`,
`file://`, bare local paths) -- no legitimate "clone a repo" flow uses those.
Private-repo clones over HTTPS (broker credential helper) and SSH are
unaffected. The one real risk is being *too* strict and rejecting an
odd-but-valid URL, so the allow-list must cover the scp-style `git@host:path`
form, which is the common SSH shorthand. No config, no migration, no data
change.

## Open questions

1. Do we want `http://` (cleartext) in the allow-list at all, or only `https://`
   + ssh? Some internal mirrors are http-only; leaning keep-it.
2. Should `GIT_ALLOW_PROTOCOL` be set globally on the server process instead of
   per-command, to also cover submodule fetches during clone? (Submodules can
   re-introduce `ext::` via `.gitmodules`.) Recommend yes -- set it process-wide
   in entrypoint or `main`.

## Test plan (TDD)

Pure-function tests for `validateCloneURL`:
- accept: `https://github.com/o/r`, `http://host/r`, `ssh://git@h/r`,
  `git@github.com:o/r.git`.
- reject: `ext::sh -c id`, `file:///etc`, `--upload-pack=x`, `fd::17`,
  `weird::thing`, `` (empty).

Then wire both sinks and assert each returns the rejection (no `git` spawned)
for an `ext::` URL.
