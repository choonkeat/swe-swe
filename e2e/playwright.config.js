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
    // compose mode terminates TLS at Traefik with a self-signed cert
    // (selfsign@host.docker.internal). simple/docker mode is plain http, where
    // this is a no-op. Without it, every navigation in compose mode fails with
    // net::ERR_CERT_AUTHORITY_INVALID.
    ignoreHTTPSErrors: true,
    // Default: every spec starts already logged in via the cookie that
    // global-setup.js captured. login.spec.js opts out per-file via
    // test.use({ storageState: { cookies: [], origins: [] } }) because
    // it tests the login flow itself.
    storageState: storageStatePath,
    headless: true,
    launchOptions: {
      // Default to the system chromium; CHROMIUM_BIN overrides it when that
      // build is broken on the host (see scripts/e2e-test.sh + global-setup.js).
      executablePath: process.env.CHROMIUM_BIN || '/usr/bin/chromium',
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
