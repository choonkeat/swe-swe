# swe-npx: drop the node/npx dependency for our Go-binary npm tools

Executable task plan for `/swe-swe:execute-step-by-step` (via
`/swe-swe:execute-in-worktree tasks/2026-07-18-swe-npx-node-free-helpers.md`).
Log convention: `tasks/2026-07-18-swe-npx-node-free-helpers.md-phase{N}.log`.

Origin: dockerless dependency-audit chat 2026-07-18. All four `@choonkeat/*`
npm tools swe-swe spawns are ALREADY static Go binaries published via the
distribute-go-bin pattern (a generated ~50-line node shim in the main package
+ per-platform `optionalDependencies` like `@choonkeat/md-serve-linux-x64`
whose tarball contains the real binary at `package/bin/<name>`). node's only
job today is running that shim. Replacing `npx -y <pkg>` with a small
registry-resolving exec helper removes node from 5 of 6 tabs in dockerless
mode, kills the npx cwd-collision bug class
(`tasks/2026-07-11-mcp-npx-cwd-collision.md`), and removes npx cold-start
latency from MCP spawn.

## Status

**DONE 2026-07-18.** All five phases complete; see per-phase logs.

- [x] Phase 1 -- `swe-npx` helper binary (TDD against a fake registry)
      (done 2026-07-18: 13 tests green, static build verified)
- [x] Phase 2 -- payload + Dockerfile wiring (8 -> 9 embedded binaries)
      (done 2026-07-18: embed test green, goldens updated)
- [x] Phase 3 -- swap call sites (goldens WILL change)
      (done 2026-07-18: all @choonkeat spawns on swe-npx, playwright untouched)
- [x] Phase 4 -- e2e verification (node-free dockerless, docker mode, collision repro)
      (done 2026-07-18: poisoned dockerless e2e PASS; docker-mode container
      verified via browser, all spawns exec from npx-cache; collision repro
      fixed live; warm spawn 11ms vs npx 1.15s)
- [x] Phase 5 -- docs + changelog
      (done 2026-07-18: dockerless.md deps, single-binary follow-up note,
      CHANGELOG entry; tasks/2026-07-11-mcp-npx-cwd-collision.md does not
      exist in this repo, so no superseded-note was needed)

## Ground rules for the executing agent

- ASCII only in all code/markdown (no em-dashes, no smart quotes).
- Run tests with `make test`, never bare `go test` / `go vet`.
- After ANY change under `cmd/swe-swe/templates/` or files feeding golden
  output: `make build golden-update`, then
  `git add cmd/swe-swe/testdata/golden` and review
  `git diff --cached -- cmd/swe-swe/testdata/golden` before committing.
- Stage explicit paths by name. NEVER `git add -A`.
- Never diagnose `git-credential-swe-swe` by piping its output to stdout.
- If any verification fails and a workaround is tempting: STOP and ask via
  send_message. No silent compromises.

## Design (settled -- do not relitigate)

### What swe-npx is

A stdlib-only Go helper (sibling of `mcp-lazy-init` / `swe-run`) that
resolves a distribute-go-bin-style npm package to its platform binary,
caches it, and **execs** it (unix `syscall.Exec` -- no wrapper process left
behind, stdio/signals pass straight through, which is exactly what stdio MCP
wants).

CLI contract (drop-in for our own npx call sites):

```
swe-npx [-y] <@scope/name>[@<version>|@latest] [args...]
```

`-y` is accepted and ignored (compatibility with the strings we replace).
Everything after the package token is passed verbatim to the binary.

### Resolution algorithm

1. Map to the platform package: `<@scope/name>-<os>-<arch>` with
   `linux -> linux`, `darwin -> darwin`; `amd64 -> x64`, `arm64 -> arm64`.
   (win32 out of scope, same as the rest of dockerless.)
2. Version pick:
   - explicit `@1.2.3` -> use it;
   - none or `@latest` -> `GET <registry>/<url-escaped platform pkg>`
     (default registry `https://registry.npmjs.org`, override
     `SWE_NPX_REGISTRY`; HTTP timeout ~5s), read `dist-tags.latest`.
   - To keep MCP spawn fast, memoize the `latest` answer per package in the
     cache dir and only re-check the registry after a TTL
     (`SWE_NPX_LATEST_TTL`, default 15m). Within TTL: zero network.
   - Registry unreachable/timeout -> fall back to the newest cached version
     with a stderr note. No cache AND no network -> clear fatal error.
3. Cache hit (`<cache>/<platform-pkg>@<version>/<name>` exists) -> exec.
4. Miss -> download `dist.tarball` from the same version doc, verify
   `dist.integrity` (sha512; fall back to `dist.shasum` sha1 if integrity is
   absent), extract the tar.gz `package/` tree into a unique temp dir under
   the cache root, chmod 0755 the binary at `package/bin/<name>`, then
   ATOMIC `os.Rename` into place. Losing a concurrent race is fine: if the
   rename target already exists, discard ours and use the winner. No
   lockfiles needed.
5. `exec` the cached binary with the remaining args and inherited env.

Cache dir: `$SWE_NPX_CACHE_DIR`, default `<os.UserHomeDir()>/.swe-swe/npx-cache`.
This is a USER-level cache (like `~/.swe-swe/commands`), shared across
sessions and projects -- that is the point: one download serves everything.

### What swe-npx is NOT

- NOT a general npx replacement. It only handles dependency-free
  distribute-go-bin platform packages. A 404 on the derived platform package
  -> fatal error telling the operator to use real npx. No npm semver-range
  resolution, no dependency trees, no install scripts -- ever.
- `@playwright/mcp` is real JS (depends on playwright-core): it STAYS on
  npx. It belongs to the Agent View heavy tier, which already needs system
  packages; node remains a documented dependency of that tier only.
- The `claude` CLI is itself a node program. We are not freeing claude users
  from node; we are decoupling OUR stack from node so non-node agents
  (codex/goose/aider/...) get a genuinely node-free box (minus Agent View).

### Why registry, not the alternative of embedding

The four tools are ours, so embedding them in the dockerless payload (like
the other 8 binaries) was considered and REJECTED: it couples swe-swe
releases to agent-chat/md-serve/whiteboard/reverse-proxy release cadence and
bloats the CLI. Registry resolution keeps them independently updatable,
which is exactly the property `npx -y` gives us today.

### Interaction with tasks/2026-07-11-mcp-npx-cwd-collision.md

swe-npx SUPERSEDES that fix for the `@choonkeat/*` packages: resolution goes
straight to the registry keyed by package name and never consults the
project cwd, so a session whose cwd is the agent-chat repo can no longer
shadow the launch. The `@playwright/mcp@latest` pin from that task stays,
since playwright remains on npx. If that task has not landed when this one
executes, note in its file that Phases covering `@choonkeat/*` strings are
obsoleted by this task.

### Call sites to swap (found by grep 2026-07-18; re-grep before editing)

| Spot | What |
|---|---|
| `templates/host/swe-swe-server/main.go` ~4795 | Files tab: `exec.Command("npx", "-y", "@choonkeat/md-serve@latest", ...)` |
| `templates/host/swe-swe-server/mcp_less.go` ~65-85 | mcp-less proxy fleet Argv: agent-chat, reverse-proxy x2, whiteboard |
| `cmd/swe-swe/dockerless.go` ~332-336 | dockerless project `.mcp.json`: same four |
| `templates/host/entrypoint.sh` | per-agent MCP config blocks (claude JSON, codex TOML, goose YAML, ...): same four, several emissions each |
| `templates/host/mcp-bridge.ts` ~465, ~487 | pi bridge SpawnedHttpService: agent-chat, whiteboard |

Every `npx -y @choonkeat/<x>` becomes `swe-npx -y @choonkeat/<x>`
(keep the `sh -c "exec ..."` wrappers and all flags/env exactly as-is).
All `@playwright/mcp` occurrences stay on `npx` -- verify the final diff
contains NO playwright changes.

Note on the md-serve spawn (`main.go` ~4795-4840): today the captured PID is
the npx wrapper and md-serve runs as its child, hence the process-group
SIGKILL dance. Because swe-npx execs, the captured PID BECOMES md-serve.
Keep the process-group logic anyway (harmless, and protects against future
children), but update the comment that explains why it exists.

## Phase 1 -- swe-npx helper binary (TDD)

Create `cmd/swe-swe/templates/host/swe-npx/` with `main.go`, `go.mod.txt`
(mirror `mcp-lazy-init/` layout exactly -- `.txt` suffix so init does not
treat templates as a module), and `main_test.go`.

TDD RED first: tests run against an `httptest.Server` fake registry serving
canned package docs + tarballs built in-test (`archive/tar` + `compress/gzip`).
Cover at minimum:

1. platform-package name derivation (`@choonkeat/md-serve` + linux/amd64 ->
   `@choonkeat/md-serve-linux-x64`; url-escaping of the scoped name).
2. explicit version -> no dist-tags lookup, direct download.
3. `latest` -> dist-tags consulted, memo file written; second call within
   TTL does NOT hit the registry (assert via request counter).
4. integrity: good sha512 passes; corrupted tarball -> fatal, nothing cached.
5. cache hit -> zero HTTP requests.
6. registry down + cache populated -> newest cached version used, stderr note.
7. registry down + empty cache -> non-zero exit, actionable error.
8. 404 platform package -> error mentioning real npx.
9. concurrent-rename race: pre-create the final cache dir, run the download
   path, assert it discards its temp dir and uses the existing one.
10. arg passthrough + `-y` swallowed (test the arg-parsing function; the
    actual `syscall.Exec` is behind a var so tests can stub it).

Wire tests into `make test`: add a Makefile target that (like
`_payload-helper`) copies `main.go`+`main_test.go`+`go.mod` to a temp module
and runs `go test` there; hook it into the existing `test` dependency chain
next to wherever the server-template tests run.

**Verify:** `make test` green; the fake-registry tests pass; binary builds
standalone with `CGO_ENABLED=0`.

## Phase 2 -- payload + Dockerfile wiring

- Makefile `dockerless-payload`: add `$(MAKE) _payload-helper NAME=swe-npx`.
  Payload count 8 -> 9; update `TestDockerlessPayloadEmbedsBinaries`
  (RED first) to assert the ninth ELF.
- Dockerfile (`templates/host/Dockerfile`): add the build stanza next to
  mcp-lazy-init's (COPY main.go+go.mod, static build) and the
  `COPY --from=server-builder ... /usr/local/bin/swe-npx` line.
- `cmd/swe-swe` dockerless init: ensure the dumped `swe-npx` lands on PATH
  exactly like the other helper binaries (same dump dir; no extra wiring
  expected -- confirm by reading the Phase 2 dump code in `dockerless.go`).

**Verify:** `make build golden-update` (Dockerfile golden changes only);
`make test` green including the 9-binary embed test;
`make dockerless-payload` prints the new binary.

## Phase 3 -- swap call sites (goldens WILL change)

Edit the five spots in the table above. Then `make build golden-update`.

**Verify:**
- `git diff --cached -- cmd/swe-swe/testdata/golden` shows ONLY
  `npx -y @choonkeat/...` -> `swe-npx -y @choonkeat/...` rewrites (plus the
  Phase 2 Dockerfile lines if committed together);
- zero `@playwright` lines in the diff;
- `grep -rn "npx -y @choonkeat" cmd/swe-swe/templates cmd/swe-swe/*.go`
  returns nothing;
- `make test` green.

## Phase 4 -- e2e verification

1. **Node-free dockerless proof (the headline claim):** run the dockerless
   smoke flow (`make test-e2e-dockerless-smoke` or scripts/e2e-dockerless.sh)
   with node/npx masked out of PATH for the SERVER process (e.g. a temp dir
   of `node`/`npx` shims that `exit 127`, prepended to PATH). Assert:
   Files tab backend comes up (md-serve fetched by swe-npx) and the
   agent-chat MCP proxy port answers. Playwright/Agent View is expected to
   degrade -- that is correct behavior, not a failure.
2. **Docker mode:** boot the test container per
   `docs/dev/test-container-workflow.md`, open a session via the MCP browser
   at host.docker.internal:9770, confirm Agent Chat + Files + whiteboard
   work; `swe-npx` cache dir appears under the container user's
   `~/.swe-swe/npx-cache`. Tear the container down after.
3. **Collision repro now fixed:** in the test container, create a session
   whose project cwd is a checkout of the agent-chat repo (the repro from
   tasks/2026-07-11); confirm the swe-swe-agent-chat MCP loads.
4. **Latency note:** record cold vs warm spawn time of
   `swe-npx -y @choonkeat/agent-chat --help` in the phase log (expect warm
   spawn to be near-instant vs npx's ~1-3s).

## Phase 5 -- docs + changelog

- `docs/dockerless.md`: Dependencies section -- node/npx moves from
  "required" to "only for Agent View (@playwright/mcp) and node-based agent
  CLIs"; mention the user-level `~/.swe-swe/npx-cache`.
- `tasks/2026-06-27-dockerless-single-binary.md`: add a follow-up line
  pointing here.
- `tasks/2026-07-11-mcp-npx-cwd-collision.md`: mark the `@choonkeat/*`
  portions superseded (see Design).
- CHANGELOG entry.

**Verify:** `make test` green; docs render sanely; commit.

## End state

A box with git + bash + a non-node agent CLI runs Agent Terminal, Terminal,
Preview, Files, and Agent Chat with NO node installed. node remains only for
the Agent View tier (@playwright/mcp) and node-based agent CLIs. MCP servers
spawn from a warm user-level binary cache with no cwd-dependent resolution,
and the npx cwd-collision class of failure is structurally gone.
