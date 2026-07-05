# Pass parent window URL to agent-chat iframe (`parent_url`)

## Goal

When agent-chat is embedded as an iframe inside swe-swe, relative markdown
links in chat (e.g. `[docs](/repos/foo/bar.md)` or `[r](docs/readme.md)`)
should resolve against the **parent window's URL** (the swe-swe page the user
actually sees), not against agent-chat's own iframe origin.

## Agent-chat side — DONE (in the agent-chat repo, `/repos/agent-chat/workspace`)

`client-dist/app.js` reads a `parent_url` query-string parameter and resolves
relative link/image URLs against it:

```js
var parentBaseUrl = new URLSearchParams(window.location.search).get('parent_url') || '';

function resolveAgainstParent(url) {
  if (!parentBaseUrl) return url;                          // no-op when standalone
  if (/^[a-z][a-z0-9+.-]*:/i.test(url) || /^\/\//.test(url)) return url; // absolute / //host
  try { return new URL(url, parentBaseUrl).href; } catch (e) { return url; }
}
```

`resolveAgainstParent()` is applied in `renderMarkdown()` to both the
`[text](url)` link rule and the `![alt](url)` image rule. Absolute URLs pass
through untouched; with no `parent_url` present, behaviour is unchanged.
Covered by unit-style tests in `e2e/markdown-images.spec.cjs`
("parent base set: …" + "parent_url query param …" cases). All green.
Shipped in agent-chat commit `feat(chat): resolve relative markdown links
against parent window URL`.

## swe-swe side — TODO (this repo, the change this task asks for)

The swe-swe app embeds agent-chat in an iframe. When it builds that iframe
`src`, append the parent (top-level) window URL as a `parent_url` query param,
URL-encoded:

```js
// wherever the agent-chat iframe src is constructed in swe-swe
const base = new URL(agentChatUrl);            // existing agent-chat URL
base.searchParams.set('parent_url', window.location.href);
iframe.src = base.toString();
```

So the iframe ends up loading something like:

```
https://agent-chat.host/?...&parent_url=https%3A%2F%2Fswe-swe.host%2Fsome%2Fpath
```

### Decisions already made (confirmed with user 2026-06-27)

- **What to pass:** the full top-level `window.location.href`. agent-chat's
  `new URL(relative, parent_url)` then handles both leading-slash links (origin
  used) and bare relative links (path used). Do NOT pin just the origin.
- **SPA staleness — accepted for v1:** the param is captured at iframe-src
  construction time. If swe-swe navigates without recreating the iframe,
  `parent_url` goes stale. This is fine for now; do NOT add a postMessage
  update channel — the query string is the agreed mechanism.
- **Encoding:** must be URL-encoded (`searchParams.set` / `encodeURIComponent`
  handles this). agent-chat reads it via `URLSearchParams.get`, which decodes.
- **No agent-chat change needed** once swe-swe passes the param — wiring is
  already in place and shipped.
