import { test, expect } from '@playwright/test';
import { openSessionViaPost } from './_helpers/sessions.js';

// Auth cookie comes from the suite-wide storageState (see playwright.config.js
// + global-setup.js); no per-test login is needed.

async function waitForUi(page, predicate) {
  return page.waitForFunction(predicate, null, { timeout: 60_000 });
}

async function openSession(page) {
  const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'terminal' });
  await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
  // set_env is silently dropped if the socket isn't OPEN yet; wait until the
  // sessionUUID has been delivered (proxy for "WS init round-trip completed").
  await waitForUi(page, () => window.terminalUI && window.terminalUI.sessionUUID);
  return uuid;
}

async function openSettings(page) {
  await page.locator('.terminal-ui__settings-btn').first().click();
  await page.locator('.settings-panel:not([hidden])').waitFor({ timeout: 5_000 });
}

async function switchSettingsTab(page, tab) {
  await page.locator(`.settings-panel__nav-item[data-tab="${tab}"]`).click();
  await page.locator(`.settings-panel__pane[data-pane="${tab}"]:not([hidden])`).waitFor({ timeout: 5_000 });
}

async function closeSettings(page) {
  await page.locator('.settings-panel__close').first().click();
  await page.locator('.settings-panel').waitFor({ state: 'hidden', timeout: 5_000 });
}

// The localStorage key the repo env-vars blob is stored under: keyed by
// (origin, init_sha) so it auto-syncs only for the matching repo, reusing the
// same trust scope as the HTTPS PAT.
async function envLocalKey(page) {
  return page.evaluate(() => {
    const initSha = document.querySelector('terminal-ui')?.dataset?.initSha || '';
    return 'swe-swe-env:' + window.location.origin + '|' + initSha;
  });
}

test.describe('per-repo environment variables UI', () => {
  test('save round-trip: textarea -> WS -> env_stored ack -> status + localStorage', async ({ page }) => {
    await openSession(page);

    await page.evaluate(() => {
      Object.keys(localStorage)
        .filter(k => k.startsWith('swe-swe-env:'))
        .forEach(k => localStorage.removeItem(k));
      if (window.terminalUI) window.terminalUI._envCount = 0;
    });

    await openSettings(page);
    await switchSettingsTab(page, 'env');

    const pane = page.locator('.settings-panel__pane[data-pane="env"]');
    await expect(pane).toBeVisible();

    // Fresh repo (no env vars saved before): the textarea is shown directly
    // (no mask) and is BLANK, Save is available.
    const textarea = page.locator('#settings-env-vars');
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('');
    await expect(page.locator('#settings-env-masked')).toBeHidden();
    await expect(page.locator('#settings-env-save')).toBeVisible();

    const blob = 'OPENAI_API_KEY=sk-test-123\nFEATURE_X=1\n';
    await textarea.fill(blob);

    // Record every status-text change so we can assert the transient
    // "Sending..." without racing the ack (mirrors the creds spec).
    await page.evaluate(() => {
      window.__envStatusHistory = [];
      const el = document.getElementById('settings-env-status');
      window.__envStatusObserver = new MutationObserver(() => {
        window.__envStatusHistory.push(el.textContent);
      });
      window.__envStatusObserver.observe(el, { childList: true, subtree: true, characterData: true });
    });

    await page.click('#settings-env-save');

    // Wait for the env_stored ack.
    await waitForUi(page, () => window.terminalUI && window.terminalUI._envStored === true);

    const status = page.locator('#settings-env-status');
    await expect(status).toHaveAttribute('data-state', 'ok');
    // The saved confirmation states the spawn-time semantics: these apply to
    // the next new session, not the running one.
    await expect(status).toContainText(/next session/i);

    const statusHistory = await page.evaluate(() => {
      window.__envStatusObserver?.disconnect();
      return window.__envStatusHistory || [];
    });
    expect(statusHistory.some(t => /Sending\.\.\./.test(t))).toBe(true);

    // localStorage round-trip: the raw blob is stored verbatim under the
    // (origin, init_sha) key. No server response is stored here.
    const key = await envLocalKey(page);
    const stored = await page.evaluate((k) => localStorage.getItem(k), key);
    expect(stored).toBe(blob);

    // Nav badge shows the count (2 vars).
    const badge = page.locator('#settings-nav-badge-env');
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText('2');
  });

  test('reveal-to-edit: reopened pane masks values until the user clicks reveal', async ({ page }) => {
    await openSession(page);

    const blob = 'SECRET_TOKEN=abc123\nDATABASE_URL=postgres://x\n';
    const key = await envLocalKey(page);
    await page.evaluate(({ k, v }) => {
      Object.keys(localStorage).filter(x => x.startsWith('swe-swe-env:')).forEach(x => localStorage.removeItem(x));
      localStorage.setItem(k, v);
    }, { k: key, v: blob });

    // Reload = a fresh session visit to a repo that already has env vars saved
    // for this (origin, init_sha) on this browser.
    await page.reload();
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
    await waitForUi(page, () => window.terminalUI && window.terminalUI.sessionUUID);

    await openSettings(page);
    await switchSettingsTab(page, 'env');

    // Masked: the textarea is hidden and does NOT contain the secret values;
    // a summary + Reveal button stand in for it.
    await expect(page.locator('#settings-env-vars')).toBeHidden();
    const masked = page.locator('#settings-env-masked');
    await expect(masked).toBeVisible();
    await expect(masked).toContainText('2 variables saved on this device');
    // While masked, the raw values must not be sitting in the textarea's value.
    expect(await page.locator('#settings-env-vars').inputValue()).toBe('');
    // Save is hidden while masked; the only affordance is Reveal.
    await expect(page.locator('#settings-env-save')).toBeHidden();

    // Reveal -> textarea appears with the stored blob, Save returns.
    await page.click('#settings-env-reveal');
    await expect(page.locator('#settings-env-vars')).toBeVisible();
    await expect(page.locator('#settings-env-vars')).toHaveValue(blob);
    await expect(page.locator('#settings-env-masked')).toBeHidden();
    await expect(page.locator('#settings-env-save')).toBeVisible();
  });

  test('reserved keys are reported as ignored after save', async ({ page }) => {
    await openSession(page);
    await page.evaluate(() => {
      Object.keys(localStorage).filter(k => k.startsWith('swe-swe-env:')).forEach(k => localStorage.removeItem(k));
      if (window.terminalUI) window.terminalUI._envStored = false;
    });

    await openSettings(page);
    await switchSettingsTab(page, 'env');

    await page.locator('#settings-env-vars').fill('PATH=/evil\nGH_TOKEN=stolen\nAPP_ENV=prod\n');
    await page.click('#settings-env-save');
    await waitForUi(page, () => window.terminalUI && window.terminalUI._envStored === true);

    const dropped = page.locator('#settings-env-dropped');
    await expect(dropped).toBeVisible();
    await expect(dropped).toContainText('PATH');
    await expect(dropped).toContainText('GH_TOKEN');
    // The kept var counts toward the badge; the reserved ones do not.
    await expect(page.locator('#settings-nav-badge-env')).toHaveText('1');
  });

  test('forget on this device clears the stored blob and the server copy', async ({ page }) => {
    await openSession(page);
    const key = await envLocalKey(page);
    await page.evaluate(({ k }) => {
      Object.keys(localStorage).filter(x => x.startsWith('swe-swe-env:')).forEach(x => localStorage.removeItem(x));
      localStorage.setItem(k, 'FOO=bar\n');
      if (window.terminalUI) window.terminalUI._envCount = 1;
    }, { k: key });

    await openSettings(page);
    await switchSettingsTab(page, 'env');

    const forget = page.locator('#settings-env-forget');
    await expect(forget).toBeVisible();
    await forget.click();

    await waitForUi(page, () => window.terminalUI && window.terminalUI._envCleared === true);

    const after = await page.evaluate((k) => localStorage.getItem(k), key);
    expect(after).toBeNull();
    await expect(page.locator('#settings-nav-badge-env')).toBeHidden();
  });
});
