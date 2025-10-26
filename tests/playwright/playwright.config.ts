import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './specs',
  fullyParallel: false,  // Run tests sequentially for permission tests
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: 1,  // Single worker to avoid conflicts
  reporter: 'html',
  timeout: 60000,  // 60 seconds per test
  
  use: {
    baseURL: 'http://localhost:7000',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },

  projects: [
    {
      name: 'chromium',
      use: { 
        ...devices['Desktop Chrome'],
        // Important for testing permission dialogs
        permissions: [],
        launchOptions: {
          args: ['--disable-blink-features=AutomationControlled'],
        },
      },
    },
  ],

  // Assuming server is already running at localhost:7000
  // No webServer configuration needed
});