# Files Tab (per-session md-serve)

## Status

**Planned, not yet started.** Adds a `Files` tab alongside the existing
panes (Agent Terminal, Agent Chat, Preview, Code, Terminal, Agent View).
Each session spawns its own `md-serve` process rooted at the session's
working directory, proxied through a per-port auth-checked listener, and
shown in a new iframe tab. This mirrors the per-session Chrome/VNC work
(see `tasks/2026-03-12-per-session-chrome-vnc.md`) almost exactly, plus
the per-session preview-port exposure plumbing
(`tasks/2026-01-30-per-session-preview-ports.md`).

## Why this shape

`md-serve` (`@choonkeat/md-serve`) is a tiny single Go binary that
renders Markdown as GitHub-styled HTML, syntax-highlights source files
with linkable line numbers (`/main.go#L42`, `?raw=1` for raw bytes),
shows directory listings, and live-reloads on mtime change (1s poll).
That makes it a read-only repo browser that stays current as the agent
edits files -- a distinct value from the Code (code-server) tab, which
we are explicitly NOT touching.

The port and process plumbing is a solved problem here: every
per-session service (preview, agent-chat, public, CDP, VNC) is derived
from the session's preview port and proxied through `proxyPortOffset`.
A Files service is just the next member of that family.

### Decisions locked in discussion

- **Per-session, no sharing.** Each session's `md-serve` serves that
  session's own workDir (worktrees differ per session).
- **Port range 9000-9019 (preview + 6000), NOT 8000-8019.** 8000/8080
  are extremely common app ports (md-serve's own default is 8080), so
  the 8xxx band risks colliding with whatever the user's preview app
  binds. 9xxx is quieter. Proxy listeners land at 29000-29019.
- **Two cross-origin addressing forms, no path-based mount.** We do NOT
  mount Files at a same-origin path (`/proxy/{uuid}/files/`): md-serve
  emits root-relative links with no base-path option, so path rewriting
  would break every link. Instead we use the same pair of cross-origin
  forms preview/agent-chat already use (see "Port math is not enough"):
  local `localhost:{filesProxyPort}` and tunnel
  `{filesProxyPort}.{publicHostname}`.
- **Dotfile exposure accepted (option a).** `-dir <workDir>` serves the
  whole working dir. md-serve blocks path traversal outside root, and
  hides dotfiles from *listings*, but dotfiles (`.env`, `.swe-swe/env`,
  `.git/`) remain fetchable by direct URL. This sits behind the same
  login cookie (`requireAuthCookie`) as every other tab, so it is the
  authenticated user only. Same trust boundary; accepted.
- **Pinned port via explicit flag.** Launch as
  `md-serve -dir <workDir> -addr :<filesPort>`. Do NOT rely on inherited
  `PORT`/cwd. (The `/ck:run-md-serve-static-files` "use defaults, do not
  invent a port" guidance is for ad-hoc single use; N parallel instances
  legitimately need pinned ports.)

## Port math is not enough (exposure + routing)

Adding `filesPortFromPreview` and a per-port listener is only the
in-container half. A new proxy-port band must also be threaded through
the generated exposure layers, all in `cmd/swe-swe/` (init time), and
the frontend must learn to address it in both modes:

1. **Traefik entrypoints (legacy / NO_TUNNEL mode)** are generated
   per-port in `cmd/swe-swe/templates.go:259-282` (preview, agentchat,
   ...). A files band needs the same generated entrypoints.
2. **docker-compose `ports:` publish list** is built in
   `cmd/swe-swe/init.go:1168-1177` (`extraPorts`) and the
   `{{TUNNEL_LOCAL_PORTS}}` placeholder. The files proxy range must be
   appended there or the host cannot reach the listener.
3. **Validation bound** at `cmd/swe-swe/init.go:620` is already stale --
   `proxyPortOffset + 5019 >= 65536` predates the VNC band (real max is
   7019 today, becomes 9019 with files). Update it to the true ceiling
   (`proxyPortOffset + 9019`) so a large `--proxy-port-offset` cannot
   silently push the files proxy past 65535.
4. **Frontend mode selection.** The browser chooses the URL form at
   runtime from the Status payload's `publicHostname`
   (`terminal-ui.js:1599`): subdomain when tunneled, `localhost:{port}`
   otherwise.

### Tunnel mode addressing (answering "is it `{port}.{unique}.{host}`?")

Yes -- in tunnel mode the iframe src is
`https://{port}.{publicHostname}`, and `publicHostname` is
`{unique}.{tunnelhostname}`, so it reads
`https://{port}.{unique}.{tunnelhostname}` (url-builder.js:137).

The important subtlety: **`{port}` is the proxy port, not md-serve's raw
port.** tunneld dials the per-port listeners directly and bypasses
Traefik's ForwardAuth, so each per-port listener is wrapped in
`requireAuthCookie` and the subdomain label points at *that* wrapped
listener (`main.go:4673`; terminal-ui.js:1596-1601 passes
`agentChatProxyPort`, not the raw port). So for Files the tunnel URL is
`{filesProxyPort}.{publicHostname}` = `29000.{unique}.{tunnelhostname}`,
demuxed to `127.0.0.1:29000` inside the container. proxyPortOffset still
applies in both modes.

## Port layout (after implementation)

| Range       | Purpose                | Derived from   |
|-------------|------------------------|----------------|
| 3000-3019   | Preview (app)          | base (config)  |
| 4000-4019   | Agent Chat             | preview + 1000 |
| 5000-5019   | Public (no auth)       | preview + 2000 |
| 6000-6019   | CDP (Chrome DevTools)  | preview + 3000 |
| 7000-7019   | VNC (Agent View)       | preview + 4000 |
| **9000-9019** | **Files (md-serve)** | **preview + 6000** |

(8000-8019 deliberately skipped; non-contiguity is fine.)

Per-port proxy listeners use the existing offset (`proxyPortOffset`, default 20000):

| 23000-23019 | Preview proxy |
| 24000-24019 | Agent Chat proxy |
| 26000-26019 | CDP proxy |
| 27000-27019 | VNC proxy |
| **29000-29019** | **Files proxy** |

## End-to-end flow

```
Browser (Files tab iframe)              swe-swe-server (Go)              md-serve
--------------------------              ------------------              --------
local:  http://localhost:29000/    -->  filesProxy listener :29000
tunnel: https://29000.{unique}.         requireAuthCookie + cors
        {tunnelhost}/  (tunneld    -->  reverse-proxy to :9000     -->  md-serve -dir <workDir>
        demux -> 127.0.0.1:29000)                                       -addr :9000
                                        <- rendered HTML / listing <-   (live-reload poll 1s)
```

All `main.go` anchors are in
`cmd/swe-swe/templates/host/swe-swe-server/main.go` (an embedded
template, so changes flow into golden output). Init-time generation is
in `cmd/swe-swe/` (init.go / templates.go).

---

## Phase 1: Port allocation for Files

### What
Add the Files port as the next derived per-session port. No process yet.

### Steps

1. Add range constants alongside the others (`main.go:104-117`):
   ```go
   filesPortStart = 9000
   filesPortEnd   = 9019
   ```
2. Add the derivation + proxy-port helpers next to the existing ones
   (`main.go:4097-4111`, `main.go:119-122`):
   ```go
   func filesPortFromPreview(previewPort int) int { return previewPort + 6000 }
   func filesProxyPort(port int) int              { return proxyPortOffset + port }
   ```
   Note: `previewProxyPort` etc. in `cmd/swe-swe/` (templates.go,
   init.go) take an explicit `proxyPortOffset` argument -- add a matching
   `filesProxyPort(port, offset int)` there too.
3. Add `FilesPort int` to the `Session` struct (next to `VNCPort`,
   `main.go:462`).
4. Set `sess.FilesPort = filesPortFromPreview(previewPort)` where the
   quintuple is assigned. Either extend
   `findAvailablePortQuintuple` -> `...Sextuple`
   (`main.go:4281-4301`, matches how CDP/VNC were added), or derive at
   the call site (smaller diff, since it is a pure function of
   previewPort). Deriving at call site is simpler.

### Verification
- Unit test: create a session, assert `FilesPort == PreviewPort + 6000`
  and within 9000-9019.
- Existing port-allocation tests pass unchanged.
- `make test` passes.

---

## Phase 2: Spawn and supervise md-serve per session

### What
Each session launches its own `md-serve` rooted at its workDir and tears
it down on session end.

### Steps

1. Install the binary in the runtime stage of
   `cmd/swe-swe/templates/host/Dockerfile`, next to the other global npm
   installs (`Dockerfile:157-173`). **Pin the version**:
   ```dockerfile
   RUN npm install -g @choonkeat/md-serve@0.1.0
   ```
   (npm global bin is already on PATH via `Dockerfile:104`.)
2. Add `FilesPID int` (or reuse a `[]int` slice) to the `Session` struct
   for cleanup tracking.
3. Add `startSessionMdServe(sess *Session) error` modeled on
   `startSessionBrowser` (`main.go:4119`):
   ```go
   cmd := exec.Command("md-serve", "-dir", sess.WorkDir,
       "-addr", fmt.Sprintf(":%d", sess.FilesPort))
   // do NOT inherit PORT; set a clean env or override PORT explicitly
   ```
   - `cmd.Start()`, then `trackPid` / `registerSessionPid` and store the
     PID on the session.
   - Supervising goroutine that `cmd.Wait()`s and **logs name + PID +
     exit status** (CLAUDE.md: never silently discard child exit
     status). Optional: restart-on-unexpected-exit, since md-serve is
     our managed process (unlike the user's preview app).
4. Add `stopSessionMdServe(sess *Session)` that kills the tracked PID.
5. Wire `startSessionMdServe` into session creation and
   `stopSessionMdServe` into cleanup next to `stopSessionBrowser`
   (`main.go:1016-1017`).
   - **Eager vs on-demand:** md-serve is a cheap Go binary, so eager
     start at session creation is simplest. To match the newer on-demand
     browser pattern (`tasks/2026-03-13-on-demand-browser.md`), gate it
     behind first Files-tab open instead. Default to eager.

### Verification
- Integration: start a session, assert md-serve is running and `:9000`
  listening; end session, assert process reaped and port freed.
- Kill md-serve manually; confirm the goroutine logs the exit (and
  restarts if that option is taken).
- `make test` passes.

---

## Phase 3: Per-port proxy listener + Status payload

### What
Expose md-serve through an auth-checked per-port listener and advertise
the proxy port to the frontend.

### Steps

1. In the block that starts the preview/agent-chat/vnc per-port
   listeners (`main.go:4664-4736`), add a Files listener on
   `filesProxyPort(sess.FilesPort)` (29000-29019):
   - plain `httputil.NewSingleHostReverseProxy` to
     `http://localhost:<FilesPort>` (no DebugHub, no inject.js).
   - wrap in `corsWrapper(requireAuthCookie(authPassword, proxy))` --
     same wrapping as preview (`main.go:4683`). This wrapping is what
     makes tunnel-mode safe (tunneld bypasses Traefik ForwardAuth).
   - store the `*http.Server` on the session (e.g. `FilesProxyServer`).
2. Shut it down in cleanup alongside `PreviewProxyServer` /
   `VNCProxyServer` (`main.go:1006-1013`).
3. Add to the Status payload (`main.go:835-848`):
   ```go
   "filesProxyPort": filesProxyPort(s.FilesPort),
   ```
   `publicHostname` is already in the payload (`main.go:847`), so the
   frontend can pick subdomain vs local mode without further changes.

### Verification
- `curl` the files proxy port without the auth cookie -> rejected; with
  cookie -> rendered listing of the workDir.
- Status JSON includes `filesProxyPort`.
- `make test` passes.

---

## Phase 4: Exposure and routing (init-time generation)

### What
Thread the new proxy band through the generated Traefik entrypoints,
compose port-publish lists, and the validation bound. This is the
"port math is not enough" layer; skipping it means the listener exists
but is unreachable from the host / tunnel.

### Steps

1. `cmd/swe-swe/templates.go:259-282`: add a files block to the
   per-port entrypoint generation (mirror the `preview%d` /
   `agentchat%d` loops) so legacy/NO_TUNNEL Traefik gets files
   entrypoints.
2. `cmd/swe-swe/init.go:1168-1177`: append the files proxy range to
   `extraPorts` (the docker-compose `ports:` list), mirroring the vnc
   line, and to `{{TUNNEL_LOCAL_PORTS}}`.
3. `cmd/swe-swe/init.go:620`: update the stale validation bound from
   `proxyPortOffset + 5019` to the real ceiling (`proxyPortOffset +
   9019`) so a large offset cannot push files proxy past 65535.
4. Add a `filesProxyPort(port, offset int)` helper in `cmd/swe-swe/`
   (the init-side counterpart, since these take an explicit offset).

### Verification
- `make build golden-update`: diff shows new files entrypoints + compose
  port mappings (preview/agentchat/vnc-style) and nothing else
  unexpected.
- A bad `--proxy-port-offset` that would overflow files proxy is now
  rejected.
- `make test` passes.

---

## Phase 5: Files tab in the frontend

### What
Add the tab to the pane registry and point its iframe at the files proxy
port, choosing subdomain vs local form by `publicHostname`.

Anchors in
`cmd/swe-swe/templates/host/swe-swe-server/static/`.

### Steps

1. Add `'files'` to `PANES_IN_ORDER` (`terminal-ui.js:61`) and
   `'files': 'Files'` to `PANE_LABELS` (`terminal-ui.js:64`).
2. Add `buildSubdomainFilesUrl` and `buildPortBasedFilesUrl` to
   `static/modules/url-builder.js` (clones of the agent-chat pair at
   lines 102-105 / 150-153; they are identical shape -- just
   `{proxyPort}.{publicHostname}` vs `localhost:{proxyPort}`).
3. In `terminal-ui.js`, store `this.filesProxyPort` from the Status
   message (mirror `previewProxyPort` at `terminal-ui.js:1549`) and pick
   the URL form the same way agent-chat does (`terminal-ui.js:1599`):
   ```js
   const filesUrl = this.publicHostname
     ? buildSubdomainFilesUrl(window.location, this.filesProxyPort, this.publicHostname)
     : buildPortBasedFilesUrl(window.location, this.filesProxyPort);
   ```
4. Add the iframe pane-host for `files` (same shape as preview/shell);
   md-serve renders full pages, so a plain iframe -- no shell-page /
   inject wrapper.
5. Leave it OUT of the default layout presets (`terminal-ui.js:41-48`)
   so it is an opt-in tab, unless we decide one preset should default
   to it.

### Verification
- Files tab appears; selecting it loads a rendered listing (no
  "Connecting..." placeholder) in both local and tunnel deployments.
- Editing a file in the session updates the Files view within ~1s.
- `url-builder.test.js`: add cases for the two new builders.

---

## Phase 6: tdspec spec update

### Steps

1. Add a `Files` variant to the `Pane` type
   (`tdspec/src/TerminalUi.elm:172-178`), doc comment BEFORE the variant
   (elm-format moves trailing comments -- MEMORY.md).
2. Add `Files` to `allPanes` (`:183`) and the exported doc list (`:4`,
   `:42`). Document it like `Shell`/`Browser`.

### Verification
- `make test` (tdspec build) passes.

---

## Phase 7: Golden, e2e, docs

### Steps

1. `make build golden-update`, then:
   ```bash
   git add -A cmd/swe-swe/testdata/golden
   git diff --cached -- cmd/swe-swe/testdata/golden
   ```
   Expect: Dockerfile md-serve install; Traefik files entrypoints;
   compose files port mappings. (Use `make golden-update`, not
   per-variant targets -- CLAUDE.md.)
2. e2e: extend `e2e/tests/terminal-ui-tabs.spec.js` (Files tab present +
   iframe loads) and `e2e/tests/ports.spec.js` (9000/29000 ranges).
3. CHANGELOG entry; update any docs enumerating tabs/ports.

### Verification
- `make test` passes; golden diff clean and expected.
- Test-container run (docs/dev/test-container-workflow.md): two sessions,
  Files in each shows its own workDir, independent; end sessions, md-serve
  reaped.

---

## Out of scope
- The Code (code-server) tab -- untouched.
- Path-based (same-origin) Files mount -- cross-origin only (local port +
  tunnel subdomain).
- A configurable `--files-ports` flag -- the range is derived. Add later
  only if a deployment needs to relocate it.
- Write/edit capability -- md-serve is read-only by design.

## Running this in a worktree
Self-contained; suited to `/swe-swe:execute-in-worktree`, then
`/swe-swe:merge-worktree` to land on local main. Each phase ends green
(`make test`) for incremental review. Phases 3-5 are the ones that touch
both deployment modes -- verify in both a local and a tunnel deployment.
