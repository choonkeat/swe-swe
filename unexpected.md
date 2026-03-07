# Unexpected Issues During Implementation

## 1. Golden variant `with-proxy-port-offset` already existed in Makefile but had no flag support

The Makefile already had line 172 (`@$(MAKE) _golden-variant NAME=with-proxy-port-offset FLAGS="--proxy-port-offset 50000"`) before the flag was implemented. This meant the golden directory only contained a `stderr.txt` with an error message. Once Commit 1 added the flag, the variant went from error-only to a full init output — creating ~80 new files in one commit.

**Conclusion:** The Makefile was pre-staged for this work. The large diff in Commit 1 was expected and correct.

## 2. `processSimpleTemplate` call sites needed both new params even for traefik-dynamic.yml

The `traefik-dynamic.yml` template doesn't use any of the 6 expansion blocks (PREVIEW_ENTRYPOINTS, etc.), but because `processSimpleTemplate` has a single signature, both call sites (docker-compose.yml and traefik-dynamic.yml) had to pass `previewPortsRange, *proxyPortOffset`. Passing `nil` ports to traefik-dynamic.yml is harmless — the expansion blocks simply never match.

**Conclusion:** No issue. The unified function signature is cleaner than splitting into two functions, and passing unused params is safe since the template content controls which expansions fire.

## 3. `git add` with explicit file paths failed from wrong working directory

Running `git add cmd/swe-swe/templates/host/swe-swe-server/static/modules/url-builder.js` failed with a cryptic error about paths not matching. The shell's CWD had drifted into a subdirectory during the JS test run (`cd ... && node --test`).

**Conclusion:** Had to use broader glob (`git add cmd/swe-swe/templates/host/swe-swe-server/static/`) from the repo root. Lesson: always use absolute paths or verify CWD before git operations.

## 4. Per-port listeners need CORS but path-based routes don't

The browser on `:1977` probing `:23000` is cross-origin, requiring `Access-Control-Allow-Origin`, `Access-Control-Allow-Credentials`, and `Access-Control-Expose-Headers` on every response including OPTIONS preflight. The path-based routes (`/proxy/{uuid}/preview/...`) are same-origin and need none of this.

**Conclusion:** Added a `corsWrapper()` that only wraps per-port listeners. The wrapper echoes back `Origin` rather than using `*` because `credentials: 'include'` is needed for auth cookies, and `*` is incompatible with credentialed requests.

## 5. The `proxyPortOffset` injection pattern differs from version injection

Version is injected via `strings.Replace` on a quoted string (`Version = "dev"` -> `Version = "2.11.0"`). But `proxyPortOffset` is an unquoted int (`proxyPortOffset = 20000` -> `proxyPortOffset = 50000`). Had to match the exact whitespace (`proxyPortOffset    = 20000` with 4 spaces) for the replacement to work.

**Conclusion:** Fragile string replacement. Works because the golden tests catch any mismatch — if the template changes its formatting, tests fail immediately. A template placeholder (`{{PROXY_PORT_OFFSET}}`) would be more robust but would require a separate template processing pass for server main.go.

## 6. Two-phase probe adds latency but is unavoidable

The port-based probe (Phase 2) adds one extra `fetch()` round-trip after the path-based probe succeeds. Considered doing both probes in parallel, but that would cause confusing CORS errors in the console while the proxy isn't up yet.

**Conclusion:** Sequential is correct. Phase 1 waits for the proxy handler to exist (may take seconds during container startup), Phase 2 is a single fast fetch against an already-running server. The mode is cached in `_proxyMode` so subsequent `setPreviewURL` calls skip Phase 2.
