import { test, expect } from '@playwright/test';
import crypto from 'crypto';

const PASSWORD = process.env.SWE_SWE_PASSWORD || 'changeme';

async function login(page) {
  await page.goto('/swe-swe-auth/login');
  await page.fill('input[type="password"]', PASSWORD);
  await Promise.all([
    page.waitForNavigation(),
    page.click('button[type="submit"]'),
  ]);
}

async function waitForUi(page, predicate) {
  return page.waitForFunction(predicate, null, { timeout: 60_000 });
}

async function openSession(page) {
  const uuid = crypto.randomUUID();
  await page.goto(`/session/${uuid}?assistant=opencode&session=terminal`);
  await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
  // The set_credentials WS message is silently dropped if the socket is not
  // OPEN yet; wait until the sessionUUID has been delivered (proxy for "WS
  // init message round-trip completed").
  await waitForUi(page, () => window.terminalUI && window.terminalUI.sessionUUID);
  return uuid;
}

async function openSettings(page) {
  const settingsBtn = page.locator('.terminal-ui__settings-btn').first();
  await settingsBtn.click();
  await page.locator('.settings-panel:not([hidden])').waitFor({ timeout: 5_000 });
}

async function closeSettings(page) {
  const closeBtn = page.locator('.settings-panel__close').first();
  await closeBtn.click();
  // Panel uses the `hidden` HTML attribute; locator state 'hidden' resolves
  // when the element is either detached or hidden via that attribute.
  await page.locator('.settings-panel').waitFor({ state: 'hidden', timeout: 5_000 });
}

test.describe('per-session git credentials UI', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('save round-trip: form -> WS -> credentials_stored ack -> status + localStorage', async ({ page }) => {
    await openSession(page);

    // Clear any stale stored creds left over by other tests in this origin.
    await page.evaluate(() => {
      Object.keys(localStorage)
        .filter(k => k.startsWith('swe-swe-creds:'))
        .forEach(k => localStorage.removeItem(k));
    });

    await openSettings(page);

    // The credentials section is rendered in the panel.
    const credsSection = page.locator('.settings-panel__credentials');
    await expect(credsSection).toBeVisible();

    // Default host is github.com; status starts as "Not yet sent to server."
    const hostInput = page.locator('#settings-cred-host');
    await expect(hostInput).toHaveValue('github.com');
    const status = page.locator('#settings-cred-status');
    await expect(status).toHaveText(/Not yet sent to server/);

    // Fill the form with test values. Token field is type=password.
    await page.fill('#settings-cred-username', 'x-access-token');
    await page.fill('#settings-cred-token', 'ghp_test_token_e2e_xyz');
    await page.fill('#settings-cred-name', 'E2E Tester');
    await page.fill('#settings-cred-email', 'e2e@example.com');
    await expect(page.locator('#settings-cred-token')).toHaveAttribute('type', 'password');

    // Click Save. The handler writes localStorage AND sends a WS message.
    await page.click('#settings-cred-save');

    // Status flips to "Sending..." synchronously.
    await expect(status).toHaveText(/Sending\.\.\./);

    // Wait for the credentials_stored ack to update _credsStoredHosts.
    await waitForUi(page, () => {
      const ui = window.terminalUI;
      return Array.isArray(ui?._credsStoredHosts) && ui._credsStoredHosts.includes('github.com');
    });

    // Status now reflects the server-confirmed host list (no values).
    await expect(status).toHaveText(/Stored on server for: github\.com/);
    // ok state attribute is set so the UI can style it green.
    await expect(status).toHaveAttribute('data-state', 'ok');

    // localStorage round-trip: the bag is keyed by host and includes only
    // the values the user entered. The server-side response is NOT stored
    // here -- we only confirm the browser-side write.
    const stored = await page.evaluate(() => {
      const raw = localStorage.getItem('swe-swe-creds:github.com');
      return raw ? JSON.parse(raw) : null;
    });
    expect(stored).toEqual({
      username: 'x-access-token',
      token: 'ghp_test_token_e2e_xyz',
      name: 'E2E Tester',
      email: 'e2e@example.com',
    });

    // Close + re-open: the form should be pre-filled from localStorage so
    // the user can see what's saved and re-Save without re-typing.
    await closeSettings(page);
    await openSettings(page);

    await expect(page.locator('#settings-cred-host')).toHaveValue('github.com');
    await expect(page.locator('#settings-cred-username')).toHaveValue('x-access-token');
    await expect(page.locator('#settings-cred-token')).toHaveValue('ghp_test_token_e2e_xyz');
    await expect(page.locator('#settings-cred-name')).toHaveValue('E2E Tester');
    await expect(page.locator('#settings-cred-email')).toHaveValue('e2e@example.com');
  });

  test('save with empty token shows validation error and does not send WS', async ({ page }) => {
    await openSession(page);

    await page.evaluate(() => {
      Object.keys(localStorage)
        .filter(k => k.startsWith('swe-swe-creds:'))
        .forEach(k => localStorage.removeItem(k));
      // Reset the in-memory hosts list too so prior tests don't leak.
      if (window.terminalUI) window.terminalUI._credsStoredHosts = [];
    });

    await openSettings(page);

    // Token blank, host defaulted to github.com.
    await page.fill('#settings-cred-token', '');
    await page.click('#settings-cred-save');

    const status = page.locator('#settings-cred-status');
    await expect(status).toHaveText(/Host and token are required/);
    await expect(status).toHaveAttribute('data-state', 'err');

    // No ack arrived because no WS message was sent.
    const hosts = await page.evaluate(() => window.terminalUI?._credsStoredHosts || []);
    expect(hosts).not.toContain('github.com');
  });

  test('switching host repopulates form from per-host localStorage', async ({ page }) => {
    await openSession(page);

    // Seed two hosts directly in localStorage so we can isolate the host-
    // change branch from the save-then-reopen branch already covered above.
    await page.evaluate(() => {
      Object.keys(localStorage)
        .filter(k => k.startsWith('swe-swe-creds:'))
        .forEach(k => localStorage.removeItem(k));
      localStorage.setItem('swe-swe-creds:github.com', JSON.stringify({
        username: 'x-access-token', token: 'ghp_github_x', name: 'GH User', email: 'gh@example.com',
      }));
      localStorage.setItem('swe-swe-creds:gitlab.com', JSON.stringify({
        username: 'gitlab-user', token: 'glpat-gitlab-y', name: 'GL User', email: 'gl@example.com',
      }));
    });

    await openSettings(page);

    // Default host github.com pre-fills with GH values.
    await expect(page.locator('#settings-cred-username')).toHaveValue('x-access-token');
    await expect(page.locator('#settings-cred-token')).toHaveValue('ghp_github_x');

    // Change the host to gitlab.com and fire the change event. The handler
    // re-runs populateCredentialsSection() and pulls in the GitLab bag.
    await page.fill('#settings-cred-host', 'gitlab.com');
    await page.locator('#settings-cred-host').dispatchEvent('change');

    await expect(page.locator('#settings-cred-username')).toHaveValue('gitlab-user');
    await expect(page.locator('#settings-cred-token')).toHaveValue('glpat-gitlab-y');
    await expect(page.locator('#settings-cred-name')).toHaveValue('GL User');
    await expect(page.locator('#settings-cred-email')).toHaveValue('gl@example.com');
  });
});
