# Infra: system `/usr/bin/chromium` SIGTRAPs on launch (breaks e2e harness)

Date: 2026-07-05
Type: infra / environment (not application code)
Status: worked around in-repo; root fix pending on the image

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

## Root cause (as far as diagnosable on the box)

- System package: `chromium 150.0.7871.46-1~deb12u1` (Debian bookworm),
  binary `/usr/lib/chromium/chromium` dated 2026-07-04 -- a recent update.
- The crash is in the **zygote**: newer Chromium builds (the system 150 AND
  Playwright's bundled `chromium-1208`) both fail with
  `Failed to send GetTerminationStatus message to zygote`. Only the older
  Playwright `chromium-1124` (dated 2026-03-03) launches cleanly.
- `ldd` shows no missing libraries; kernel is `6.8.0-134-generic`. `gdb` and
  `strace` are not installed, so the exact trapping syscall was not captured,
  but the "only old chromium works / newer zygote dies" pattern points to a
  syscall/seccomp incompatibility the newer build hits on this kernel.
- The swe-swe MCP browser keeps working only because it is a `1124`-era
  process still resident in memory from before the package update; it is not
  evidence that a fresh launch works.

## In-repo workaround (shipped)

The e2e harness now selects a chromium that actually launches instead of
hard-coding the (broken) system binary:

- `e2e/playwright.config.js` and `e2e/global-setup.js` honor a `CHROMIUM_BIN`
  env var, defaulting to `/usr/bin/chromium` (so CI on a healthy host is
  unchanged).
- `scripts/e2e-test.sh` probes `/usr/bin/chromium` with a headless
  `about:blank` render; if it fails, it falls back to the newest working
  Playwright-bundled `chromium-*` and exports `CHROMIUM_BIN`. Fully automatic
  -- `make e2e-test` just works on this box.

This makes the suite runnable but does NOT fix the system chromium itself.

## Real fix (image / infra -- pending)

Pick one:

1. Pin/downgrade the system `chromium` package in the container image to a
   known-good build (pre-150, matching what launches here), or hold the
   package so a security update cannot silently reintroduce a broken build.
2. Install `strace`+`gdb` on the box, capture the exact SIGTRAP/SIGSYS
   syscall, and adjust the seccomp profile / kernel config so Chromium 150's
   zygote can start. Then the harness can drop back to `/usr/bin/chromium`.

Owner: infra. Until then, the `CHROMIUM_BIN` auto-fallback keeps e2e green.
