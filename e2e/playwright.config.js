import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig } from '@playwright/test';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const baseURL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;
const storageStatePath = path.join(__dirname, '.auth', 'state.json');

export default defineConfig({
  testDir: './tests',
  globalSetup: './global-setup.js',
  timeout: 180_000, // 3 minutes per test (AI agent needs time)
  expect: {
    timeout: 120_000, // 2 minutes for assertions (waiting for AI response)
  },
  // One retry per test: most flakes here are LLM-driven (the agent-chat
  // probe and the agent-browser tool-use assertion both depend on an
  // OpenCode response within a window). 0 retries means a single slow
  // response kills the whole run; 2+ retries can mask real regressions.
  // 1 is the standard tradeoff: a clean retry hides genuine flakes, a
  // 2/2 failure still fails the suite.
  retries: 1,
  workers: 1, // sequential -- tests share the server
  reporter: 'list',
  use: {
    baseURL,
    // Default: every spec starts already logged in via the cookie that
    // global-setup.js captured. login.spec.js opts out per-file via
    // test.use({ storageState: { cookies: [], origins: [] } }) because
    // it tests the login flow itself.
    storageState: storageStatePath,
    headless: true,
    launchOptions: {
      executablePath: '/usr/bin/chromium',
      args: ['--no-sandbox', '--disable-gpu'],
    },
  },
  projects: [
    {
      name: 'chromium',
      use: { browserName: 'chromium' },
    },
  ],
});
