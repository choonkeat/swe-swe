---
description: Publish a swe-swe-scaffolded repo to a new origin -- replace the empty seed commit with a proper "Initial commit" by the real author, then push as origin/main
argument-hint: <remote-url>
---

Set `<remote-url>` as `origin` and push this repo's real work to `origin/main`. If the repo was scaffolded by swe-swe (its root commit is an empty `swe-swe <swe-swe@localhost>` "initial" commit), offer to **replace that seed** with a clean **"Initial commit" authored by the real user**, keep it as the root, and publish that plus all the user's commits.

**Remote URL:** `$ARGUMENTS`

> WARNING: This rewrites history (the root commit is re-authored -> all SHAs change). Do it on a NEW `main` branch, keep the original branch until the push succeeds, and confirm with the user before pushing. Never force-push over an origin/main that already has commits.

## Procedure

### 1. Resolve the remote URL
- If `$ARGUMENTS` is empty, ask the user for the remote URL via chat and stop until they give it.
- Record the current branch: `SRC=$(git branch --show-current)`.

### 2. Set the origin
```bash
URL="$ARGUMENTS"
git remote get-url origin >/dev/null 2>&1 && git remote set-url origin "$URL" || git remote add origin "$URL"
git remote -v | grep '^origin'
```

### 3. Inspect the root commit (detection)
```bash
roots=$(git rev-list --max-parents=0 HEAD | wc -l)          # expect 1
root=$(git rev-list --max-parents=0 HEAD | tail -1)
seed_author=$(git show -s --format='%an <%ae>' "$root")
seed_subject=$(git show -s --format='%s' "$root")
seed_files=$(git show --stat --format= "$root" | grep -c . || true)   # 0 == empty tree
ahead=$(git rev-list --count "$root"..HEAD)                 # commits on top of root
echo "root=$root roots=$roots author='$seed_author' subject='$seed_subject' files=$seed_files ahead=$ahead"
```
Classify:
- **swe-swe seed** = `roots == 1` AND `seed_author` is `swe-swe <swe-swe@localhost>` (subject `initial` and `files == 0` are confirming signals).
- Anything else = **real history** (do NOT rewrite).

### 4. Guardrails before doing anything destructive
- `roots > 1` (multiple root commits) -> **stop**, report, ask the user how to proceed.
- History contains merge commits in `"$root"..HEAD` (`git rev-list --merges "$root"..HEAD` non-empty) -> **stop** and ask (cherry-pick range assumes linear history).
- `ahead == 0` -> nothing but the seed to publish -> **stop**, tell the user there's no work to push yet.
- `git fetch origin` then, if `origin/main` already exists with commits -> **stop** (do not force-push); report and ask.

### 5. Confirm with the user (single checkpoint)
Show, via chat, exactly what will happen and wait for a yes:
- **swe-swe seed path:** "Replace the empty seed `<root>` (`swe-swe@localhost` / \"initial\") with a fresh **\"Initial commit\"** by `<real author>`, and push it plus these `$ahead` commits as `origin/main`?" -- list them: `git log --oneline "$root"..HEAD`. (Resolve `<real author>` per step 6a.)
- **real-history path:** "Root isn't a swe-swe seed, so I'll keep all history and push `$SRC` as `origin/main` as-is. Proceed?"

Do not continue until the user confirms.

### 6a. swe-swe seed path -- replace the seed with a real "Initial commit"
Keep an empty root commit (so the tree/patches replay identically), but re-author it and give it a real message.
```bash
# Resolve the real author: prefer the repo's configured identity; else fall back to the
# first real commit's author. Never keep swe-swe@localhost.
NAME=$(git config user.name);  EMAIL=$(git config user.email)
if [ -z "$NAME" ] || [ -z "$EMAIL" ] || [ "$EMAIL" = "swe-swe@localhost" ]; then
  first=$(git rev-list --reverse "$root"..HEAD | head -1)
  NAME=$(git show -s --format='%an' "$first");  EMAIL=$(git show -s --format='%ae' "$first")
fi
echo "new root author: $NAME <$EMAIL>"

# If we're already on 'main', move it aside so we can build the clean one.
if [ "$SRC" = "main" ]; then git branch -m main main-preseed; SRC=main-preseed; fi

# New parentless branch whose tree == the seed's tree (empty), re-committed as a proper Initial commit.
git checkout --orphan main "$root"
GIT_AUTHOR_NAME="$NAME" GIT_AUTHOR_EMAIL="$EMAIL" \
GIT_COMMITTER_NAME="$NAME" GIT_COMMITTER_EMAIL="$EMAIL" \
  git commit --allow-empty -m "Initial commit"

# Replay ALL real commits (everything after the seed) onto the new root.
git cherry-pick "$root".."$SRC" || { echo "CONFLICT -- abort with: git cherry-pick --abort; git checkout $SRC; git branch -D main"; exit 1; }
```
(`$SRC` is the original branch, left intact until the push succeeds; step 8 deletes it. The new root has the same empty tree as the seed, so every replayed patch applies cleanly.)

### 6b. real-history path -- no rewrite
```bash
git branch -M "$SRC" main 2>/dev/null || git checkout -b main
```

### 7. Show the result and push
```bash
echo "== main will be published as origin/main =="; git log --oneline main
git push -u origin main
```
- If push **fails on auth** (credential prompt / 401 / 403): stop and tell the user to push themselves with `!git push -u origin main`, or to run the swe-swe auth/setup step first. Do NOT retry blindly.

### 8. After a SUCCESSFUL push only
- Delete the old branch (decision: switch to `main`): if `$SRC` exists and isn't `main`, `git branch -D "$SRC"`.
- Report: origin URL, branch `main`, the pushed commit count, the new root (`Initial commit` by the real author), and confirm no `swe-swe@localhost` commits remain (`git log --format='%ae' main | grep -c swe-swe@localhost` -> 0).

## Notes
- Idempotent-ish: re-running just updates the origin URL and (if `main` already pushed) reports state; once the seed is replaced (no `swe-swe@localhost` in history) it won't re-do the rewrite.
- Never force-push here unless the user explicitly asks and understands origin/main will be overwritten.

Note: If your agent doesn't support slash commands, use `@swe-swe/setup-repo` instead.
