# Infra: Debian chromium 150 SIGTRAPs on launch (broke browser feature + e2e)

Date: 2026-07-05
Type: infra / image + e2e harness
Status: RESOLVED -- image pinned to chromium 147; e2e harness has an auto-fallback

## Symptom

Every Playwright e2e spec fails in `global-setup` before any test runs, because
`chromium.launch({ executablePath: '/usr/bin/chromium' })` dies immediately:

```
<process did exit: exitCode=null, signal=SIGTRAP>
```

Reproduces outside Playwright too -- a trivial headless render cores:

```
$ /usr/bin/chromium --headless --no-sandbox --disable-gpu \
    --disable-dev-shm-usage --dump-dom about:blank
... Trace/breakpoint trap (core dumped)   # exit 133 = 128 + SIGTRAP(5)
```

## Root cause: it is the chromium VERSION, not the kernel

- The broken build is `chromium 150.0.7871.46-1~deb12u1` from the Debian
  **bookworm-security** pocket. It SIGTRAPs on launch -- the zygote dies
  (`Failed to send GetTerminationStatus message to zygote`), so every browser
  feature is dead.
- Proven to be version-specific, not host-specific: on the SAME host kernel
  (`6.8.0-134-generic`), chromium **149.0.7827.114** launches fine inside a
  swe-swe container while chromium **150** cores on the host. Playwright's
  bundled `chromium-1208` fails the same way as 150; only the older `1124`
  launches. `ldd` shows no missing libs; `gdb`/`strace` were unavailable to
  capture the exact trapping syscall.
- The release image was NOT yet broken only by luck: an image built before
  150 hit the mirror had 149. But `apt-get install chromium` is unpinned, so
  the next rebuild would pull the broken 150 and ship a dead browser feature.

## apt availability (why we pin to 147, not 149)

- `150.0.7871.46-1~deb12u1` -- bookworm-**security** (broken, current)
- `149.0.7827.114-1~deb12u1` -- bookworm-security (works, but being purged;
  the host mirror already dropped it, so it is not a stable pin)
- `147.0.7727.137-1~deb12u1` -- bookworm/**main** (works; the base-distro
  pocket, permanently available and not bumped by security churn)

So 147 from main is the only stable, known-good pin.

## Fix 1 (shipped): pin the image to chromium 147

The real fix -- so releases are not broken by default. Both image Dockerfiles
now install a pinned, known-good chromium and hold it:

- `cmd/swe-swe/templates/host/Dockerfile` (main image, embedded + re-golden'd)
- `docker/browser-backend/Dockerfile` (lean browser-backend image)

```
chromium=147.0.7727.137-1~deb12u1
chromium-common=147.0.7727.137-1~deb12u1   # must match, else apt pulls 150
&& apt-mark hold chromium chromium-common  # a later apt run can't bump it
```

Verified end-to-end: a fresh `make e2e-up-simple` rebuild installs 147 (both
held) and chromium launches in-container (headless about:blank -> exit 0).

When bookworm/main's chromium eventually rolls to a new point-release version,
bump the pinned string in both Dockerfiles (the exact-version pin fails the
build loudly if the string is purged -- far better than silently pulling a
broken build).

## Fix 2 (shipped): e2e harness auto-selects a launchable chromium

Separate from the image: the Playwright e2e suite runs chromium on the HOST,
and this dev box's host still has the broken system 150 until it is rebuilt.

- `e2e/playwright.config.js` + `e2e/global-setup.js` honor a `CHROMIUM_BIN`
  env var (default `/usr/bin/chromium`, so a healthy CI host is unchanged).
- `scripts/e2e-test.sh` probes `/usr/bin/chromium`; when it fails, it falls
  back to the newest working Playwright-bundled `chromium-*` and exports
  `CHROMIUM_BIN`. Fully automatic -- `make e2e-test` just works.

## Still open (optional)

The exact trapping syscall in chromium 150 was never captured (no gdb/strace).
Not needed now that we pin away from it, but if 150+ must be adopted later,
install `strace`/`gdb`, catch the SIGTRAP/SIGSYS, and adjust the seccomp
profile accordingly.
