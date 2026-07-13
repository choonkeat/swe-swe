// Runs once before the entire playwright suite starts. Two jobs:
//
//   1. Log in, end every session currently alive in the container so the
//      suite starts from a clean baseline. Individual test afterEach hooks
//      handle within-run cleanup, and only on pass, so failed-test state
//      is preserved until the next run for inspection.
//
//   2. Persist the authenticated cookie jar to STORAGE_STATE_PATH so every
//      test starts already logged in. Without this, every spec calls
//      `login(page)` in beforeEach (18 logins/run) which trips the auth
//      rate limiter (`auth.go`: 10 failed POSTs / 5min / IP) under compose
//      latency and surfaces as cascading "Too Many Requests" failures.
//      Reusing the cookie collapses the 18 logins down to 1.
//
// Specs that test the login flow itself opt out by setting
// `test.use({ storageState: { cookies: [], origins: [] } })`.

import { chromium } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { login, endAllSessions } from './tests/_helpers/sessions.js';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
export const STORAGE_STATE_PATH = path.join(__dirname, '.auth', 'state.json');

export default async function globalSetup(config) {
  // Dockerless mode runs with auth disabled (no SWE_SWE_PASSWORD), so there
  // is no login form to drive. Skip the login/baseline-cleanup dance and just
  // persist an empty storage state so the config's storageState path exists;
  // the dockerless spec opts into an empty cookie jar per-file anyway.
  if (process.env.E2E_DOCKERLESS) {
    fs.mkdirSync(path.dirname(STORAGE_STATE_PATH), { recursive: true });
    fs.writeFileSync(STORAGE_STATE_PATH, JSON.stringify({ cookies: [], origins: [] }));
    console.log('[global-setup] E2E_DOCKERLESS: open-auth mode, skipping login');
    return;
  }

  // Default to the system chromium, but allow CHROMIUM_BIN to override it.
  // Some boxes ship a chromium build that crashes on launch (e.g. a newer
  // Debian package whose zygote dies on this kernel); scripts/e2e-test.sh
  // detects that and points CHROMIUM_BIN at a working Playwright-bundled
  // chromium. Keep this in sync with playwright.config.js.
  const browser = await chromium.launch({
    executablePath: process.env.CHROMIUM_BIN || '/usr/bin/chromium',
    args: ['--no-sandbox', '--disable-gpu'],
  });
  const baseURL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;
  // compose mode serves https behind Traefik with a self-signed cert; this
  // context is created directly (not via config `use`), so it must opt into
  // ignoring cert errors itself. No-op for http (simple/docker) modes.
  const context = await browser.newContext({ baseURL, ignoreHTTPSErrors: true });
  const page = await context.newPage();
  try {
    await login(page);
    const n = await endAllSessions(page);
    if (n > 0) console.log(`[global-setup] ended ${n} pre-existing session(s)`);

    fs.mkdirSync(path.dirname(STORAGE_STATE_PATH), { recursive: true });
    await context.storageState({ path: STORAGE_STATE_PATH });
  } finally {
    await context.close();
    await browser.close();
  }
}
