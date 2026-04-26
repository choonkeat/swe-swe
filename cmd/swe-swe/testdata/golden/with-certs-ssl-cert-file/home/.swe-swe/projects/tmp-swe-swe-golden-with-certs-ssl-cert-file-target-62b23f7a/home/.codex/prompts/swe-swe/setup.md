---
description: Configure git identity, HTTPS or SSH auth, dev server, and env vars for this swe-swe session
---

# Setup swe-swe Environment

State-aware checklist. Detect what's already configured, only walk the user through what's missing. Verify each step before moving on.

## Style

Conversational. One thing at a time. Skip what's already configured. Always end a section with a verification command the user can see succeed.

## Step 1: Detect repo state

Run these and report findings:

```bash
git rev-parse --show-toplevel 2>/dev/null
git remote -v 2>/dev/null
```

Three branches:
- **Has a remote, HTTPS URL** (e.g. `https://github.com/...`) — proceed to Step 2 and 3a (HTTPS auth).
- **Has a remote, SSH URL** (e.g. `git@github.com:...`) — proceed to Step 2 and 3b (SSH auth).
- **No remote / not a repo** — skip Step 3, ask if the user wants to clone or init.

## Step 2: Git identity

Check resolution order:

```bash
git config --show-origin --get user.name
git config --show-origin --get user.email
```

What you'll see and what it means:

- **`file:.git/config`** — set in the repo's local config. The Settings UI's Author Name/Email fields will be **readonly** because git's local config beats the per-session GIT_CONFIG_GLOBAL. To change it, edit the repo: `git config --local user.name '...'` or `git config --local --unset user.name` to fall back to per-session.
- **`file:/home/app/.gitconfig`** — set in the user's global config. Per-session Settings UI will override (the per-session `GIT_CONFIG_GLOBAL` includes `~/.gitconfig` as a baseline and adds its own `[user]` section on top).
- **`(unset)`** — neither local nor global. Tell the user to open Settings (gear icon on this session), scroll to "Git HTTPS Credentials (per session)," fill Author Name + Email, click Save Credentials. Identity now flows through `GIT_CONFIG_GLOBAL` for all git invocations in this session.

After the user updates Settings (or local/global config), verify:

```bash
git config user.name
git config user.email
```

## Step 3a: HTTPS authentication (per-session credential broker)

If the remote URL starts with `https://`, the swe-swe credential broker handles auth. No `~/.gitconfig` setup needed.

Verify the broker is alive and this shell is registered:

```bash
swe-swe-broker-probe
# Expect: {"echoed":..., "sid":"<this-session-uuid>", ...}
```

Check whether creds are already saved for the host:

```bash
git ls-remote origin HEAD 2>&1 | head -2
```

- **Succeeds silently** → creds are configured, you're done.
- **Prompts for `Username for 'https://...'`** → broker has nothing for the host. Ctrl+C and walk the user through Settings:
  1. Click the gear icon on this session
  2. Scroll to "Git HTTPS Credentials (per session)"
  3. Host: the remote's hostname (e.g. `github.com`)
  4. Username: `x-access-token` (default for GitHub PATs) or actual username
  5. Personal Access Token: paste a PAT with the scopes the user needs (`repo` for read+write)
  6. Author Name + Email: optional; fills `[user]` in the per-session gitconfig (only takes effect if no local/global override)
  7. Click "Save Credentials" → status flips to "Stored on server for: <host>"
- **Auth error from server** → bad creds, re-paste a working PAT.

Verify after save:

```bash
git ls-remote origin HEAD
# Expect: silent success, prints the HEAD ref
```

## Step 3b: SSH authentication

If the remote URL starts with `git@` or `ssh://`, the credential broker is bypassed; SSH key auth is in play.

Check for keys:

```bash
ls ~/.ssh/id_* 2>/dev/null
```

- **No keys** → offer to generate:
  ```bash
  ssh-keygen -t ed25519 -C "<user-email>" -f ~/.ssh/id_ed25519 -N ""
  ```
  Then add to ssh-agent:
  ```bash
  eval "$(ssh-agent -s)" && ssh-add ~/.ssh/id_ed25519
  ```
  Show the public key and tell the user to add it at GitHub Settings -> SSH and GPG keys, or GitLab equivalent:
  ```bash
  cat ~/.ssh/id_ed25519.pub
  ```
- **Keys exist** → confirm they are loaded:
  ```bash
  ssh-add -l
  ```

Verify connectivity:

```bash
ssh -T git@github.com 2>&1 | head -3
# Expect: "Hi <username>! You've successfully authenticated..."
```

If the user only ever uses SSH, the per-session credential broker is not relevant for them. Settings UI's HTTPS form can be ignored.

## Step 4: Dev server (testing setup)

Ask: **what command starts your dev server, and what port does it bind?**

- If a `.swe-swe/docs/AGENTS.md` already documents this, just confirm with the user.
- Document how the server is reachable from outside the container: `http://host.docker.internal:<port>` from agents on the host, or via the App Preview proxy from the swe-swe UI.

Verify the port is bound (after they start the server in a Terminal pane):

```bash
ss -lnt | grep -E ":<port>\b"
```

For browser automation against the dev server, point at `.swe-swe/docs/browser-automation.md` (created by `swe-swe init`).

## Step 5: Custom environment variables

Check for `.swe-swe/env`:

```bash
test -f .swe-swe/env && cat .swe-swe/env
```

- **Exists** → show contents, ask if user wants to add or modify.
- **Missing** → ask: "Any env vars your sessions need (API keys, feature flags)?" If yes, collect `KEY=value` pairs (one per line) and write to `.swe-swe/env`. Variables in this file are sourced into every session's shell at startup.
- **No** → skip.

If the user adds or changes vars, mention that **already-running sessions need a shell restart** to pick them up. New sessions will get them automatically.

## Notes

- The pointer `## swe-swe -- See .swe-swe/docs/AGENTS.md` is upserted automatically by `swe-swe init` into CLAUDE.md / AGENTS.md; this skill no longer touches that.
- For updating `.swe-swe/docs/AGENTS.md` itself, use `swe-swe:update-swe-swe`.

Note: If your agent doesn't support slash commands, use `@swe-swe/setup` instead.
