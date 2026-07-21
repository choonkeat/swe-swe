<!-- agent-chat export
title: Prctx Token Env And Agent Usable Help
date: 2026-07-22
index: 05
slug: prctx-token-env-and-agent-usable-help
session: 80416732e5f84074
agent: claude
version: 0.8.17 (a9ccfee)
-->

# Prctx Token Env And Agent Usable Help

_2026-07-22 · 05 · claude · agent-chat 0.8.17 (a9ccfee)_

**USER**

> Look at this patch for prctx vs the changes we made
>
> *(attached: prctx-gitlab-dedicated-patch.tgz -- not archived; it bundled a prebuilt binary)*

<small>took 1m 2s</small><br>
**AGENT**

> Don't apply the patch — it would conflict, and we already ship a superset of part 1. Only `--token-env` is genuinely missing.
>
> **Verified just now on main (e9f0f40dc):**
> ```
> prctx fetch https://git.gitlab-dedicated.example.com/grp/sub1/sub2/repo/-/merge_requests/5
>   -> "GITLAB_TOKEN is not set"   (provider resolved, 4-level subgroup parsed)
> prctx fetch https://git.corp.example/o/r/pull/5
>   -> "GITHUB_TOKEN is not set"
> go vet + go test ./cmd/prctx: ok
> ```
>
> **Patch item 1 (self-hosted hosts) — already done, better.** Our 465669d8f resolves in order: `PRCTX_GITHUB_HOSTS`/`PRCTX_GITLAB_HOSTS` → URL shape (`/pull/` vs `/-/merge_requests/`, carried as `PRRef.Kind` and persisted in state so bare-number follow-ups work) → `github.*`/`gitlab.*` prefix. The patch instead uses `strings.Contains(host, "gitlab")`, which needs no config but misroutes a GitHub Enterprise box named e.g. `gitlab-migration.corp.com`. Our `hostListed` also accepts `https://` prefixes and is case-insensitive; the patch's does exact-string match only. We additionally have `PRCTX_GITLAB_API_BASE` / `PRCTX_GITHUB_API_BASE` / `PRCTX_GITHUB_GRAPHQL_URL` for subpath installs — the patch has none.
>
> **Patch item 2 (`--token-env NAME`) — we don't have it.** Real gap, but ~20 lines: an `extractTokenEnv(args)` pre-pass plus `lookupToken(defaults...)` in both providers. The workaround today is `GITLAB_TOKEN=$MY_VAR prctx fetch ...`.
>
> **Why `git apply` fails:** the patch is against aa4dd9904 (v2.32.0) and rewrites the exact `providerFor` block and `usage()` text that 465669d8f already replaced, and it defines its own `hostListed` with a different signature (`(key, host)` reading the env itself vs our `(list, host)`).
>
> Want me to port just `--token-env` onto main (source + the 4 `templates/host/prctx/` copies + golden-update)? About 20 minutes.

[Quick replies]
- Yes, port --token-env
- No, skip it
- Show me the lookupToken diff first

**USER**

> Yes, port --token-env

**USER**

> Improve our cli `-h` too, so agent can use it well.
>
> Btw how does swe-swe know how/when to use it? Do we have a 1-liner intro in our bundled @.swe-swe/docs/AGENTS.md or something?

<small>took 2m 53s</small><br>
**AGENT**

> Committed 596ed9b0b on main (unpushed). Try it:
>
> ```
> prctx --token-env GITLAB_DEDICATED_TOKEN fetch <mr-url>
> ```
>
> **Live-verified just now:**
> ```
> MY_GL_TOKEN=xyz prctx --token-env MY_GL_TOKEN fetch https://git.gitlab-dedicated.example.com/grp/sub1/sub2/repo/-/merge_requests/5
>   -> reached GET /api/v4/projects/grp%2Fsub1%2Fsub2%2Frepo/merge_requests/5  (token used, only DNS failed)
> prctx fetch --token-env ABSENT_TOK <mr-url>
>   -> "ABSENT_TOK / GITLAB_TOKEN is not set"   (flag works after the subcommand too)
> make test: EXIT=0, 10 packages ok, prctx bundle in sync, ASCII check passed
> ```
>
> **What changed** — 204 files, but only 4 distinct ones (the rest is the golden-file fan-out):
> 1. `cmd/prctx/main.go` — `extractTokenEnv(args)` pre-pass in `run()` + `lookupToken(defaults...)` + usage text.
> 2. `cmd/prctx/{github,gitlab}.go` — both `token()` methods go through `lookupToken`.
> 3. `cmd/prctx/prctx_test.go` — 3 new tests (flag placement/spellings/dangling value, override precedence + fallback, error naming every var).
> 4. `slash-commands/swe-swe/pr-discuss.md` — documents the flag in the Auth section.
>
> **Two deliberate departures from the patch:** the override does *not* replace the defaults — an empty or unset `--token-env NAME` falls through to `GITLAB_TOKEN` rather than hard-failing; and I kept the `os` imports in both providers, which the patch deleted (they're still needed for our `PRCTX_*_API_BASE` overrides, which didn't exist at aa4dd9904 — applying the patch as-is would have broken the build).
>
> I did not port the patch's `strings.Contains(host, "gitlab")` heuristic, as discussed — it would misroute a GitHub Enterprise host named `gitlab-migration.corp.com`, and our URL-shape detection already covers the Dedicated case without config.
>
> Next: run it against the real GitLab Dedicated MR to confirm end-to-end, then push main.

<small>took 4m 8s</small><br>
**AGENT**

> Committed 6c76dd197. Run `go run ./cmd/prctx -h` to see the new help.
>
> **Answer to your question: no, there was nothing.** `prctx` was not named in the bundled `.swe-swe/docs/AGENTS.md` at all. The only route to it was the `/swe-swe:pr-discuss` slash command, which Goose and Aider never see, and which even a slash-capable agent won't reach for if the user just pastes a PR url. Fixed: added a `## Tools on PATH` section with a one-liner ("read a PR/MR's review comments, stage replies locally, then post them upstream... nothing is sent until `prctx flush`... run `prctx -h`"), plus the missing `pr-discuss` row in the command table.
>
> **`-h` now opens with what the tool is for and a copyable session:**
> ```
> prctx fetch https://github.com/o/r/pull/42   # 1. pull threads, prints them
> prctx show --json                            # 2. machine-readable, for deciding
> prctx reply $ID "Fixed in abc1234."          # 3. stage answers (nothing sent)
> prctx comment src/main.go:88 "Leaks here."   #    stage a NEW comment
> prctx flush                                  # 5. post it all upstream
> ```
> Plus explicit argument shapes and a "Notes for agents" block: staging never touches the server, `flush` is idempotent (won't double-post after a partial failure), `flush` refuses once local HEAD moves past the fetch, prctx never writes to git, state lives in `~/.local/state/prctx`.
>
> Two things I corrected while writing it — the draft I started from claimed thread ids look like `t1`/`t2`. They don't; they're opaque provider ids (GitHub GraphQL node id, GitLab discussion sha), so the help now says copy them verbatim. And the `--force` guard is "local HEAD moved since fetch", not "unpushed commits" — worded accordingly.
>
> `make test` EXIT=0 both commits. Main is now 2 ahead: 596ed9b0b (`--token-env`) + 6c76dd197 (docs), both unpushed.
>
> **Separately:** that AGENTS.md command table lists 3 of 17 slash commands — `execute-in-worktree`, `merge-worktree`, `save-session`, `procfile` and 10 others are absent, and it's a hand-maintained static file so it will drift again. Want me to generate it from `slash-commands/swe-swe/*.md` frontmatter instead? About 30 minutes.

[Quick replies]
- Yes, generate the table
- Push main first
- Leave the table alone

**USER**

> /wrap-up

