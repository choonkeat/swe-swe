# Distribution: ship the server binary via npm

## Status

**Planned.** Not started. Makes
`tasks/2026-06-27-dockerless-single-binary.md` Phase 2 actually
installable by end users.

## Problem

Today the npm package ships only the per-platform **`swe-swe` CLI**
(`package.json` `bin`/`files: ["bin"]`; built by `build-cli` /
`build-platforms`, released by `publish` in the Makefile). The
**`swe-swe-server`** binary is never distributed -- it is compiled inside
`docker build` from embedded source. For the dockerless DX
(`docs/dockerless.md`: `npm i -g swe-swe` then `swe-swe up`) the server
binary must arrive on the user's machine via npm too.

Distribution is **npm/npx only**. No Homebrew.

## Goals / non-goals

- **Goal:** `npm i -g swe-swe` (or `npx -y swe-swe`) puts both `swe-swe`
  and the server binary on the machine, for every supported platform, so
  `swe-swe up` (dockerless) can locate and exec the server.
- **Goal:** the user never names the server binary -- `swe-swe up` finds
  it next to itself.
- **Non-goal:** Homebrew formula (explicitly dropped).
- **Non-goal:** changing how docker mode builds the server (it can keep
  compiling from embedded source, or switch to COPY of the shipped
  binary -- decided in Phase 2).

## Decisions to make

- **One binary or two?** Either (a) ship `swe-swe` + `swe-swe-server` as
  two binaries in the npm artifact, or (b) make `swe-swe` itself the
  multi-call binary (Phase 1) so there is a single file and `swe-swe up`
  re-execs itself in server mode. (b) is the cleanest packaging and keeps
  "the user only types `swe-swe`" literally true. Prefer (b) unless
  size/build reasons block it.
- **Per-platform delivery.** Mirror the existing `build-platforms`
  matrix (linux amd64/arm64, darwin amd64/arm64, ...). Note dockerless
  *runs* only on Linux for now (Linux-only couplings), but the CLI half
  installs everywhere; `swe-swe up --dockerless` can refuse non-Linux
  with a clear message until macOS support lands.

## Work

1. Extend `build-platforms` to also build the server (or the unified
   multi-call binary) for each target; update `dist/` layout.
2. Update `package.json` (`bin`, `files`) and the npm publish flow
   (`publish` target) to include the server binary per platform. Match
   the existing per-platform install mechanism the CLI already uses
   (`install.sh` / postinstall).
3. `swe-swe up` (dockerless dispatch, Phase 3) locates the server binary
   relative to its own path and execs it; error clearly if missing.
4. Keep `npx -y swe-swe` working (no global install) end-to-end.
5. Size check: `-ldflags="-s -w"` already used; record the artifact size
   delta from adding the server.

## Verify

- Clean machine: `npm i -g swe-swe && swe-swe init --dockerless &&
  swe-swe up` works with no separate server install step.
- `npx -y swe-swe init --dockerless` then `npx -y swe-swe up` works.
- `swe-swe --version` and the server's version agree (single source).
- Non-Linux host: `swe-swe up --dockerless` fails with a clear "Linux
  only for now" message rather than a confusing runtime error.
