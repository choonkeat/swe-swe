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
  const browser = await chromium.launch({
    executablePath: '/usr/bin/chromium',
    args: ['--no-sandbox', '--disable-gpu'],
  });
  const baseURL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;
  const context = await browser.newContext({ baseURL });
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
