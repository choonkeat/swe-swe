# prctx: `show` renders already-flushed replies as still "staged"

**Started**: 2026-07-01
**Goal**: Stop `prctx show` from displaying a `> staged reply` for replies that have already been posted upstream.
**Severity**: Cosmetic / misleading output. **No data-loss or double-post risk** (see Root Cause).

---

## Symptom

After a successful `prctx flush`, re-running `prctx show <pr>` (or `--json`) still lists each
flushed reply under its thread as `> staged reply: ...`, even though the flush reported
`flushed 5 item(s)` and a fresh `prctx fetch` shows the reply as a real posted comment.

Observed on `choonkeat/tiny-form-fields#60`: all 5 replies posted successfully, yet:

```
## thread PRRT_kwDOMGhaz858tV9G  src/Main.elm:3068  [unresolved]
  wynn987: more accurate since we do want them to fill this in ...
  choonkeat: Makes sense — `.+` enforces non-empty even with no other constraint. Good.   <- real, posted
  > staged reply: Makes sense — `.+` enforces non-empty even with no other constraint. Good.   <- stale, duplicate
```

The header even reads `0 staged draft(s)`, contradicting the per-thread `> staged reply` lines.

---

## Root Cause

Two different code paths clear their "pending" marker differently after a flush:

- **Resolve** clears its flag: `flush.go:103` sets `t.PendingResolve = false` after a successful resolve.
- **Reply** does NOT clear `t.PendingReply`. Instead it records an idempotency stamp:
  `flush.go:59` sets `t.PostedReplyID = id` and leaves `t.PendingReply` untouched.

`show.go` renders the staged-reply line based only on whether the text is present:

```go
// cmd/prctx/show.go:27
if t.PendingReply != "" {
    fmt.Fprintf(w, "  > staged reply: %s\n", oneLine(t.PendingReply))
}
```

Because `PendingReply` is never emptied, the line renders forever, regardless of `PostedReplyID`.

**Why this is safe (not a double-post):**
`flush.go:52` skips any thread where `PostedReplyID != 0`, and `mergePending` (main.go:185)
carries `PostedReplyID` across a re-fetch. So a re-flush correctly no-ops on already-posted
replies. The bug is purely presentational.

---

## Fix

### Step 1: Gate the staged-reply display on the posted stamp
**Goal**: Only show `> staged reply` for replies that have NOT been posted yet.

**What to do** — in `cmd/prctx/show.go` (`render`), change:
```go
if t.PendingReply != "" {
```
to:
```go
if t.PendingReply != "" && t.PostedReplyID == 0 {
```

**Files to modify**:
- `cmd/prctx/show.go`

**Test procedure**:
1. `prctx fetch <pr>`; `prctx reply <pr> <thread-id> "test"`.
2. `prctx show <pr>` → shows `> staged reply: test`. ✅ (unposted)
3. `prctx flush <pr>` → `posted reply ...`.
4. `prctx show <pr>` → NO `> staged reply` line for that thread. ✅ (posted)
5. `prctx flush <pr>` again → `nothing staged to flush` / no double-post. ✅

**Status**: [x] DONE (show.go gated on `PostedReplyID == 0`; bundled copy synced; `make test-prctx` green)

---

### Step 2 (optional): Same gate for `--json` consumers
**Goal**: Machine consumers of `show --json` shouldn't treat a posted reply as pending either.

`renderJSON` dumps the raw `State`, so `pending_reply` remains populated alongside a non-zero
`posted_reply_id`. A JSON consumer can already disambiguate via `posted_reply_id`, so this is
only worth doing if any tooling keys off `pending_reply` alone. If addressed, prefer documenting
the invariant ("pending_reply is authoritative only when posted_reply_id == 0") over mutating the
struct, to keep the idempotency stamp + reply text intact for audit/history.

**Status**: [ ] PENDING (decide if needed)

---

## Alternatives considered

- **Clear `PendingReply` on flush** (symmetry with `PendingResolve`): simpler mental model, but
  discards the local record of what was replied. The posted comment still comes back on the next
  `fetch` as a real comment, so no information is truly lost — but the display gate (Step 1) keeps
  both the stamp and the text without special-casing, so it's preferred.

---

## Notes

- Discovered while flushing review replies for `tiny-form-fields#60` on 2026-07-01.
- Keep the fix display-only; do not touch `flush.go` idempotency logic.
