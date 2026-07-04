# Distribution: ship binaries via the embedded payload (npm)

## Status

**Planned / mostly subsumed.** The embedded-payload approach (see
`tasks/2026-06-27-dockerless-single-binary.md` "Approach: embedded
payload") changes how binaries are distributed: the `swe-swe` CLI
`go:embed`s the prebuilt binaries and dumps them on
`swe-swe init --dockerless`. So there is **no separate server binary to
publish** -- npm ships the CLI exactly as today, and the CLI carries the
payload. This file now only tracks the packaging deltas that the embed
model still needs.

Distribution is **npm/npx only**. No Homebrew.

## What the embed model already gives us

- One `npm i -g swe-swe` (or `npx -y swe-swe`) delivers the CLI + every
  binary it embeds. No second install step, no `swe-swe-server` in the
  user's vocabulary.
- Per-platform delivery already exists: `build-platforms` builds the CLI
  per GOOS/GOARCH and npm uses per-platform optionalDependencies, so each
  user downloads one artifact.

## Remaining packaging work

1. **Build matrix carries the payload.** Ensure `build-platforms` runs
   the Phase 1 binary-build + payload-stage step before each per-platform
   CLI build, so every published CLI embeds the correct Linux set (and,
   in Phase 6, the darwin host set for darwin CLIs). See Phase 1 of the
   main task.
2. **Size budget.** Embedding the binaries adds ~25-30MB per CLI
   platform. Confirm the npm per-platform packages stay within sane
   limits; record before/after. `-ldflags="-s -w"` already applied.
3. **`npx -y swe-swe` path.** Verify the no-global-install path still
   works end-to-end with the larger artifact (download + dump + run).
4. **Version coherence.** The embedded `swe-swe-server` reports the same
   version as the CLI (stamped at build time, as `init.go` already does
   for the in-image source today).

## Non-goals

- Homebrew formula (dropped).
- A standalone published `swe-swe-server` artifact (the CLI embeds it).
