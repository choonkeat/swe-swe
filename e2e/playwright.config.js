import { defineConfig } from '@playwright/test';

const baseURL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;

export default defineConfig({
  testDir: './tests',
  timeout: 180_000, // 3 minutes per test (AI agent needs time)
  expect: {
    timeout: 120_000, // 2 minutes for assertions (waiting for AI response)
  },
  retries: 0,
  workers: 1, // sequential -- tests share the server
  reporter: 'list',
  use: {
    baseURL,
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
