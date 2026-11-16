# Task: `mcp` CLI can list and read MCP resources (for MCP Apps pages)

## Status: IN PROGRESS -- Phases 1-3 done (Phase 1 = b79026114; Phase 2 uncommitted); Phase 4 e2e pending

Filed 2026-11-16 from the agent-chat `mcp-apps` branch discussion.

## Why

agent-chat is adding support for **MCP Apps** (SEP-1865, first official MCP
extension, Jan 2026): a tool declares an interactive HTML page in its
definition, e.g.

```json
{ "name": "show_calendar", "_meta": { "ui": { "resourceUri": "ui://calendar/view.html" } } }
```

and the host fetches that page with the standard MCP `resources/read` request
and renders it in a sandboxed iframe. In swe-swe the terminal agent can't render
anything, so agent-chat will show the page inside a chat bubble. agent-chat's
step 1: **the agent fetches the page and hands it to an agent-chat tool**
("show this page").

- Native-MCP mode: Claude Code can fetch it already (it has a read-resource tool).
- **MCP-less mode (`--without-mcp`): the agent can't.** `cmd/mcp/main.go` only
  issues `tools/list` and `tools/call` (lines ~155 and ~747). There is no way to
  reach `resources/list` / `resources/read`.

The proxy is NOT the blocker: `cmd/mcp-cli-proxy/main.go` `handleConn`
forwards any JSON-RPC method to the child verbatim (it only peeks
`tools/call` for blocking-tool notices). So this is a client-only change in
`cmd/mcp`.

## Compatibility rule (must hold)

agent-chat and swe-swe ship independently; agent-chat reaches users first
(latest on every session), swe-swe upgrades are less predictable. So:

- agent-chat must work on swe-swe builds WITHOUT this feature (it falls back:
  page not shown, or a plain text note). Nothing in swe-swe may require a newer
  agent-chat.
- The feature must be **discoverable by the agent**: `mcp -h` (top help) and
  `mcp <server> -h` must mention it, so agent instructions can say "if `mcp -h`
  lists `--read-resource`, use it".
- On an OLD `mcp`, the new invocation must fail cleanly (it does today:
  `mcp <server> --read-resource ...` -> "has no tool" error, exit 1). Keep the
  new syntax dash-prefixed so it can never collide with a real tool name.

## Proposed surface

```
mcp <server> --list-resources              # resources/list (+ resources/templates/list if supported)
mcp <server> --read-resource <uri>         # resources/read; writes contents to a file, prints path
mcp <server> --read-resource <uri> --stdout   # print text contents to stdout instead
```

Output of `--read-resource` (mirror the existing image handling in `render`):

- For each entry in `result.contents[]`:
  - `text` -> write to a temp file under `imageDir()` (rename the helper or add
    `outDir()`), extension from `mimeType` (`text/html;profile=mcp-app` ->
    `.html`; add to `extFor`), print
    `[resource <uri> <mimeType> saved to <path>]`.
  - `blob` -> base64-decode, same file handling.
- Pass through any `_meta` on the content entry (MCP Apps puts CSP / domain /
  permission hints there) as one JSON line on stdout:
  `[resource-meta <uri>] {...}` — agent-chat needs it to set the iframe's
  security policy. Don't drop it.
- Why a file, not stdout, by default: app pages are often 100s of KB of bundled
  HTML/JS; dumping that into the agent's context is wasteful. The agent passes
  the path (or reads it) when calling agent-chat.

Also in help output:

- `mcp <server> -h`: for each tool whose definition has
  `_meta.ui.resourceUri`, add a line `page: <uri>  (mcp <server> --read-resource <uri>)`.
  This requires keeping `_meta` on the `tool` struct (currently not decoded).
- `printTopHelp`: document both new flags.

Error handling: JSON-RPC error from the server (e.g. `-32601 method not found`
for servers without resources) -> print `mcp: <server> does not serve
resources` and exit 1. Unknown URI -> server's error message, exit 1.

## Open question to check while implementing (do not block on it)

MCP Apps servers may only expose `_meta.ui` (or the UI tools at all) when the
client advertises the UI extension in `initialize` capabilities. Today
`mcp-cli-proxy` sends `"capabilities":{}` (`startChild`, ~line 411) with
protocol `2024-11-05`. Check the SEP-1865 spec for the exact capability key
(expected under `capabilities.extensions`, name like
`io.modelcontextprotocol/ui`). If servers gate on it, add a proxy flag to
advertise it — separate commit, since advertising UI support when no UI is
reachable could mislead servers. Record the finding in this file either way.

**Answered 2026-11-16:** the key is
`capabilities.extensions["io.modelcontextprotocol/ui"] = {"mimeTypes":["text/html;profile=mcp-app"]}`,
and the spec says "Servers SHOULD check client capabilities before registering
UI-enabled tools" (text-only fallback otherwise). So spec-following servers
WILL hide pages from the proxy as it is today. Claude Code 2.1.325 doesn't
advertise it either (its initialize capabilities are only `roots` +
`elicitation`; measured with agent-chat's `testdata/mcp-apps-sample --ui=auto`,
which logged `ui=false`). The proxy flag is therefore needed for MCP Apps to
work in MCP-less mode at all; promote phase 3 from "investigate" to "implement
`--advertise-ui`" (off by default, turned on by swe-swe only once agent-chat
can display pages).

## Review notes (2026-11-16)

Verified: proxy forwards any method verbatim; both proxy and `cmd/mcp`
scanners allow 16 MB lines, so 100s-of-KB pages fit. Line refs above match.

Plan amendments:

1. **Do Phase 3 first.** (DONE, see findings below.) If servers gate `_meta.ui` on a client capability,
   `page:` lines never appear and Phase 4 fails -- the proxy flag may be a
   prerequisite, not a side commit.
2. **Sanitize the URI for the temp filename.** `os.CreateTemp` rejects
   patterns containing a path separator; `ui://calendar/view.html` has `/`.
   Map non-`[A-Za-z0-9._-]` to `_`, cap length.
3. **Strip MIME params before `extFor`.** It matches exact strings;
   `text/html;profile=mcp-app` must go through `mime.ParseMediaType` first.
4. **`--list-resources` partial support.** If `resources/list` succeeds but
   `resources/templates/list` returns -32601, print a note, exit 0. Exit 1
   only if `resources/list` itself fails.
5. **`--stdout` with a `blob` entry.** Refuse that entry (stderr:
   "binary content, omit --stdout to save to a file"), exit 1.

## Phase 3 findings (2026-11-16) -- NOT a blocker

Sources: ext-apps `specification/2026-01-26/apps.mdx` (stable),
`src/server/index.ts`, `examples/basic-server-vanillajs/server.ts`.

- Capability key (exact): `initialize` params
  `capabilities.extensions["io.modelcontextprotocol/ui"] = {"mimeTypes":["text/html;profile=mcp-app"]}`.
  The spec's own example uses protocolVersion `2024-11-05`, so our proxy's
  version is fine.
- Gating is only SHOULD: "Servers SHOULD check client capabilities before
  registering UI-enabled tools." No MUST to omit `_meta.ui`.
- The SDK does NOT gate: `registerAppTool` / `registerAppResource` always
  register; `getUiCapability()` is a pure accessor, branching is left to the
  server author. The basic example server never reads client capabilities.
- Verdict: phases 1-2 work against SDK-default servers without any proxy
  change. Servers that hand-roll the SHOULD check will hide UI tools from
  MCP-less sessions -- the optional `--advertise-ui` proxy flag stays a
  separate, later commit.

Spec details that change Phases 1-2:

- Tool key: decode BOTH `_meta.ui.resourceUri` (preferred) and the
  deprecated flat `_meta["ui/resourceUri"]` for the `page:` help line.
- Page mimeType is exactly `text/html;profile=mcp-app`; URIs start `ui://`.
- Security hints live in `_meta.ui` on the `resources/read` content entry
  (`csp.{connectDomains,resourceDomains,frameDomains,baseUriDomains}`,
  `permissions`, `domain`, `prefersBorder`); absent csp means a restrictive
  default. The SDK also treats `_meta.ui` on the `resources/list` entry as a
  fallback -- `--list-resources` should print it too.

## Phases

1. **`--read-resource`** in `cmd/mcp/main.go` + tests in `main_test.go` using
   the existing fake-socket pattern (`relay_test.go`): text content, blob
   content, `_meta` passthrough, server-without-resources error, old-syntax
   collision check (a tool literally named `--read-resource` is impossible, so
   just assert dash-prefixed args are routed before tool lookup).
2. **`--list-resources`** + help text (top help, server help with `page:` lines;
   decode `_meta` on `tool`).
3. **Investigate the capability question** above; write findings here; proxy
   flag only if needed.
4. **End-to-end check** in an MCP-less session against a sample MCP Apps server
   (agent-chat plans a test-only sample server in its repo; any server from
   github.com/modelcontextprotocol/ext-apps examples also works):
   `mcp <server> -h` shows the `page:` line; `--read-resource` saves the HTML.

## Out of scope

- Routing ALL tool traffic (native-MCP mode too) through `mcp-cli-proxy` so
  page buttons can call tools directly. That's a larger, separate decision;
  agent-chat's interim plan is that a button click arrives as a chat message
  and the agent calls the tool itself.
