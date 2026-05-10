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

// Phase A reshaped Settings into a sidebar+tabs layout. The credentials and
// SSH controls live behind data-tab="git" and data-tab="ssh" panes that are
// hidden until the user clicks the nav item. Tests that touch those controls
// must switch tabs explicitly; otherwise fill/toBeVisible/etc. fail on the
// hidden ancestor.
async function switchSettingsTab(page, tab) {
  await page.locator(`.settings-panel__nav-item[data-tab="${tab}"]`).click();
  await page.locator(`.settings-panel__pane[data-pane="${tab}"]:not([hidden])`).waitFor({ timeout: 5_000 });
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
    await switchSettingsTab(page, 'git');

    // The credentials section is rendered in the git tab pane.
    const credsSection = page.locator('.settings-panel__pane[data-pane="git"]');
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
    await switchSettingsTab(page, 'git');

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
    await switchSettingsTab(page, 'git');

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
    await switchSettingsTab(page, 'git');

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

  test('Author fields go readonly when session WorkDir has local .git/config user.*', async ({ page }) => {
    // Connect once so the session row exists with WorkDir=/workspace
    // (the e2e container's /workspace has user.name="E2E Test" /
    // user.email="e2e@test.local" baked into .git/config). Then reload
    // the page so the server can include LocalUser{Name,Email} in the
    // index template.
    const uuid = await openSession(page);
    await page.reload();
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });

    // The data-* attributes flow from the index template into <terminal-ui>.
    const terminalUi = page.locator('terminal-ui');
    await expect(terminalUi).toHaveAttribute('data-local-user-name', 'E2E Test');
    await expect(terminalUi).toHaveAttribute('data-local-user-email', 'e2e@test.local');

    await openSettings(page);
    await switchSettingsTab(page, 'git');

    // populateCredentialsSection sets the values from local config and
    // marks the inputs readonly.
    const nameInput = page.locator('#settings-cred-name');
    const emailInput = page.locator('#settings-cred-email');
    await expect(nameInput).toHaveValue('E2E Test');
    await expect(emailInput).toHaveValue('e2e@test.local');
    await expect(nameInput).toHaveAttribute('readonly', '');
    await expect(emailInput).toHaveAttribute('readonly', '');

    // Explainer is inserted; mention .git/config so the user knows where
    // the override comes from.
    const explainer = page.locator('#settings-cred-local-override');
    await expect(explainer).toBeVisible();
    await expect(explainer).toContainText('.git/config');
  });
});

// Test ed25519 key generated for e2e only; never used outside this suite.
// Fingerprint: SHA256:YNOCH+zR5nOPiv90YpfKRkXMeHPseO6WdGegIzLmj+U
const TEST_SIGNING_KEY_PEM = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACA/lEFqfZd6xhkn6HW5K7QjLEQFkV34Tq9W0wIAnx0ogQAAAJghU4NbIVOD
WwAAAAtzc2gtZWQyNTUxOQAAACA/lEFqfZd6xhkn6HW5K7QjLEQFkV34Tq9W0wIAnx0ogQ
AAAEC0jJWbHEAJ8zCd60hZS5xa43xt1Qh3bkj7PQQRYXxRFT+UQWp9l3rGGSfodbkrtCMs
RAWRXfhOr1bTAgCfHSiBAAAAEGUyZS10ZXN0QHN3ZS1zd2UBAgMEBQ==
-----END OPENSSH PRIVATE KEY-----
`;

const TEST_SIGNING_FINGERPRINT = 'SHA256:YNOCH+zR5nOPiv90YpfKRkXMeHPseO6WdGegIzLmj+U';

test.describe('per-session SSH commit signing UI', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('save round-trip: signing form -> WS -> credentials_stored ack with fingerprint -> status + localStorage', async ({ page }) => {
    await openSession(page);

    await page.evaluate(() => {
      Object.keys(localStorage)
        .filter(k => k.startsWith('swe-swe-creds:') || k.startsWith('swe-swe-signing:'))
        .forEach(k => localStorage.removeItem(k));
      if (window.terminalUI) {
        window.terminalUI._credsStoredHosts = [];
        window.terminalUI._signingFingerprint = '';
      }
    });

    await openSettings(page);
    await switchSettingsTab(page, 'ssh');

    // Signing section is rendered in the SSH commit-signing tab.
    const signingSection = page.locator('.settings-panel__pane[data-pane="ssh"]');
    await expect(signingSection).toBeVisible();

    // Fields the form must expose. Test-first: this fails until the
    // implementation lands the matching ids.
    const keyTextarea = page.locator('#settings-cred-signing-key');
    const passphraseInput = page.locator('#settings-cred-signing-passphrase');
    const labelInput = page.locator('#settings-cred-signing-label');
    await expect(keyTextarea).toBeVisible();
    await expect(passphraseInput).toBeVisible();
    await expect(labelInput).toBeVisible();
    await expect(passphraseInput).toHaveAttribute('type', 'password');

    // Phase A split signing onto its own Save button + set_signing_key WS
    // message, so we no longer need to fill an HTTPS token to satisfy the
    // unified _saveCredentials path.
    await page.fill('#settings-cred-signing-key', TEST_SIGNING_KEY_PEM);
    await page.fill('#settings-cred-signing-label', 'e2e-signing-key');

    // Save key. Triggers set_signing_key over the WS.
    await page.click('#settings-cred-signing-save');

    // Wait for the signing_key_stored ack to populate the fingerprint.
    await waitForUi(page, () => {
      const ui = window.terminalUI;
      return typeof ui?._signingFingerprint === 'string' && ui._signingFingerprint.startsWith('SHA256:');
    });

    const got = await page.evaluate(() => window.terminalUI._signingFingerprint);
    expect(got).toBe(TEST_SIGNING_FINGERPRINT);

    // Status reflects the registered key fingerprint somewhere visible
    // to the user.
    const signingStatus = page.locator('#settings-cred-signing-status');
    await expect(signingStatus).toContainText('SHA256:');

    // localStorage round-trip: the key + label persist; passphrase
    // does NOT persist (consumed once on save, not retained).
    const stored = await page.evaluate(() => {
      const raw = localStorage.getItem('swe-swe-signing:default');
      return raw ? JSON.parse(raw) : null;
    });
    expect(stored).toMatchObject({
      privateKey: TEST_SIGNING_KEY_PEM,
      label: 'e2e-signing-key',
    });
    expect(stored).not.toHaveProperty('passphrase');

    // Close + re-open: key + label rehydrate from localStorage.
    await closeSettings(page);
    await openSettings(page);
    await switchSettingsTab(page, 'ssh');
    await expect(page.locator('#settings-cred-signing-key')).toHaveValue(TEST_SIGNING_KEY_PEM);
    await expect(page.locator('#settings-cred-signing-label')).toHaveValue('e2e-signing-key');
    // Passphrase is NEVER rehydrated -- it isn't in localStorage to
    // rehydrate from.
    await expect(page.locator('#settings-cred-signing-passphrase')).toHaveValue('');
  });
});
