# Files tab: make md-serve follow the swe-swe light/dark theme

Executable task plan for `/swe-swe:execute-step-by-step` (via
`/swe-swe:execute-in-worktree tasks/2026-07-12-md-serve-theme-cookie.md`).
Log convention: `tasks/2026-07-12-md-serve-theme-cookie.md-phase{N}.log`.

Origin: chat session 2026-07-12 (md-serve breadcrumb / theme-cookie work). The
md-serve side is already shipped: `@choonkeat/md-serve@0.6.0` adds a
`-theme-cookie NAME` flag that, when the request carries cookie `NAME` with
value `light` or `dark`, forces that theme server-side (swaps the bundled
`github-markdown-light.css` / `-dark.css`, pins chroma + chrome + `color-scheme`)
instead of following the browser's `prefers-color-scheme`. Unset = unchanged
auto behavior. This task wires swe-swe to pass that flag so the Files tab
matches the rest of the swe-swe UI.

## Ground rules for the executing agent

- ASCII only in all code/markdown (no em-dashes, no smart quotes).
- Run tests with `make test`, never bare `go test`.
- After ANY change under `cmd/swe-swe/templates/`: `make build golden-update`,
  then `git add -A cmd/swe-swe/testdata/golden` and review
  `git diff --cached -- cmd/swe-swe/testdata/golden` before committing. The
  file changed here (`swe-swe-server/main.go`) is a template embedded into
  every generated project, so the golden fixtures WILL move.
- If any verification fails and a workaround is tempting: STOP and ask via
  send_message. No silent compromises.

## Design (settled -- do not relitigate)

### Problem
Each session spawns its own md-serve for the Files tab, launched by
`startSessionMdServe` (`cmd/swe-swe/templates/host/swe-swe-server/main.go`,
~4655) via `npx -y @choonkeat/md-serve@latest -dir <workDir> -addr :<FilesPort>`.
md-serve themes purely off the browser's `prefers-color-scheme`, so the Files
iframe ignores swe-swe's own light/dark toggle (which is stored in
`localStorage['swe-swe-theme-mode']` and mirrored to cookie `swe-swe-theme` by
`static/theme-mode.js`, and consumed by swe-swe's own pages + the agent-chat UI
via `--theme-cookie swe-swe-theme`). Result: user picks dark in swe-swe, opens
Files, and md-serve renders light (or vice-versa) whenever it disagrees with the
OS setting.

### Fix
Pass `-theme-cookie swe-swe-theme` to the per-session md-serve, exactly as
agent-chat is already launched (`--theme-cookie swe-swe-theme` in mcp-bridge.ts
/ dockerless.go / templates.go). One-line change in `startSessionMdServe`:

```go
cmd := exec.Command("npx", "-y", "@choonkeat/md-serve@latest",
    "-dir", sess.WorkDir,
    "-addr", fmt.Sprintf(":%d", sess.FilesPort),
    "-theme-cookie", "swe-swe-theme",
)
```

Requires md-serve >= 0.6.0. Because the launch pins `@latest` and 0.6.0 is
published, this is satisfied at runtime; no image rebuild needed. (An older
md-serve would reject the unknown flag and fail to start -- non-critical per the
function's contract, but it won't happen with `@latest`.)

### Why the cookie reaches md-serve (and the one place it doesn't)
The Files tab is fronted by an auth-checked reverse proxy
(`httputil.NewSingleHostReverseProxy` -> `localhost:FilesPort`, ~5383). That
proxy forwards the inbound `Cookie` header to md-serve unchanged, so md-serve
sees whatever `swe-swe-theme` the browser sends to the filesProxyPort origin.

- **Local mode** (main UI `localhost:{port}`, Files `localhost:{filesProxyPort}`):
  cookies are host-scoped, NOT port-scoped, so `swe-swe-theme` (set `path=/`,
  no Domain in `theme-mode.js:50`) is sent to the Files port too. Works with no
  further change.
- **Tunnel / subdomain mode** (Files at `{filesProxyPort}.{publicHostname}`):
  a host-only cookie on the parent is NOT sent to the subdomain. For the theme
  to follow here, `swe-swe-theme` must be scoped to the parent domain
  (`Domain=.{publicHostname}`), the same way the AUTH cookie must already be
  scoped to reach the Files subdomain (`requireAuthCookie` on the files proxy
  passes in tunnel mode, so that scoping precedent exists -- see
  `tasks/2026-07-08-share-live-session-scoped-cookie.md`). Confirm during
  verification; if the auth cookie is parent-domained but the theme cookie is
  not, widen the theme cookie's Domain in `theme-mode.js` to match. Do NOT
  broaden it in local mode (host-only is correct there).

## Phase 1 -- Wire the flag

Step 1.1. In `cmd/swe-swe/templates/host/swe-swe-server/main.go`,
`startSessionMdServe`, add `"-theme-cookie", "swe-swe-theme"` to the
`exec.Command` args (see Design). Update the function's doc comment to note the
Files tab follows the swe-swe theme cookie.

Step 1.2. `make build golden-update`; `git add -A cmd/swe-swe/testdata/golden`;
review `git diff --cached -- cmd/swe-swe/testdata/golden` -- the only expected
change is the added flag inside the embedded `swe-swe-server/main.go` (and any
file that snapshots the launch args). Nothing else should move.

Verification 1: `make test` green.

## Phase 2 -- Prove it end to end

Step 2.1. Bring up a session (dockerless is fine). Toggle swe-swe to **dark**,
open the Files tab, confirm md-serve renders dark (github dark canvas, not the
light `#ffffff`). Toggle to **light**, reload Files, confirm it flips. Then set
the OS/browser to the OPPOSITE of the swe-swe choice and confirm the Files tab
still follows swe-swe, not the OS -- that is the whole point of the cookie
override.

Step 2.2. Confirm the process-group teardown is unaffected: end the session and
verify no orphaned md-serve holds `FilesPort` (the existing `stopSessionMdServe`
negative-PID kill; the added flag does not change PID handling).

Step 2.3. (Tunnel mode, if reachable in the test env) Repeat 2.1 against the
`{filesProxyPort}.{publicHostname}` URL. If the theme does NOT follow, apply the
`Domain=.{publicHostname}` scoping described in Design and re-verify; if it
already follows, note that and change nothing.

Verification 2: existing Files-tab e2e still green
(`e2e/tests/dockerless-tabs.spec.js`, `terminal-ui-tabs.spec.js`). Optionally
add an assertion that a themed request to the files proxy yields the pinned
stylesheet (`github-markdown-dark.css` for `swe-swe-theme=dark`).

## Phase 3 -- Land it

Step 3.1. `git add` the specific changed paths by name (template main.go, the
golden dir, and `theme-mode.js` only if Step 2.3 required it). NEVER
`git add -A` outside the golden dir. Commit:
`feat(files): md-serve follows the swe-swe light/dark theme cookie`.

Step 3.2. Update `CHANGELOG.md` (Files-tab entry) noting the Files tab now
honors the swe-swe theme via md-serve `-theme-cookie` (needs md-serve >= 0.6.0,
pulled automatically via `@latest`).

Done when: swe-swe's theme toggle drives the Files tab in local mode, teardown
is clean, tests green, and the tunnel-mode cookie scoping is either confirmed
working or fixed.
