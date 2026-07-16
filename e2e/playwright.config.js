import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig } from '@playwright/test';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const baseURL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;
const storageStatePath = path.join(__dirname, '.auth', 'state.json');

// The everyday suite is deterministic: it opens `shell` sessions or waits only
// on the agent-chat sidecar becoming available (~15s, no model call). The one
// test that needs a live model to actually DO something -- agent-browser
// (agent -> Playwright MCP -> CDP -> screenshot) -- is the "capstone" and is
// slow + provider-flaky, so it is excluded by default and opted into with
// E2E_LLM=1 (`make test-e2e-llm`), which runs ONLY the capstone.
const runLLM = !!process.env.E2E_LLM;

export default defineConfig({
  testDir: './tests',
  globalSetup: './global-setup.js',
  // Default suite excludes the LLM capstone; E2E_LLM=1 runs only it.
  testMatch: runLLM ? ['**/agent-browser.spec.js'] : undefined,
  testIgnore: runLLM ? undefined : ['**/agent-browser.spec.js'],
  timeout: 180_000, // 3 minutes per test (AI agent needs time)
  expect: {
    timeout: 120_000, // 2 minutes for assertions (waiting for AI response)
  },
  // Default suite is deterministic, so a retry is cheap insurance (it only runs
  // on failure) and avoids a whole-suite re-run over one transient flake. The
  // LLM capstone is provider-flaky, so give it an extra retry.
  retries: runLLM ? 2 : 1,
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
