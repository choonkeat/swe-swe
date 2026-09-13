import { test, expect } from '@playwright/test';

// The tunnel status dot on the homepage settings gear, and the public address
// row it replaced the top-of-page strip with.
//
// Opens no sessions, so it imports from '@playwright/test' rather than the
// reaper fixture -- there is nothing to reap and no reason to end sessions a
// neighbouring spec may still be using.
//
// The unconfigured case runs against the real server response (the e2e stack
// boots with no tunnel, so /api/server/tunnel really does answer
// configured:false). The four live states cannot be produced for real -- there
// is no tunnel server to connect to -- so they stub that one JSON endpoint and
// assert what the page renders from it. The state machine that fills the
// endpoint in is covered by tunnel_runtime_test.go.

const GREEN = 'rgb(34, 197, 94)';
const AMBER = 'rgb(245, 158, 11)';
const RED = 'rgb(239, 68, 68)';

async function stubTunnel(page, body) {
  await page.route('**/api/server/tunnel', async (route) => {
    if (route.request().method() !== 'GET') return route.continue();
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(body),
    });
  });
}

// The dot is created by the first poll, which fires on DOMContentLoaded.
function dot(page) {
  return page.locator('#settings-btn .header__settings-dot');
}

async function dotColor(page) {
  return dot(page).evaluate((el) => getComputedStyle(el).backgroundColor);
}

async function openSettings(page) {
  await page.locator('#settings-btn').click();
  await expect(page.locator('#settings-dialog-overlay')).toBeVisible();
}

test.describe('tunnel status dot on the settings gear', () => {
  test('no tunnel configured: no dot at all, plain gear tooltip, no address row', async ({ page }) => {
    // No stub: this is the real server, which boots with no tunnel.
    const status = await page.request.get('/api/server/tunnel');
    expect(status.ok()).toBeTruthy();
    expect((await status.json()).configured).toBe(false);

    await page.goto('/');
    await expect(page.locator('#settings-btn')).toBeVisible();
    // Give the first poll room to have created a dot if it were going to.
    await page.waitForTimeout(1500);

    await expect(dot(page)).toHaveCount(0);
    await expect(page.locator('#settings-btn')).toHaveAttribute('title', 'Settings');

    await openSettings(page);
    await expect(page.locator('#server-tunnel-url')).toBeHidden();
  });

  test('connected: green dot, and the address moves into the Settings pane with Copy', async ({ page }) => {
    await stubTunnel(page, {
      configured: true,
      state: 'connected',
      url: 'https://1977.my-box-tunnel.example.com',
      serverUrl: 'https://tunnel.example.com',
      unique: 'my-box',
    });
    await page.goto('/');

    await expect(dot(page)).toBeVisible();
    expect(await dotColor(page)).toBe(GREEN);
    await expect(dot(page)).toHaveAttribute('title', 'Tunnel connected');
    await expect(page.locator('#settings-btn')).toHaveAttribute('title', 'Settings - Tunnel connected');

    await openSettings(page);
    await expect(page.locator('#server-tunnel-url')).toBeVisible();
    await expect(page.locator('#server-tunnel-state')).toHaveText('Connected:');
    const link = page.locator('#server-tunnel-link');
    await expect(link).toHaveText('https://1977.my-box-tunnel.example.com');
    await expect(link).toHaveAttribute('href', 'https://1977.my-box-tunnel.example.com');
    await expect(page.locator('#server-tunnel-copy')).toBeVisible();
    await expect(page.locator('#server-tunnel-detail')).toBeHidden();

    // The old strip is gone -- assert it, or a stray copy could come back
    // unnoticed alongside the dot.
    await expect(page.locator('#server-tunnel-strip, .tunnel-strip')).toHaveCount(0);
  });

  // Read the clipboard by pasting into a scratch input rather than through
  // navigator.clipboard.readText(): the suite reaches the server over plain
  // http on a non-localhost host, which is not a secure context, so the async
  // clipboard API is absent -- the same condition a real box is in before its
  // tunnel is up. Pasting exercises whichever route the page actually took.
  async function readClipboardByPaste(page) {
    await page.evaluate(() => {
      const el = document.createElement('input');
      el.id = 'e2e-paste-target';
      el.style.position = 'fixed';
      el.style.top = '0';
      document.body.appendChild(el);
      el.focus();
    });
    await page.keyboard.press('Control+V');
    const value = await page.inputValue('#e2e-paste-target');
    await page.evaluate(() => document.getElementById('e2e-paste-target').remove());
    return value;
  }

  test('connected: Copy puts the address on the clipboard over plain http too', async ({ page }) => {
    await stubTunnel(page, {
      configured: true,
      state: 'connected',
      url: 'https://1977.my-box-tunnel.example.com',
      serverUrl: 'https://tunnel.example.com',
      unique: 'my-box',
    });
    await page.goto('/');
    // Guard the premise: if this ever became a secure context the fallback
    // path would stop being exercised and nobody would notice.
    expect(await page.evaluate(() => window.isSecureContext)).toBe(false);
    await openSettings(page);

    const copy = page.locator('#server-tunnel-copy');
    await copy.click();
    await expect(copy).toHaveText('Copied');
    expect(await readClipboardByPaste(page)).toBe('https://1977.my-box-tunnel.example.com');
    // Reverts, so a second copy is obviously a second copy.
    await expect(copy).toHaveText('Copy', { timeout: 5_000 });
  });

  test('connecting: amber dot, pulsing, and the pane says the address is not there yet', async ({ page }) => {
    await stubTunnel(page, {
      configured: true,
      state: 'connecting',
      serverUrl: 'https://tunnel.example.com',
      unique: 'my-box',
    });
    await page.goto('/');

    await expect(dot(page)).toBeVisible();
    expect(await dotColor(page)).toBe(AMBER);
    // A still amber dot reads the same as a stuck one, so the pulse is part of
    // the design, not decoration.
    expect(await dot(page).evaluate((el) => getComputedStyle(el).animationName))
      .toBe('tunnel-dot-pulse');
    await expect(dot(page)).toHaveAttribute(
      'title',
      'Tunnel connecting to https://tunnel.example.com as my-box...',
    );

    await openSettings(page);
    await expect(page.locator('#server-tunnel-link')).toBeHidden();
    await expect(page.locator('#server-tunnel-copy')).toBeHidden();
    await expect(page.locator('#server-tunnel-detail'))
      .toHaveText('The public address appears here once the tunnel connects.');
  });

  test('reconnecting: amber dot naming the countdown', async ({ page }) => {
    await stubTunnel(page, {
      configured: true,
      state: 'reconnecting',
      serverUrl: 'https://tunnel.example.com',
      unique: 'my-box',
      reason: 'dial tcp: connection refused',
      retryAfterMs: 12000,
    });
    await page.goto('/');

    expect(await dotColor(page)).toBe(AMBER);
    await expect(dot(page)).toHaveAttribute(
      'title',
      'Tunnel reconnecting to https://tunnel.example.com (dial tcp: connection refused) in 12s',
    );
  });

  for (const state of ['error', 'fatal', 'disconnected']) {
    test(`${state}: red dot carrying the reason and what to do`, async ({ page }) => {
      await stubTunnel(page, {
        configured: true,
        state,
        serverUrl: 'https://tunnel.example.com',
        unique: 'my-box',
        reason: 'identity rejected',
      });
      await page.goto('/');

      expect(await dotColor(page)).toBe(RED);
      const words = `Tunnel ${state} (identity rejected). Fix the Tunnel secrets below and Apply again.`;
      await expect(dot(page)).toHaveAttribute('title', words);
      await expect(page.locator('#settings-btn')).toHaveAttribute('title', `Settings - ${words}`);

      await openSettings(page);
      await expect(page.locator('#server-tunnel-state')).toHaveText(words);
      await expect(page.locator('#server-tunnel-link')).toBeHidden();
    });
  }

  test('the dot does not swallow the click: hitting it still opens Settings', async ({ page }) => {
    await stubTunnel(page, {
      configured: true,
      state: 'connected',
      url: 'https://1977.my-box-tunnel.example.com',
      serverUrl: 'https://tunnel.example.com',
      unique: 'my-box',
    });
    await page.goto('/');
    await expect(dot(page)).toBeVisible();

    await dot(page).click();
    await expect(page.locator('#settings-dialog-overlay')).toBeVisible();
  });

  test('the dot follows the state without a reload', async ({ page }) => {
    let state = 'connected';
    await page.route('**/api/server/tunnel', async (route) => {
      if (route.request().method() !== 'GET') return route.continue();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          configured: true,
          state,
          url: state === 'connected' ? 'https://1977.my-box-tunnel.example.com' : '',
          serverUrl: 'https://tunnel.example.com',
          unique: 'my-box',
          reason: state === 'connected' ? '' : 'identity rejected',
        }),
      });
    });
    await page.goto('/');
    await expect(dot(page)).toBeVisible();
    expect(await dotColor(page)).toBe(GREEN);

    // The poll runs every 3s; the next one must repaint without a navigation.
    state = 'error';
    await expect
      .poll(() => dotColor(page), { timeout: 15_000 })
      .toBe(RED);

    // ...and the pane it feeds drops the now-stale address.
    await openSettings(page);
    await expect(page.locator('#server-tunnel-link')).toBeHidden();
    await expect(page.locator('#server-tunnel-detail')).toBeVisible();
  });
});
