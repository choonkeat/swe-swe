# `--runtime` flag: unify `--with-docker` / `--dockerless`

Status: PLANNED (not started)
Decided: 2026-07-19 (chat session "Fix dockerless mode marker lockout")

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

**Default: `host`.** This is a deliberate default flip: a bare `swe-swe init`
on a fresh machine becomes a host-native (no Docker required) setup. Chosen
to match the dockerless single-binary roadmap (docs/dockerless.md,
tasks/2026-06-27-dockerless-single-binary.md) where Docker is optional.

## Compatibility rules

- Legacy flags KEEP WORKING indefinitely but become UNDOCUMENTED (removed
  from --help text? No: keep in `flag` parsing but drop from README/docs/
  interactive prompts. Decide whether to hide from -h via a custom usage
  func -- preferred: hide).
- Legacy flags naturally overwrite the new default when present:
  - `--dockerless`            -> runtime=host
  - `--with-docker`           -> runtime=container-with-docker-socket
  - (neither, no --runtime)   -> runtime=host (the new default)
- Explicit `--runtime` + a CONFLICTING legacy flag in the same invocation
  (e.g. `--runtime=host --with-docker`) -> hard error, ambiguous intent.
  `--runtime` + a legacy flag that AGREES is accepted.
- BUT note: prior to this change, bare `swe-swe init` (no legacy flags)
  meant `container`. The default flip changes behavior for that invocation.
  Mitigations:
  - `--previous-init-flags=reuse` (and the auto-reuse path in
    `checkAndUpgrade`) must derive runtime from the SAVED config's
    `Dockerless`/`WithDocker` fields, so existing projects re-init into the
    same mode they had. The flip only affects genuinely fresh projects.
  - restart-loop2.sh-style automation always passes explicit flags; audit
    our own scripts (`.swe-swe/restart-loop2.sh`, `.swe-swe/pre-restart.sh`,
    `.swe-swe/restart-dockerless.sh`, e2e scripts) and add explicit
    `--runtime` to each.
- InitConfig: add `Runtime string` (json `runtime`); keep writing the legacy
  `Dockerless`/`WithDocker` bools for one release cycle so older CLIs can
  still read a newer init.json (they ignore unknown keys anyway -- verify).
  Loading order: `runtime` field wins; fall back to deriving from the bools.

## Implementation plan (two-commit TDD per CLAUDE.md)

1. **Baseline commit**: parse `--runtime` (validate enum), map legacy flags
   + conflict detection, store `Runtime` in InitConfig, no behavior change
   yet (executeInit/executeDockerlessInit still dispatch off the old bools,
   which the mapper sets). Golden test variants:
   - `runtime-host`, `runtime-container`, `runtime-container-with-docker-socket`
   - `runtime-conflict-legacy` (expect error)
   - legacy-flag variants unchanged (prove no regression)
   `make build golden-update`, verify diff shows init.json only.
2. **Implementation commit**: dispatch off `Runtime` everywhere
   (`config.Dockerless`/`config.WithDocker` become derived accessors),
   default flip to host, interactive init (`runInteractiveInit`) asks the
   runtime question with the three options, hide legacy flags from usage.
   `make build golden-update`; expect golden churn in docker-compose.yml /
   Dockerfile presence for the default-variant fixtures.
3. **Docs sweep**: README.md, docs/dockerless.md, docs/dev/*.md,
   www/ (if it references init flags), slash-command sources under
   cmd/swe-swe/slash-commands/, interactive prompts. Replace every
   `--with-docker` / `--dockerless` mention with `--runtime=...`. Do NOT
   document the legacy flags anywhere.

## Open questions

- Interactive init default answer: `host` to match the flag default, or
  keep suggesting container on machines where docker is detected? (Lean:
  always host, mention docker modes as alternatives.)
- Should `swe-swe up` print the active runtime at startup? (Cheap, helps
  support; lean yes.)

## Related

- tasks/2026-07-19-dockerless-boot-parity.md (entrypoint duty parity --
  blocks making `host` a GOOD default, since host mode currently lacks
  slash-commands/skills/non-Claude agent setup)
- Dockerless mode marker lockout fix: ac5b9b03d (docker-mode init clears
  the `mode` marker)
