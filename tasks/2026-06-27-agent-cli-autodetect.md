# Agent CLI autodetect on boot (host-agnostic)

## Status

**Planned.** Not started.

Applies regardless of dockerless -- it improves both modes.

## Premise

`swe-swe-server` should decide which coding-agent CLIs are available by
probing the host at boot (`which` / `exec.LookPath`), not by being told
at init time. Then:

- **docker mode:** agents are chosen at `swe-swe init`, preinstalled by
  the Dockerfile/entrypoint, and the server picks them up dynamically.
- **dockerless mode:** nothing to specify -- the server picks up whatever
  agent CLIs are already on the host's PATH.

## Good news: most of this already works

`detectAvailableAssistants()` (`main.go:1466`, called at `main.go:1948`)
already does exactly this:

- It walks the static `assistantConfigs` table and includes any whose
  `Binary` is found via `exec.LookPath` (`main.go:1477`).
- It logs each detected assistant and errors only if none are found
  (`main.go:1494`: "install claude, gemini, codex, goose, or aider; or
  provide -shell flag").
- The detection table is independent of what `swe-swe init` selected --
  a host with `gemini` installed is detected even if init never mentioned
  gemini.

So the server is **already host-agnostic** about agents. The remaining
work is to make that the intended, tested, documented contract and to
remove any vestigial coupling.

## Work

1. **Lock it in with a test.** A unit/integration test that, given a
   fake PATH containing only (say) `gemini`, `detectAvailableAssistants`
   yields exactly Gemini (+ always-available non-homepage entries). Guard
   against future regressions that re-introduce init-time gating.

2. **Decouple init agent selection from detection.** Confirm and codify
   that `swe-swe init`'s agent flags only control docker-image preinstall
   (`{{IF NODEJS}}` / npm-install in the Dockerfile, `templates.go`), and
   are a **no-op for dockerless** -- the user does not pass them. If any
   code path still requires an init-declared agent to enable it server
   side, remove that requirement.

3. **Make the table the single source of truth.** Audit anywhere the set
   of agents is duplicated (init flags, Dockerfile install lines, the
   homepage UI, `assistantConfigs`). Ideally derive the others from one
   table so adding an agent is one edit. At minimum, document the
   relationship.

4. **Surface detection in the UI / logs.** Ensure the homepage selection
   reflects exactly the detected set (it reads `availableAssistants`),
   and that boot logs make "what did I find on this host" obvious for
   dockerless debugging.

5. **Docs.** State the contract: "swe-swe-server detects agent CLIs on
   PATH at startup; in dockerless mode you just install the CLI you want
   and run." Cross-link from `docs/dockerless.md`.

## Verify

- Test from step 1 green via `make test`.
- Dockerless: with only `claude` on PATH, homepage offers Claude; add
  `gemini` to PATH, restart, homepage offers both -- no init change.
- Docker mode: unchanged behavior (selected agents preinstalled +
  detected).

## Non-goals

- Hot re-detection without restart (detection stays boot-time).
- Auto-installing missing agents (out of scope; that's the image's job in
  docker mode and the user's job in dockerless).
