import { test, expect } from '@playwright/test';

const PASSWORD = process.env.SWE_SWE_PASSWORD || 'changeme';

test.describe('Login', () => {
  test('shows login page', async ({ page }) => {
    await page.goto('/');
    // Should redirect to login
    await expect(page).toHaveURL(/swe-swe-auth\/login/);
    await expect(page.locator('input[type="password"]')).toBeVisible();
  });

  test('rejects wrong password', async ({ page }) => {
    await page.goto('/swe-swe-auth/login');
    await page.fill('input[type="password"]', 'wrongpassword');
    await page.click('button[type="submit"]');
    // Should stay on login page with error
    await expect(page).toHaveURL(/swe-swe-auth\/login/);
  });

  test('accepts correct password and redirects to home', async ({ page }) => {
    await page.goto('/swe-swe-auth/login');
    await page.fill('input[type="password"]', PASSWORD);
    await Promise.all([
      page.waitForNavigation(),
      page.click('button[type="submit"]'),
    ]);
    // Should redirect to home page (not back to login)
    const url = page.url();
    expect(url).not.toContain('swe-swe-auth/login');
  });
});
