<!-- agent-chat export
title: Docs Staleness Audit And Runtime Flag
date: 2026-07-19
index: 07
slug: docs-staleness-audit-and-runtime-flag
session: f8f62cf6888c1a1f
agent: claude
version: 0.8.16 (b49d6cc)
-->

# Docs Staleness Audit And Runtime Flag

_2026-07-19 · 07 · claude · agent-chat 0.8.16 (b49d6cc)_

**USER**

> Is @README.md and other docs stale/incorrect? Missing out on dockerless mention? 
>
> Btw did we change -with-docker cli flag to -runtime as discussed before? 
>
> Discuss

<small>took 1m 23s</small><br>
**AGENT**

> Audited. Short answers first:
>
> **1. Did we change `--with-docker` -> `--runtime`?** **No.** It is still only a plan. `tasks/2026-07-19-runtime-flag.md` exists, status literally `PLANNED (not started)`, decided today in the "Fix dockerless mode marker lockout" session. `swe-swe init -h` still shows `-with-docker` and `-dockerless` as two separate flags; there is no `-runtime` anywhere in the Go source.
>
> **2. Are README/docs stale?** Yes, in three distinct ways.
>
> **A. Dockerless is invisible in the entry-point docs.**
> - `README.md` -- zero mentions. Requirements still say flatly "Docker & Docker Compose installed". Features list has no dockerless. Documentation list omits `docs/dockerless.md`, `docs/dockerless-mac-vm.md`, `docs/multi-service.md`.
> - `docs/configuration.md` -- the init flag block has **no `--dockerless`**.
> - `docs/cli-commands-and-binary-management.md` -- **0 occurrences** of "dockerless", and it still asserts "All other commands are passed directly to docker compose", which is wrong for a dockerless project (`handleDockerlessCommand` intercepts up/down/etc.).
> - `docs/README.md` (docs index) -- no dockerless, no multi-service.
> - Note `docs/dockerless.md` itself is still written as an aspirational spec ("describes the intended experience ... proposed in tasks/..."), even though it shipped and you have a live DO droplet on it.
>
> **B. The flag reference has drifted from the binary.** Flags in `init -h` but missing from `docs/configuration.md`: `--dockerless`, `--with-skills`, `--proxy-port-offset`, `--metadata-dir`, `--ask`, and all four tunnel flags (`--tunnel-server-url`, `--tunnel-unique`, `--tunnel-client-cert`, `--tunnel-local-ports`). Ironic: README advertises `--with-skills` but the "all init flags" reference doesn't list it.
>
> **C. The built-in `swe-swe --help` text is itself stale** (`cmd/swe-swe/templates.go`) -- a hand-maintained block that omits `--dockerless`, `--with-skills`, `--public-ports`, `--repos-dir`, `--email`, and all tunnel flags, and its `--ssl` line still omits letsencrypt. So the CLI's own help disagrees with `init -h`.
>
> Also unmentioned in README: Procfile/`swe-run` multi-service and the Preview vhost host-demux, both shipped this month.
>
> My suggested order, if you want me to fix: (1) `--help` block + `configuration.md` flag parity -- mechanical, golden-testable; (2) README: dockerless in Requirements/Features + doc links; (3) reframe `docs/dockerless.md` from proposal to shipped; (4) cli-commands doc's dockerless command path. Question: land `--runtime` first or after? Doing docs first means rewriting the flag prose twice.

[Quick replies]
- Do docs first, all 4
- Land --runtime first, then docs
- Just fix flag parity (1) for now
- Just README (2) for now
- Show me the --runtime plan in full

**USER**

> Do docs first, all 4

<small>took 4.6s</small><br>
**AGENT**

> Starting all 4. Step 1: reading the help block in templates.go + configuration.md for flag parity.

<small>took 4m 7s</small><br>
**AGENT**

> All 4 doc passes drafted (plus two extra stale-fact fixes I found: the compose "Available Services" table still listed chrome/code-server/vscode-proxy/auth, and the auth service is embedded now). Building + running tests/goldens.

<small>took 3m 13s</small><br>
**AGENT**

> Done -- all 4 passes landed, uncommitted, `make test` green, `go vet` clean, **no golden churn** (nothing I touched is a template).
>
> **1. Flag parity** -- `cmd/swe-swe/main.go` `printUsage()` + `docs/configuration.md`
> - Added the 10 missing flags to both: `--dockerless`, `--with-skills`, `--proxy-port-offset`, `--metadata-dir`, `--ask`, `--email`, `--public-ports`, `--repos-dir`, and a new **Tunnel Options** section for all four `--tunnel-*` flags.
> - Fixed `--ssl` in the CLI help (was missing letsencrypt) and the agent list (was missing `pi`) in three places.
> - `configuration.md` gained a Dockerless section noting which flags don't apply there (`--with-docker`, `--apt-get-install`, `--npm-install`, `--ssl` -- verified none appear in `dockerless.go`; tunnel flags *do* apply via `loadDockerlessTunnelConfig`).
>
> **2. README** -- dockerless in Requirements, two new Features bullets (dockerless + Procfile/`swe-run`), a "Without Docker" quick-start para, doc links for `dockerless.md`/`dockerless-mac-vm.md`/`multi-service.md`, and a note that compose commands don't apply in dockerless projects. Also added dockerless + multi-service to `docs/README.md` (the index had neither).
>
> **3. `docs/dockerless.md`** -- reframed from press-release ("Not all of it works yet") to shipped, with Phase 6 (native macOS) called out as the remaining gap. Fixed two claims that were **wrong against the code**:
> - "Stop it with `swe-swe down`" -- `down` only prints a reminder; it's a foreground process, Ctrl-C stops it.
> - "The choice is remembered, so later `swe-swe up` reuses it" -- nothing persists `--agent-view`. Replaced with the real mechanism, `SWE_AGENT_VIEW` (verified in `browser_backend.go:90`).
> - Also noted `swe-swe up` forwards extra args to `swe-swe-server`, which is *why* `--agent-view` works there, and that the browser-backend image needs `make browser-backend-image` first.
>
> **4. `docs/cli-commands-and-binary-management.md`** -- added the dockerless command path (up/down/rejected-commands). While in there I hit two staler things than the dockerless gap, and fixed them:
> - The **"Available Services" table was fiction**: it listed `chrome`, `code-server`, `vscode-proxy`, `auth` as separate services. Every golden compose file has only `swe-swe`, plus `traefik` in the 4 SSL variants. Rewrote the table with a "when present" column and purged the `swe-swe up chrome` / `down chrome vscode` examples throughout.
> - The **auth service is gone** -- `init.go:1310` skips its templates and `:1526` actively `RemoveAll`s stale `auth/` dirs. Removed it from the directory-structure tree, the init steps, the build stages, the architecture diagram, and Related Files.
>
> Not touched, flagging instead: the tdspec modules likely carry the same embedded-auth staleness (matches an existing note of yours). And `--runtime` remains unimplemented -- `tasks/2026-07-19-runtime-flag.md` is still the only artifact, so the flag prose above will need one more pass when it lands.
>
> Want me to commit these, or keep going into `--runtime`?

[Quick replies]
- Commit the docs
- Commit, then implement --runtime
- Show me the full diff first
- Also audit tdspec for the same staleness
- Leave uncommitted, I'll review

**USER**

> Question first: --runtime implemented means legacy flag still works but not mentioned anywhere (docs and -h) correct?
>
> If so, yes commit and implement --runtime

<small>took 1m 40s</small><br>
**AGENT**

> Docs committed (a00a5c315). Now on --runtime, starting with the baseline commit per your plan doc. One thing I need to raise before the default flip -- will include it with the results.

<small>took 9m 44s</small><br>
**AGENT**

> Answering your question first: **yes, exactly that.** Your plan doc's compat rules say legacy flags "KEEP WORKING indefinitely but become UNDOCUMENTED" -- hidden from `-h` via a custom usage func, and scrubbed from README/docs/interactive prompts. Nothing breaks for existing scripts or saved configs; they just stop being advertised.
>
> Two commits pushed to local main:
>
> **a00a5c315** -- the docs pass (as reported before).
>
> **9b6b8dd0a** -- `--runtime` baseline, i.e. commit 1 of your TDD workflow. Parsed, validated, persisted to `init.json`; every dispatch still runs off the legacy booleans, which the resolver derives. `make test` green, `make ascii-check` green, and the golden diff is **init.json only**, exactly as your plan predicted.
>
> - `--runtime=container-with-docker-socket` produces output **byte-identical** to `--with-docker` (`diff -r` on the two golden targets is empty).
> - Agreeing `--runtime` + legacy flag is accepted; contradicting is a hard error. `--with-docker --dockerless` together is now rejected too -- previously it silently took the dockerless path.
> - Bonus bug fix your plan implies: `Dockerless` was `json:"-"`, never persisted, and the reuse block never restored it -- so `--previous-init-flags=reuse` on a dockerless project **silently re-inited it as a container**. Reuse now restores the runtime as a unit, falling back to the legacy field + on-disk marker for older init.json files.
> - Goldens: `runtime-container`, `runtime-container-with-docker-socket`, plus `runtime-invalid` / `runtime-conflict-legacy`. The last two are stderr-only (a rejected init writes no project), so they get a dedicated `TestGoldenRuntimeRejections` instead of the `TestGoldenFiles` table. **No `runtime-host` fixture** -- a host init dumps the embedded binaries and those don't belong in testdata. That's a deviation from your plan; say the word if you want it anyway.
>
> **Before commit 2, I want to flag the default flip -- I think it should NOT ship yet.** Three things argue against `--runtime` defaulting to `host` right now:
>
> 1. **Your own plan says so.** Its Related section: `tasks/2026-07-19-dockerless-boot-parity.md` "blocks making `host` a GOOD default, since host mode currently lacks slash-commands/skills/non-Claude agent setup."
> 2. **Windows would hard-fail.** `dockerlessGOOSGuard` returns an error for anything that isn't linux/darwin, and the guard runs before anything is written. A bare `swe-swe init` on a Windows CLI build would just exit 1. Today it works.
> 3. **macOS would silently degrade.** darwin is allowed but prints `dockerlessDarwinWarning` -- the per-session credential broker and PTY recording aren't ported (Phase 6). Bare `init` on a Mac would go from "works" to "experimental".
>
> I left the default at `container` in the baseline. My recommendation: **ship commit 2 without the flip** -- dispatch off `Runtime`, hide the legacy flags from `-h`, add the interactive runtime question, sweep the docs -- and flip the default in a separate commit once boot parity lands, gating on GOOS so non-Linux stays on `container`.
>
> Which way?

[Quick replies]
- Agreed, commit 2 without the flip
- Flip it anyway, host default everywhere
- Flip but only on Linux hosts
- Stop here, I'll review the two commits
- Add the runtime-host golden too

**USER**

> Agree no flip. But I want the json for the new “runtime” attribute to be used. Unmarshal can understand old flags but should write out both old and new. Discuss

<small>took 38.4s</small><br>
**AGENT**

> Plan: move the derivation out of the reuse block and into `loadInitConfig` / `saveInitConfig`, so every caller sees a populated `Runtime`.
>
> 1. **`loadInitConfig` normalizes on read.** After unmarshal, if `runtime` is absent, set it from the legacy fields: `withDocker:true` -> `container-with-docker-socket`, else `container`. Old init.json files stop returning `Runtime == ""`.
> 2. **`saveInitConfig` derives legacy on write.** Set `withDocker = runtimeWithDocker(cfg.Runtime)` before marshalling, so `runtime` is the single input and the legacy key can never drift from it. Both keys land in the file.
> 3. **Reuse block shrinks to `savedRuntime := savedConfig.Runtime`** -- no inline fallback, no marker lookup. The fallback lives in step 1.
>
> That also fixes three callers I hadn't touched: `docker.go:192`, `dockerless.go:250`, `list.go:81` all call `loadInitConfig` and would read `Runtime == ""` on any pre-existing project.
>
> **One decision I need from you: does `dockerless` become a persisted json key too?**
>
> Right now `Dockerless` is `json:"-"` -- the on-disk marker file (`.swe-swe/mode`) is the source of truth, and `docker.go:92` dispatches `up` off `isDockerlessProject()`, not off init.json.
>
> - **Persist it** -- "write out both old and new" is then literally true for both legacy flags, and step 1's fallback for old dockerless projects needs no marker read. Cost: two sources of truth for host mode. If someone hand-edits init.json, `init` and `up` disagree.
> - **Leave it `json:"-"`** (my recommendation) -- `runtime: "host"` in json IS the new representation, and the marker stays the single thing `up` dispatches on. Step 1 then reads the marker only when `runtime` is absent, i.e. only for files written before today. That path disappears on the first re-init.
>
> I lean leave-it: adding a second writable home for a fact that already has one buys nothing once `runtime` exists.
>
> Which one? Then I'll implement steps 1-3 and re-run goldens -- about 20 minutes.

[Quick replies]
- Leave dockerless as json:"-"
- Persist dockerless too
- Explain the marker vs init.json split first

**USER**

> Leave dockerless as json:"-"

<small>took 5m 28s</small><br>
**AGENT**

> `ddd22000c` -- `runtime` is now the authority in init.json. Three commits on local main, `make test` green, `make ascii-check` green.
>
> What changed:
>
> 1. **`saveInitConfig` derives `withDocker` from `Runtime`** instead of writing it as an independent value. Both keys land in the file, so an older CLI keeps reading `withDocker` and a newer one reads `runtime` -- but they cannot drift, because there is only one input.
> 2. **`loadInitConfig` backfills `Runtime`** when it is absent: from `withDocker`, or from the on-disk mode marker for host projects. That path only runs for files written before today and disappears on the next init.
> 3. **Reuse block shrank** to reading `savedConfig.Runtime` -- no inline fallback, no marker lookup.
>
> `Dockerless` stays `json:"-"` as you decided; `runtime: "host"` is its representation in the file and the marker stays the only thing `swe-swe up` dispatches on.
>
> This reached three callers the reuse-block version never did: `docker.go:192`, `dockerless.go:250`, `list.go:81` all load init.json and would have read `Runtime == ""` on any pre-existing project.
>
> Golden churn was one file -- the interactive `--ask` path builds its config directly, so it had no runtime key; the save-time backfill gives it one. Verified `with-docker` writes both keys: `"withDocker": true` and `"runtime": "container-with-docker-socket"`.
>
> New tests: `TestNormalizeRuntimeForLoad` (6 cases incl. stale-legacy-key and marker precedence), `TestNormalizeRuntimeForSave` (5 cases), `TestRuntimeJSONRoundTrip` (all 3 modes, save then load with no marker present). One stale assertion fixed: `TestSaveLoadInitConfig` compared original against loaded, which no longer matches now that save backfills.
>
> Next: commit 2 of the flag work -- dispatch off `Runtime`, hide the legacy flags from `-h`, add the runtime question to interactive init, and sweep `--with-docker` / `--dockerless` out of the docs. No default flip. Roughly an hour. Start it?

[Quick replies]
- Start commit 2
- Show me the init.json diff first
- Just hide the legacy flags for now
- Stop here, I'll review the three commits

**USER**

> Show me the init.json diff first

**USER**

> Start commit 2

<small>took 5.0s</small><br>
**AGENT**

> One added line per file, nothing removed. Diff across both commits (`a00a5c315..HEAD`):
>
> **Bare `swe-swe init`**
> ```diff
>    "proxyPortOffset": 20000,
> +  "runtime": "container",
>    "cliVersion": "dev"
> ```
>
> **`swe-swe init --with-docker`** (legacy flag, unchanged behavior)
> ```diff
>    "proxyPortOffset": 20000,
> +  "runtime": "container-with-docker-socket",
>    "cliVersion": "dev"
> ```
>
> **`swe-swe init --ask`** (interactive) -- same as bare init: `+ "runtime": "container",`
>
> Both legacy and new keys are present. Full file for `--with-docker`:
> ```json
>   "withDocker": true,
>   ...
>   "runtime": "container-with-docker-socket",
>   "cliVersion": "dev"
> ```
>
> `--with-docker` and `--runtime=container-with-docker-socket` produce **byte-identical init.json** -- the two golden files differ in nothing but their path-derived directory name.
>
> No `dockerless` key anywhere, as you decided. A host project is `"runtime": "host"` plus the `.swe-swe` mode marker.
>
> Next: start commit 2 -- dispatch off `Runtime`, hide legacy flags from `-h`, interactive runtime question, docs sweep. No default flip. About an hour.

<small>took 17.4s</small><br>
**AGENT**

> Commit 2, step 1 of 4: making WithDocker/Dockerless derived accessors so everything dispatches off Runtime.

<small>took 1m 51s</small><br>
**AGENT**

> Step 1 of 4 done: all dispatch now runs through Runtime accessors (init.go + list.go), tests green. Step 2: hiding the legacy flags from help output.

<small>took 3m 0s</small><br>
**AGENT**

> Steps 1-3 done (accessors, hidden legacy flags, interactive runtime question). Step 4 of 4: sweeping --with-docker / --dockerless out of the docs.

<small>took 8m 29s</small><br>
**AGENT**

> `23ae9d670` -- commit 2 done. Four commits on local main, `make test` green, `make ascii-check` green, live-smoke-tested.
>
> What `--runtime` now does:
>
> 1. **Drives all behavior.** `WithDocker` / `Dockerless` are no longer read anywhere; every call site goes through `withDockerSocket()` / `isHostRuntime()`, derived from `Runtime`. The fields survive only as the legacy json key and as inputs for configs built in code.
> 2. **Legacy flags hidden, not removed.** `swe-swe init -h` no longer lists `--with-docker` / `--dockerless`; the top-level usage documents `--runtime` in their place. `TestDeprecatedInitFlagsHiddenButUsable` pins both halves -- absent from usage, still parses.
> 3. **Interactive init asks where it runs** instead of Docker-socket yes/no. It can now produce a host project at all: it previously always called `executeInit`, so host mode was unreachable interactively. The host option only appears where `dockerlessGOOSGuard` admits the platform.
> 4. **Docs swept** -- README, docs/, the shipped `.swe-swe/docs/` and the procfile slash command all say `--runtime=...`.
>
> Live smoke test on a real init:
>
> | invocation | result |
> |---|---|
> | `--runtime=container-with-docker-socket` | `"withDocker": true` + `"runtime": "container-with-docker-socket"` |
> | `--with-docker` (legacy) | same `runtime` value -- still works |
> | `--runtime=host` | `"runtime": "host"`, `mode` marker written, 15 binaries in `bin/` |
> | `--previous-init-flags=reuse` on that host project | **stays host** -- the bug from commit 1, confirmed fixed end to end |
>
> Default is still `container`. Golden churn was the shipped docs + procfile command text only.
>
> Left alone on purpose: `scripts/*.sh`, `Makefile`, `deploy/`, `e2e/` still pass the legacy flags. They work unchanged, and rewriting load-bearing test infra deserves its own commit with an e2e run behind it. `tasks/`, `agent-chats/`, `CHANGELOG.md` and ADR-0013 are historical records.
>
> Two things I found but did not act on:
> - `cmd/swe-swe/templates/host/swe-swe-server/container-templates/.swe-swe/docs/docker.md` is a **stale dead duplicate** -- it is embedded in the binary, written at init, then immediately `RemoveAll`'d and replaced from `templates/container/`. Its content predates the Procfile rewrite entirely.
> - `tasks/2026-07-19-runtime-flag.md` still says PLANNED and still specifies the default flip.
>
> Next, pick one: update the task doc to reflect what shipped (5 min), or delete the dead duplicate template (15 min incl. golden run).

[Quick replies]
- Update the task doc
- Delete the dead duplicate template
- Both
- Push all four commits
- Stop here, I'll review

**USER**

> See if this review of our plan (before we execute md) is relevant
>
> [review.md.pdf](./assets/2026-07-19-07-1-ad42075eba52.pdf)

**USER**

> Also delete the dead duplicate template

<small>took 3m 48s</small><br>
**AGENT**

> All four review findings were real. Fixed in `4dece7b89`; five commits on local main, `make test` green.
>
> | # | Finding | Verdict |
> |---|---|---|
> | 1 | `--tunnel-local-ports` documented as applying in host mode | **Confirmed, worst of the four** |
> | 2 | `swe-swe build traefik` targets an image with no build section | Confirmed |
> | 3 | dockerless TL;DR says "nothing else" but writes `.mcp.json` | Confirmed, and understated |
> | 4 | README contradicts the new "macOS experimental" wording | Confirmed |
>
> **On #1** -- the review is right and it matters more than it says. `dockerless.go:240` calls `TunnelLocalPorts` "intentionally absent", and `dockerless_test.go` pins it as a regression test with the comment "swe-swe-server has no such flag and exits 2 (usage dump) if passed." That is the exact crash-loop that took down the first dockerless droplet. My sentence "the tunnel flags do [apply]" pointed users straight at it. Now: the other three tunnel flags apply, `--tunnel-local-ports` explicitly does not, with the reason.
>
> **On #2** -- verified: `docker-compose.yml:3-4` is `image: traefik:v2.11`, only `swe-swe` has a `build:` section. Changed to `swe-swe build swe-swe`.
>
> **On #3** -- the review found `.mcp.json`; init also writes `.claude/settings.local.json` with the hook guards. Documented both, plus the fact that nothing touches the global `~/.claude`.
>
> **On #4** -- I introduced that contradiction in the same commit. README now separates "native macOS is experimental" from "the Linux VM is the proven setup".
>
> Separately, on the duplicate template: **I was wrong to call it "checked-in".** `.gitignore:21` ignores `cmd/swe-swe/templates/host/swe-swe-server/container-templates/` -- it was never in the repo, just stale local build output on this box. It still mattered, because `//go:embed all:templates` does not respect `.gitignore`, so local builds were embedding four stale doc files that a clean checkout never would. Deleted from disk: zero golden churn, `make test` still green (`test-server` recreates it in /tmp from `templates/container/`), and it did not regenerate. Nothing to commit.
>
> The review only saw `a00a5c315`. Commits 2-4 (`--runtime`) came after and were not covered.
>
> Next: send the reviewer the `--runtime` commits (`9b6b8dd0a..23ae9d670`), or update `tasks/2026-07-19-runtime-flag.md` to match what shipped?

[Quick replies]
- Update the task doc
- Get the --runtime commits reviewed
- Push all five commits
- Stop here, I'll review

**USER**

> Update the task doc

<small>took 1m 26s</small><br>
**AGENT**

> `471d4f9ce` -- task doc now matches reality. Six commits on local main.
>
> `tasks/2026-07-19-runtime-flag.md` changes:
>
> 1. **Status**: `PLANNED (not started)` -> `SHIPPED 2026-07-19, EXCEPT the default flip`, with the three commit SHAs.
> 2. **Decision section** no longer claims the default is `host` -- it says `container (unchanged)` and points at the Deferred section.
> 3. **Compat rules** rewritten as-built, including the reuse bug the work exposed and the fact that `--with-docker --dockerless` together is now an error (it used to silently take the dockerless path).
> 4. **Two deviations recorded**: no `runtime-host` golden fixture (a host init dumps binaries, which don't belong in testdata), and the rejection variants needing `TestGoldenRuntimeRejections` because a rejected init writes no project to diff.
> 5. **New "Deferred" section** with the three reasons the flip didn't ship -- boot parity, Windows hard-fail, macOS degradation -- plus what a future flip still needs (a GOOS gate) and what's already in place for it.
> 6. **New "Also not done"**: our automation still passes the legacy flags on purpose.
>
> Two plan claims I checked rather than copied: the sweep list named `www/` and `docs/dev/*.md`, and neither contains `--with-docker` or `--dockerless` -- noted in the doc so nobody re-hunts them.
>
> Also saved a memory note so a future session doesn't flip the default without the GOOS gate.
>
> All six commits are local and unpushed:
> ```
> 471d4f9ce docs(tasks): runtime-flag plan reflects what shipped
> 4dece7b89 docs: fix four claims the implementation does not support
> 23ae9d670 feat(init): dispatch off --runtime, retire the legacy flags from docs
> ddd22000c feat(init): make `runtime` the authority in init.json
> 9b6b8dd0a feat(init): parse --runtime (baseline, no behavior change)
> a00a5c315 docs: cover dockerless, restore flag parity, purge stale service/auth facts
> ```
>
> Next: push them? (Reminder from a past session: the first `git push origin main` rewrites SHAs and aborts by design -- just push again, don't force.)

[Quick replies]
- Push all six
- Not yet, I'll review first
- Get the --runtime commits reviewed
- Wrap up the session

**USER**

> Wrap up the session

