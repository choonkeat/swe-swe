// Runs once before the entire playwright suite starts. We give the suite a
// clean baseline by ending every session that's currently alive in the
// container. This is the only sweep that touches state across runs --
// individual test afterEach hooks handle within-run cleanup, and only on
// pass, so failed-test state is always preserved until the NEXT suite
// run for inspection.

import { chromium } from '@playwright/test';
import { login, endAllSessions } from './tests/_helpers/sessions.js';

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
  } finally {
    await context.close();
    await browser.close();
  }
}
