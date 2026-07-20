# `--runtime` flag: unify `--with-docker` / `--dockerless`

Status: SHIPPED 2026-07-19, EXCEPT the default flip (see "Deferred" below)
Decided: 2026-07-19 (chat session "Fix dockerless mode marker lockout")
Implemented: 2026-07-19 (chat session "Docs staleness and runtime flag audit")

Commits, oldest first:

- `9b6b8dd0a` baseline -- parse, validate, persist; no behavior change
- `ddd22000c` make `runtime` the authority in init.json
- `23ae9d670` dispatch off `Runtime`; retire the legacy flags from docs

## Problem

`swe-swe init` grew two flags that sound related but control different axes:

- `--with-docker` -- give the agent Docker access INSIDE the workspace
  container (mount host docker socket, socket group perms in entrypoint.sh).
- `--dockerless` -- do not use containers at all; dump host-native binaries
  and run swe-swe-server directly on the host.

"with-docker" vs "dockerless" reads like a boolean pair but is not: one is a
container capability, the other is a deployment mode. Confusing to document,
confusing to reason about.

## Decision

Replace both (in the UI/docs sense) with a single enum flag:

```
--runtime = container | container-with-docker-socket | host
```

- `container` -- today's default docker-compose mode WITHOUT docker socket.
- `container-with-docker-socket` -- today's `--with-docker`.
- `host` -- today's `--dockerless`.

**Default: `container` (unchanged).** The plan called for flipping this to
`host`; that is deferred, see below.

## Compatibility rules

All implemented as specified:

- Legacy flags KEEP WORKING indefinitely but are UNDOCUMENTED. `swe-swe init`
  gets a custom `fs.Usage` (`printInitFlagUsage`) that omits anything in
  `deprecatedInitFlags`; the top-level usage documents `--runtime` in their
  place. `TestDeprecatedInitFlagsHiddenButUsable` pins both halves.
- Legacy flags map onto the new enum:
  - `--dockerless`            -> runtime=host
  - `--with-docker`           -> runtime=container-with-docker-socket
  - (neither, no `--runtime`) -> runtime=container
- Explicit `--runtime` + a CONFLICTING legacy flag -> hard error. `--runtime`
  + a legacy flag that AGREES is accepted. `resolveRuntime` owns both rules.
- `--with-docker --dockerless` together is now also an error. It used to be
  accepted and silently took the dockerless path.
- InitConfig has `Runtime string` (json `runtime`). `runtime` is the single
  input to what gets written: `saveInitConfig` DERIVES the legacy `withDocker`
  key from it, so the two cannot drift apart in a file. `loadInitConfig`
  backfills `Runtime` for older files -- from `withDocker`, or from the
  on-disk mode marker for host projects. `Dockerless` stays `json:"-"`:
  `runtime: "host"` is its representation in the file, and the marker remains
  the only thing `swe-swe up` dispatches on.

### Bug this exposed and fixed

`Dockerless` was never persisted and the reuse block never restored it, so
`--previous-init-flags=reuse` on a dockerless project silently re-inited it
as a container. Reuse now restores the runtime as a unit. Verified live:
`init --runtime=host` then `init --previous-init-flags=reuse` keeps
`"runtime": "host"`, the mode marker, and the dumped binaries.

## What shipped, against the plan

1. **Baseline commit** -- as planned. Golden diff was init.json only.
   Golden variants: `runtime-container`,
   `runtime-container-with-docker-socket`, `runtime-invalid`,
   `runtime-conflict-legacy`.
   - The last two are stderr-only (a rejected init writes no project), so
     they cannot join the `TestGoldenFiles` table; `TestGoldenRuntimeRejections`
     asserts them instead.
   - **No `runtime-host` fixture**, contrary to the plan: a host init dumps
     the embedded binaries, which do not belong in testdata.
2. **Implementation commit** -- as planned minus the default flip.
   `config.Dockerless` / `config.WithDocker` are no longer read anywhere;
   `withDockerSocket()` / `isHostRuntime()` derive from `Runtime`.
   Interactive init asks the runtime question and can now produce a host
   project at all -- it previously always called `executeInit`, so host mode
   was unreachable interactively. Golden churn was the shipped `.swe-swe`
   docs and procfile command text, not compose/Dockerfile presence (that
   would only have come from the default flip).
3. **Docs sweep** -- README, docs/, the shipped container docs under
   `templates/container/.swe-swe/docs/`, and the procfile slash command.
   `www/` and `docs/dev/*.md` had no `--with-docker` / `--dockerless`
   mentions to sweep.

## Deferred: the default flip to `host`

Not shipped, for three reasons:

1. `tasks/2026-07-19-dockerless-boot-parity.md` blocks it -- host mode still
   lacks slash-commands/skills/non-Claude agent setup, so `host` is not yet a
   GOOD default. (This plan's own Related section already said so.)
2. **Windows would hard-fail.** `dockerlessGOOSGuard` errors on anything that
   is not linux/darwin, and the guard runs before anything is written. A bare
   `swe-swe init` on a Windows CLI build would exit 1 where it works today.
3. **macOS would silently degrade.** darwin is admitted but prints
   `dockerlessDarwinWarning`: the per-session credential broker and PTY
   recording are not ported (Phase 6).

When boot parity lands, the flip needs a GOOS gate so non-Linux stays on
`container`. Everything else it needs is already in place: `resolveRuntime`
takes the default as a parameter, and reuse derives from the saved config,
so the flip only ever affects genuinely fresh projects.

## Also not done

- **Our own automation still passes the legacy flags**: `scripts/*.sh`,
  `Makefile`, `deploy/`, `e2e/`. They work unchanged. Rewriting load-bearing
  test infra deserves its own commit with an e2e run behind it.
- `swe-swe up` does not print the active runtime at startup (was an open
  question; still open, still cheap).

## Open questions, resolved

- Interactive init default answer: **`container`**, matching the flag
  default. The prompt offers `d` for the docker-socket mode and `h` for host,
  and only offers `h` where `dockerlessGOOSGuard` admits the platform, so an
  unsupported host cannot pick a mode that will not init.
- `swe-swe up` printing the active runtime: still open.

## Related

- tasks/2026-07-19-dockerless-boot-parity.md (entrypoint duty parity --
  blocks making `host` a GOOD default, since host mode currently lacks
  slash-commands/skills/non-Claude agent setup)
- Dockerless mode marker lockout fix: ac5b9b03d (docker-mode init clears
  the `mode` marker)
