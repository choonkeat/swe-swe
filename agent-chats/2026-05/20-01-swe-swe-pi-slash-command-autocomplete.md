<!-- agent-chat export
title: Swe Swe Pi Slash Command Autocomplete
date: 2026-05-20
index: 01
slug: swe-swe-pi-slash-command-autocomplete
agent: claude
version: 0.6.0 (cc87293)
-->

# Swe Swe Pi Slash Command Autocomplete

_2026-05-20 · 01 · claude · agent-chat 0.6.0 (cc87293)_

**USER**

> when using `pi` agent on swe-swe, why do i only get `~/.claude/commands/swe-swe/` but not `.claude/commands/swe-swe`. whereas i think i get both when using `claude` agent on swe-swe.
>
> how does swe-swe autocomplete for `/` work -- and is there a bug?
>
> research and discuss
>
> <div style="display:flex;flex-wrap:wrap;gap:8px;">
> <a href="./assets/2026-05-20-01-1.png" style="flex:0 1 calc(33% - 8px);max-width:calc(33% - 8px);"><img src="./assets/2026-05-20-01-1.png" alt="Screenshot 2026-05-20 at 9.43.28 AM.png" style="width:100%;height:auto;display:block;border-radius:6px;"></a>
> </div>

<small>took 13.5s</small><br>
**AGENT**

> I’ll inspect the swe-swe slash command/autocomplete implementation and compare how it handles pi vs claude command directories.

<small>took 2m 49s</small><br>
**AGENT**

> I researched this against the live session and code.
>
> Short answer: yes, there is a swe-swe autocomplete gap/bug, but it is specifically a Pi/project-command mismatch, not Claude behavior.
>
> What is happening:
>
> - The Agent Chat autocomplete is not Pi’s native `/` menu. Pi is running an MCP bridge that starts `@choonkeat/agent-chat` with:
>   - `--autocomplete-triggers /=slash-command`
>   - `--autocomplete-url http://localhost:$SWE_SERVER_PORT/api/autocomplete/$SESSION_UUID?...`
> - When you type `/...` in Agent Chat, agent-chat POSTs `{type:"slash-command", query:"..."}` to swe-swe-server.
> - swe-swe-server picks command directories based on the session assistant.
>
> For Pi, current swe-swe code scans:
>
> - system/global: `/home/app/.pi/agent/prompts`
> - project: `<workDir>/.pi/prompts`
>
> For Claude, it scans:
>
> - system/global: `/home/app/.claude/commands`
> - project: `<workDir>/.claude/commands`
>
> So in your Pi session, project `.claude/commands/swe-swe/reboot.md` is ignored. That explains the screenshot: `reboot.md` exists under `.claude/commands/swe-swe/`, but Pi autocomplete is looking under `.pi/prompts`, and this worktree’s `.pi` is effectively empty.
>
> I verified live:
>
> - `POST /api/autocomplete/<pi-session>` with query `reboo` returns only `swe-swe:debug-preview-page`.
> - The Pi global prompt dir has bundled swe-swe commands, but not the project/worktree `reboot.md`/`merge-worktree.md` files.
> - The project has `.claude/commands/swe-swe/reboot.md`, but no equivalent `.pi/prompts/...` file.
>
> Why Claude seems to get both:
>
> - Claude’s autocomplete path includes both `~/.claude/commands` and project `.claude/commands`.
> - Therefore Claude sees the global bundled/user commands plus the project/worktree commands.
> - Pi does the analogous thing for `.pi`, not `.claude`; since project `.pi/prompts` is missing, Pi only sees global Pi prompts.
>
> Pi’s own docs agree with this directory choice:
>
> - global prompt templates: `~/.pi/agent/prompts/*.md`
> - project prompt templates: `.pi/prompts/*.md`
>
> So Pi not reading `.claude/commands` is expected. The bug is that swe-swe’s project/worktree slash commands are present only in `.claude/commands`, not mirrored into `.pi/prompts` for Pi sessions, even though swe-swe advertises Pi as a slash-command-capable assistant.
>
> There is also a smaller autocomplete UX issue: `reboo` matched `swe-swe:debug-preview-page` only because the hint text fuzzy-matches “browser tools”. That is technically by design in `filterAutocomplete`, but it is misleading when no command-name match exists. It makes the missing `reboot` look extra strange.
>
> Likely fix options:
>
> 1. Best fix: when swe-swe creates project/worktree slash commands, also create Pi equivalents under `.pi/prompts/`.
> 2. Compatibility fix: for assistant `pi`, have autocomplete also scan `.claude/commands` as a fallback/source, probably after `.pi/prompts` so real Pi prompts win.
> 3. UX fix: make hint-only fuzzy matches weaker/optional, or require substring matches in hints, so `/reboo` doesn’t suggest `debug-preview-page` unless no better command exists and the match is meaningful.
>
> My recommendation: implement option 1 plus possibly option 3. Option 2 is convenient but conflates Claude and Pi conventions; Pi’s documented project command location is `.pi/prompts`, not `.claude/commands`.

[Quick replies]
- Fix it
- Show me the relevant code
- Just discuss

**USER**

> swe-swe `/` autocomplete is its own standalone service looking at file directories. how it is affected by choice of agent?

<small>took 14.8s</small><br>
**AGENT**

> It is standalone in the sense that agent-chat is just calling swe-swe-server’s autocomplete HTTP API. But that API is explicitly session-aware, and the session records which assistant was selected.
>
> The flow is:
>
> 1. Agent Chat sends request to:
>    `POST /api/autocomplete/{sessionUUID}?key=...`
>
> 2. `handleAutocompleteAPI` does:
>    - parse `{sessionUUID}` from URL
>    - look up `sess := sessions[sessionUUID]`
>    - read `sess.Assistant`
>    - choose directories from that assistant
>
> Relevant code path:
>
> ```go
> sess, ok := sessions[sessionUUID]
> ...
> systemDir, ext := slashCommandDirForAgent(sess.Assistant, sess.AssistantConfig.SlashCmdFormat)
> projectDir := projectCommandDir(sess.Assistant, sess.WorkDir)
> ```
>
> And the agent-specific directory mapping is:
>
> ```go
> // system/global
> claude   -> /home/app/.claude/commands
> codex    -> /home/app/.codex/prompts
> opencode -> /home/app/.config/opencode/command
> gemini   -> /home/app/.gemini/commands
> pi       -> /home/app/.pi/agent/prompts
>
> // project
> claude   -> <workDir>/.claude/commands
> codex    -> <workDir>/.codex/prompts
> opencode -> <workDir>/.opencode/command
> gemini   -> <workDir>/.gemini/commands
> pi       -> <workDir>/.pi/prompts
> ```
>
> So the choice of agent affects autocomplete because the API is not “scan every known command dir”; it is “scan the command dirs for this session’s assistant”.
>
> That means in a Pi session, even if `.claude/commands/swe-swe/reboot.md` exists, the autocomplete endpoint will not read it. It reads `.pi/prompts` for project commands and `~/.pi/agent/prompts` for global commands.
>
> The URL is standalone, but `{sessionUUID}` makes it contextual.

[Quick replies]
- Got it
- So fix by scanning .claude too?
- Implement the fix

**USER**

> is it a better idea then to have swe-swe distributed slash commands in `~/.swe-swe/commands` by swe-swe init, and swe-swe sessions just idempotent symlink according to their agent? discuss the change needed for swe-swe init and swe-swe up

<small>took 52.2s</small><br>
**AGENT**

> Yes — conceptually, a canonical swe-swe-owned command store is cleaner.
>
> I’d frame it like this:
>
> - `~/.swe-swe/commands/...` = swe-swe’s canonical distribution area.
> - Agent-native dirs are just projections/adapters:
>   - Claude: `~/.claude/commands/swe-swe -> ~/.swe-swe/commands/md/swe-swe`
>   - Codex: `~/.codex/prompts/swe-swe -> ~/.swe-swe/commands/md/swe-swe`
>   - OpenCode: `~/.config/opencode/command/swe-swe -> ~/.swe-swe/commands/md/swe-swe`
>   - Pi: `~/.pi/agent/prompts/swe-swe -> ~/.swe-swe/commands/md/swe-swe`
>   - Gemini: `~/.gemini/commands/swe-swe -> ~/.swe-swe/commands/toml/swe-swe`
>
> That avoids today’s divergence where `.claude/commands/swe-swe` can contain project/worktree-specific commands that Pi autocomplete never sees.
>
> Important distinction: this helps most for global/container-home commands. For project/worktree commands, you’d probably want the same pattern in the workspace too:
>
> - project canonical: `.swe-swe/commands/md/swe-swe/...`
> - project projections:
>   - `.claude/commands/swe-swe -> .swe-swe/commands/md/swe-swe`
>   - `.pi/prompts/swe-swe -> .swe-swe/commands/md/swe-swe`
>
> Otherwise Pi still won’t see project commands unless autocomplete gets special-cased.
>
> Changes needed in `swe-swe init`:
>
> 1. Stop writing bundled commands as the primary source into every agent dir.
> 2. Write bundled commands once into the metadata home mount, e.g.:
>    - `./home/.swe-swe/commands/md/swe-swe/*.md`
>    - `./home/.swe-swe/commands/toml/swe-swe/*.toml`
> 3. Create idempotent symlinks from agent dirs to that canonical location.
> 4. For project/worktree-local swe-swe commands, similarly write once under the project/worktree `.swe-swe/commands/...` and symlink `.claude` / `.pi` / etc. projections.
> 5. Be conservative when a target already exists:
>    - if it is the expected symlink: leave/update it
>    - if missing: create it
>    - if it is a real user-owned dir/file: do not clobber; maybe warn
>    - if it is an old swe-swe-managed dir: migrate or replace only if safe
>
> Changes needed in `swe-swe up` / runtime entrypoint:
>
> 1. On every container start, run a small idempotent “ensure command projections” step.
> 2. That step should repair missing symlinks, because users may delete agent config dirs or switch enabled agents after init.
> 3. For `--with-slash-commands`, instead of copying each repo directly into every agent dir, copy/pull once into something like:
>    - `/home/app/.swe-swe/commands/repos/md/<alias>`
>    then symlink that alias into each selected agent’s command dir.
> 4. `swe-swe up` itself can also do a host-side repair before docker compose, but the entrypoint is the more important place because it sees the mounted `/home/app` exactly as the agents see it.
> 5. If the generated entrypoint is old, plain `swe-swe up` won’t get the new logic until config regeneration/rebuild. So this change should hook into the existing version-upgrade path: `swe-swe init --previous-init-flags=reuse` then `swe-swe up --build`.
>
> Main caveats:
>
> - Do not symlink a mixed `.md`/`.toml` directory into Gemini. Keep format-specific canonical dirs.
> - Pi’s native prompt-template discovery is documented as `.pi/prompts/*.md`; swe-swe autocomplete supports one namespace level, but native Pi may not fully mirror Claude’s namespace semantics. So this fixes swe-swe Agent Chat autocomplete; native Pi `/` behavior may need separate validation.
> - Symlinks are great inside Linux containers, but migration must be careful on host-mounted dirs and existing real directories.
>
> So yes: central store + idempotent per-agent projections is a better architecture than duplicating distributed commands into each agent’s home directory. I’d do it for both container-home commands and project/worktree commands; doing only `~/.swe-swe/commands` would not fully solve the `.claude` vs `.pi` project-command mismatch you’re seeing.

[Quick replies]
- Implement it
- Discuss migration more
- Show code locations

**USER**

> Are we able to implement this TDD style with lots of report progress along the way? Discuss

<small>took 16.0s</small><br>
**AGENT**

> Yes. This is a good TDD-sized change, but I’d split it into phases so each test failure explains one design decision.
>
> Suggested TDD plan:
>
> 1. Lock down current autocomplete bug
>    - Add/adjust tests showing Pi autocomplete scans `.pi/prompts`, not `.claude/commands`.
>    - Add a failing test for “project swe-swe command should be visible to Pi when distributed through canonical swe-swe commands”.
>
> 2. Add canonical command-layout helpers
>    - Unit-test pure path/helper functions first:
>      - canonical home md dir: `.swe-swe/commands/md/swe-swe`
>      - canonical home toml dir: `.swe-swe/commands/toml/swe-swe`
>      - agent projection dirs for Claude/Pi/Gemini/etc.
>    - Add tests for idempotent symlink behavior:
>      - creates missing link
>      - leaves correct link alone
>      - refuses to clobber real dir/file
>      - optionally replaces old swe-swe-managed dir only if we can prove ownership
>
> 3. Change `swe-swe init`
>    - First golden tests should fail: expected files move from duplicated agent dirs to canonical `.swe-swe/commands/...` plus symlinks.
>    - Implement extraction into canonical dirs.
>    - Implement projections into each agent dir.
>    - Update goldens only after reviewing diff carefully.
>
> 4. Change entrypoint / `swe-swe up` runtime repair
>    - Add golden tests for generated `entrypoint.sh` showing it runs an idempotent command-projection setup.
>    - For `--with-slash-commands`, add/adjust tests so custom repos are copied/pulled once into `.swe-swe/commands/repos/...` and linked into selected agents.
>    - Verify Docker/no-Docker ownership handling remains right.
>
> 5. Change project/worktree command generation if applicable
>    - This is probably where your screenshot bug actually lives.
>    - Add a test where a worktree command like `reboot.md` is created in the canonical project/worktree store and projected to both `.claude/commands/swe-swe` and `.pi/prompts/swe-swe`.
>    - Then Pi autocomplete should find it without reading `.claude`.
>
> 6. Regression tests for autocomplete output
>    - Pi session + workDir containing canonical/project projections returns `swe-swe:reboot` for `reboo`.
>    - Claude still returns the same commands.
>    - Duplicate hints remain sensible if both global and project commands exist.
>
> Progress reporting style I’d use:
>
> - Before each phase: “Starting phase N: …”
> - After reading/changing tests: “Added failing test X; it fails because …”
> - After implementation: “Made minimal code change; rerunning targeted tests …”
> - Before golden updates: show exactly which golden families changed.
> - After full test pass: summarize behavior and migration caveats.
>
> One recommendation: don’t implement the whole central-store migration in a single jump. Start with the narrow failing Pi project-command case, then generalize. The risky part is not writing symlinks; it’s preserving user-owned command dirs and not breaking existing Claude/Codex/Gemini expectations.
>
> So yes, we can do it TDD style with frequent progress updates. I’d want explicit go-ahead before editing because this touches init, generated entrypoint, goldens, and possibly worktree command generation.

[Quick replies]
- Proceed TDD
- Narrow fix only
- Keep discussing

**USER**

> > Lock down current autocomplete bug
> > TDD
>
> so what are we using specifically?

<small>took 28.6s</small><br>
**AGENT**

> Specifically, I’d use the existing Go test suite around `cmd/swe-swe/templates/host/swe-swe-server/autocomplete_test.go` as the first TDD anchor.
>
> For “lock down current autocomplete bug”, the concrete test should be an HTTP-level test of `handleAutocompleteAPI`, not just a helper test.
>
> The test shape:
>
> ```go
> func TestHandleAutocompleteAPI_PiUsesPiProjectPromptDir(t *testing.T) {
>     tmp := t.TempDir()
>
>     // Simulate project/worktree command only in Claude dir.
>     writeFile(t,
>         filepath.Join(tmp, "project", ".claude", "commands", "swe-swe", "reboot.md"),
>         "---\ndescription: Reboot session\n---\n")
>
>     // Simulate Pi projection dir either absent or empty.
>     mkdirAll(t, filepath.Join(tmp, "project", ".pi", "prompts"))
>
>     sessions["test-uuid"] = &Session{
>         UUID: "test-uuid",
>         Assistant: "pi",
>         WorkDir: filepath.Join(tmp, "project"),
>         AssistantConfig: AssistantConfig{SlashCmdFormat: SlashCmdMD},
>     }
>
>     POST /api/autocomplete/test-uuid query "reboo"
>
>     // Current behavior: swe-swe:reboot is missing.
>     // Desired behavior after canonical/projection fix: swe-swe:reboot present.
> }
> ```
>
> But because `slashCommandDirForAgent("pi")` currently hardcodes `/home/app/.pi/agent/prompts`, a pure HTTP test cannot easily fake the global system dir. For the bug we care about, that’s okay: the project dir is derived from `sess.WorkDir`, so we can test project behavior deterministically.
>
> Then add the desired passing case:
>
> ```go
> func TestHandleAutocompleteAPI_PiFindsProjectedSweSweProjectCommands(t *testing.T) {
>     tmp := t.TempDir()
>     project := filepath.Join(tmp, "project")
>
>     // This is the target projection we want init/up/worktree setup to create.
>     writeFile(t,
>         filepath.Join(project, ".pi", "prompts", "swe-swe", "reboot.md"),
>         "---\ndescription: Reboot session\n---\n")
>
>     sessions["test-uuid"] = &Session{Assistant: "pi", WorkDir: project, ...}
>
>     POST query "reboo"
>     assert result contains `swe-swe:reboot`
> }
> ```
>
> That second test probably already passes if the `.pi/prompts` file exists, which is useful: it proves the autocomplete service is not the main missing piece. The missing piece is command distribution/projection.
>
> So the actual TDD chain should be:
>
> 1. `autocomplete_test.go`
>    - prove Pi autocomplete finds `.pi/prompts/swe-swe/reboot.md`
>    - prove it does not read `.claude/commands/swe-swe/reboot.md`
>    - this documents the current agent-specific behavior.
>
> 2. New or existing init tests in `cmd/swe-swe/main_test.go`
>    - assert `swe-swe init` produces canonical command files under `home/.swe-swe/commands/md/swe-swe/`
>    - assert it creates/provisions Pi projection `home/.pi/agent/prompts/swe-swe`
>    - assert Claude projection still exists at `home/.claude/commands/swe-swe`
>    - assert Gemini gets TOML projection, not MD.
>
> 3. Golden tests
>    - run/update the existing golden init tests after the unit tests define expected behavior.
>    - These will show the filesystem layout change across variants.
>
> 4. Entrypoint template tests / goldens
>    - assert runtime repair links/copies custom slash-command repos into central `.swe-swe/commands/...`, then projects them to the selected agents.
>
> Commands I’d use along the way:
>
> ```bash
> go test ./cmd/swe-swe/templates/host/swe-swe-server -run Autocomplete
>
> go test ./cmd/swe-swe -run 'Init|Golden|Slash'
>
> make golden-update   # only after reviewing intentional expected changes
>
> go test ./cmd/swe-swe/...
> ```
>
> So: the first TDD target is not “fix autocomplete”; it’s “prove autocomplete is correctly agent-specific, and therefore distribution/projection must create Pi-visible files”. The failing implementation tests should mostly live around `swe-swe init` and entrypoint generation, not the autocomplete handler itself.

[Quick replies]
- Proceed with those tests
- Refine test plan
- Narrow to autocomplete only

**USER**

> are we going to stand up a test server to test against? if so, what test server/container and how does it work?

<small>took 17.2s</small><br>
**AGENT**

> For the first TDD pass: no, I would not stand up a real server/container.
>
> I’d use Go’s in-process `httptest` against `handleAutocompleteAPI` directly. That gives us a realistic HTTP request/response without Docker, ports, browser UI, MCP, or agent-chat.
>
> It works like this:
>
> ```go
> req := httptest.NewRequest(
>     http.MethodPost,
>     "/api/autocomplete/test-uuid?key=test-api-key",
>     strings.NewReader(`{"type":"slash-command","query":"reboo"}`),
> )
> w := httptest.NewRecorder()
> handleAutocompleteAPI(w, req)
>
> // Assert w.Code and decode w.Body JSON.
> ```
>
> Before calling the handler, the test seeds the global session map:
>
> ```go
> sessions["test-uuid"] = &Session{
>     UUID: "test-uuid",
>     Assistant: "pi",
>     WorkDir: tempProjectDir,
>     AssistantConfig: AssistantConfig{SlashCmdFormat: SlashCmdMD},
> }
> ```
>
> And creates fake command files under `t.TempDir()`:
>
> ```text
> /tmp/.../project/.pi/prompts/swe-swe/reboot.md
> /tmp/.../project/.claude/commands/swe-swe/reboot.md
> ```
>
> That tests the actual autocomplete server handler logic, including:
>
> - auth key handling
> - session UUID lookup
> - assistant-specific directory selection
> - project directory scanning
> - fuzzy filtering/sorting
> - JSON response shape
>
> What it does not test:
>
> - Docker volume mounts
> - entrypoint symlink repair
> - actual `@choonkeat/agent-chat` UI behavior
> - Pi MCP bridge startup
> - browser autocomplete rendering
>
> For the init/up migration, I’d still start with non-container tests:
>
> - `swe-swe init` golden tests produce the expected host metadata tree under test temp dirs.
> - generated `entrypoint.sh` golden tests show the runtime repair/copy/symlink logic.
>
> Only after those pass would I consider a real container smoke test.
>
> If we do stand one up, it would be an optional/manual e2e smoke test like:
>
> 1. Build or use local swe-swe binary.
> 2. Create a temp project.
> 3. Run:
>    ```bash
>    swe-swe init --project-directory /tmp/project --agents pi,claude
>    swe-swe up --project-directory /tmp/project --build -d
>    ```
> 4. Wait for `swe-swe-server` on its configured port.
> 5. Create or use a Pi session.
> 6. POST directly to:
>    ```bash
>    curl -X POST \
>      "http://localhost:<port>/api/autocomplete/<sessionUUID>?key=<MCP_AUTH_KEY>" \
>      -H 'content-type: application/json' \
>      -d '{"type":"slash-command","query":"reboo"}'
>    ```
> 7. Assert `swe-swe:reboot` appears.
>
> But that requires discovering the session UUID/auth key and managing Docker lifecycle, so it’s slower and flakier. I would not use that as the primary TDD loop.
>
> So the practical plan is:
>
> - Primary TDD: in-process `httptest` + filesystem temp dirs + init/golden tests.
> - Optional final confidence: one Docker smoke test against a real running swe-swe-server.

[Quick replies]
- Proceed in-process first
- I want real container e2e
- Keep discussing

**USER**

> Proceed in-process first

<small>took 13.3s</small><br>
**AGENT**

> Starting the in-process TDD loop: I’ll add HTTP-level autocomplete tests using `httptest` and temp command directories, then run only the autocomplete tests first.

<small>took 35.5s</small><br>
**AGENT**

> Added the first in-process autocomplete tests. Targeted `go test` against the template server package doesn’t compile directly from source because that directory is an embedded/generated template package with missing generated paths/import layout. I’m checking the project’s intended test entrypoint now.

<small>took 24.4s</small><br>
**AGENT**

> The intended test runner is `make test-server`, which copies the template server into `/tmp`, creates its Go module, then runs Go tests there. First run reached the new tests; failures are just my test setup not creating parent dirs before writing files, so I’m fixing that now.

<small>took 30.7s</small><br>
**AGENT**

> In-process autocomplete tests now pass via the real project test harness (`make test-server`). They document: Pi reads project `.pi/prompts`, Pi does not read project `.claude/commands`, and the helper mappings include Pi.

<small>took 35.6s</small><br>
**AGENT**

> Next in-process TDD phase: I’m adding focused init-level tests for a canonical `home/.swe-swe/commands/{md,toml}` store plus per-agent symlink projections, before wiring it into full init/goldens.

<small>took 28.0s</small><br>
**AGENT**

> Added the focused init-level test. It fails as expected because `installBundledSlashCommands` does not exist yet. Now I’ll implement the minimal helper: write canonical MD/TOML stores and create relative symlink projections.

<small>took 45.9s</small><br>
**AGENT**

> Minimal canonical-store helper implemented and the focused init test passes. It writes bundled commands under `.swe-swe/commands/{md,toml}` and creates relative symlinks for Claude/Codex/OpenCode/Pi/Gemini.

<small>took 28.2s</small><br>
**AGENT**

> Ran `go test ./cmd/swe-swe`; it passes. Existing golden tests didn’t catch the new symlink layout yet, so I’ll inspect the generated/expected tree next and decide whether to add explicit golden assertions or update goldens later.

<small>took 1m 1s</small><br>
**AGENT**

> I’m extending the same pattern to runtime custom slash commands: generated entrypoint should copy/pull each repo once into `/home/app/.swe-swe/commands/md/<alias>` and symlink selected agents to that central copy.

<small>took 1m 2s</small><br>
**AGENT**

> Runtime custom slash-command TDD step is green: generated entrypoint now materializes each custom repo once in `/home/app/.swe-swe/commands/md/<alias>` and symlinks Claude/Pi/etc. only when those agents are selected.

<small>took 30.1s</small><br>
**AGENT**

> Targeted tests are still green after runtime changes: `go test ./cmd/swe-swe` and the in-process server autocomplete tests via `make test-server` both pass.

<small>took 45.5s</small><br>
**AGENT**

> Added a migration-safety test: if an old/user real command dir already exists, init must not fail or clobber it. It fails with the current helper, so I’m changing symlink creation to leave real dirs untouched.

<small>took 56.5s</small><br>
**AGENT**

> Re-ran focused suites after migration-safety change: `go test ./cmd/swe-swe` and in-process autocomplete tests via `make test-server` both pass.

<small>took 51.5s</small><br>
**AGENT**

> Implemented the first in-process TDD pass.
>
> What I added:
>
> - HTTP-level autocomplete tests using `httptest` in `cmd/swe-swe/templates/host/swe-swe-server/autocomplete_test.go`:
>   - Pi project autocomplete reads `<workDir>/.pi/prompts/...`
>   - Pi project autocomplete does not read `<workDir>/.claude/commands/...`
>   - Pi path mappings are now explicitly covered in tests
>
> - Init-level canonical store tests in `cmd/swe-swe/main_test.go`:
>   - bundled commands are written once to `home/.swe-swe/commands/md/swe-swe`
>   - Gemini TOML commands go to `home/.swe-swe/commands/toml/swe-swe`
>   - Claude/Codex/OpenCode/Pi/Gemini command dirs become symlink projections
>   - existing real dirs are preserved, not clobbered, for migration safety
>
> - Runtime/custom slash-command template test:
>   - `--with-slash-commands` repos now materialize once under `/home/app/.swe-swe/commands/md/<alias>`
>   - selected agent dirs symlink to that central copy
>
> Code changed:
>
> - `cmd/swe-swe/init.go`
>   - added `installBundledSlashCommands`
>   - added relative symlink projection helper
>   - init now writes bundled commands to canonical store first
>
> - `cmd/swe-swe/templates.go`
>   - generated entrypoint now copies/pulls custom slash-command repos once into `/home/app/.swe-swe/commands/md/<alias>` and links selected agents to it
>
> Validation run:
>
> - `go test ./cmd/swe-swe` ✅
> - `TEST_SERVER_ARGS='-run Autocomplete' make test-server` ✅
> - `make test` ✅
>
> Important caveat: this first pass fixes global/container-home bundled/custom command distribution. It does not yet migrate arbitrary project/worktree `.claude/commands` into `.pi/prompts`. So the exact screenshot case where `reboot.md` exists only in project `.claude/commands/swe-swe/` still needs the next phase: project/worktree canonical command projections.

[Quick replies]
- Proceed to project/worktree phase
- Show diff
- Stop here

**USER**

> Proceed to project/worktree phase

<small>took 49.5s</small><br>
**AGENT**

> Starting project/worktree phase. I’m going to add a failing in-process autocomplete test for the exact legacy shape: command exists only under project `.claude/commands`, Pi session should self-heal/project it into `.pi/prompts` and return it.

<small>took 39.5s</small><br>
**AGENT**

> The project/worktree regression test fails as expected: with only `.claude/commands/swe-swe/<cmd>.md`, Pi autocomplete returns no results. Now I’ll implement the self-healing projection before discovery.

<small>took 36.8s</small><br>
**AGENT**

> Project/worktree projection is implemented and the new regression test passes. The server now creates conservative symlinks: `.swe-swe/commands/md -> .claude/commands` and `.pi/prompts -> .swe-swe/commands/md` when legacy project `.claude/commands` exists and targets are missing.

<small>took 28.4s</small><br>
**AGENT**

> Project/worktree phase targeted tests are green: autocomplete suite via `make test-server` and `go test ./cmd/swe-swe` both pass after the self-healing projection change.

<small>took 39.0s</small><br>
**AGENT**

> Full `make test` passes after the project/worktree phase too.

<small>took 21.6s</small><br>
**AGENT**

> Project/worktree phase is implemented and tested.
>
> What changed:
>
> - `cmd/swe-swe/templates/host/swe-swe-server/autocomplete.go`
>   - Before autocomplete scans dirs, it now runs a conservative self-heal for project/worktree command projections.
>   - If a workdir has legacy/project Claude commands at:
>     - `.claude/commands`
>   - and the Pi projection is missing, it creates:
>     - `.swe-swe/commands/md -> ../../.claude/commands`
>     - `.pi/prompts -> ../.swe-swe/commands/md`
>   - Existing files/dirs are not clobbered.
>
> - `cmd/swe-swe/templates/host/swe-swe-server/autocomplete_test.go`
>   - Added regression test for the exact shape we discussed: Pi session, command only under project `.claude/commands/swe-swe/*.md`, autocomplete query finds it after projection.
>
> Earlier phase remains included:
>
> - bundled commands now install into canonical `home/.swe-swe/commands/{md,toml}/swe-swe`
> - agent dirs symlink to that canonical store
> - custom `--with-slash-commands` repos materialize once under `/home/app/.swe-swe/commands/md/<alias>` and selected agents symlink to it
>
> Validation:
>
> - `TEST_SERVER_ARGS='-run Autocomplete' make test-server` ✅
> - `go test ./cmd/swe-swe` ✅
> - `make test` ✅
>
> Effect on the screenshot case: with the updated server, a Pi session whose workdir has `.claude/commands/swe-swe/reboot.md` but no `.pi/prompts` should self-heal on autocomplete and return `swe-swe:reboot` for `/reboo`.

[Quick replies]
- Show diff
- Run a live smoke test
- Looks good

**USER**

> export chat and git commit along with changes in this conversation

<small>took 5.8s</small><br>
**AGENT**

> I’ll export the chat transcript, review the working tree, then commit the code changes plus the exported chat archive.

