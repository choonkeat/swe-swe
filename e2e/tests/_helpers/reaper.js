// Auto-reap fixture: after every test, end all live sessions so Chrome + npx
// sidecars don't accumulate across the run.
//
// The umbrella runs Playwright with workers:1 (tests share one server), so
// without a per-test reap the resident session set grows monotonically: every
// session-opening test leaves its chromium/node/md-serve/agent-chat tree behind.
// On this box that slides host MemAvailable from ~1.5 GB to ~287 MB over a
// single spec file, which (a) OOM-crashes the server and (b) trips the server's
// memory-admission gate into false-refusing new sessions -> a cascade of
// waitForFunction timeouts. Reaping after each test caps the resident set to
// ~one session at a time.
//
// Specs that open sessions import `test`/`expect` (and `chromium`/`request`)
// from here instead of '@playwright/test'. New specs get the cap for free.
//
// Escape hatch: KEEP_SESSIONS_ON_FAIL=1 preserves a failed test's session for
// local inspection (skips the reap on failure only; passing tests always reap).
import { test as base } from '@playwright/test';
import { endAllSessions } from './sessions.js';

export const test = base.extend({
  // auto:true -> runs for every test in files that import this `test`, with no
  // per-test opt-in. The teardown (after `use`) is where the reap happens.
  autoReapSessions: [
    async ({ page }, use, testInfo) => {
      await use();
      if (testInfo.status !== 'passed' && process.env.KEEP_SESSIONS_ON_FAIL === '1') {
        return;
      }
      try {
        await endAllSessions(page);
      } catch {
        // Best-effort: the server may be mid-teardown or a page may be
        // detached. A stray session at worst delays the next reap; it is not
        // worth failing an otherwise-green test over.
      }
    },
    { auto: true },
  ],
});

export { expect, request, chromium, devices } from '@playwright/test';
