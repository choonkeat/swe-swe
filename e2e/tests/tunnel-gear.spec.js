import { test, expect } from '@playwright/test';

// The tunnel status colour on the homepage settings gear, and the public
// address row it replaced the top-of-page strip with.
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

// The gear carries the state as its own colour: a coloured dot in the corner
// of an icon reads as unread notifications. Both themes are listed because the
// mid greens are too pale on white, and the suite must not care which theme
// the browser happens to be in.
const COLORS = {
  connected: { dark: 'rgb(34, 197, 94)', light: 'rgb(22, 163, 74)' },
  amber: { dark: 'rgb(245, 158, 11)', light: 'rgb(217, 119, 6)' },
  red: { dark: 'rgb(239, 68, 68)', light: 'rgb(239, 68, 68)' },
};

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

// The tint, the tooltip and the click all land on the button that
// <settings-gear> renders inside itself; #settings-btn is only the host the
// homepage header positions.
function gear(page) {
  return page.locator('#settings-btn button');
}

async function gearColor(page) {
  return gear(page).evaluate((el) => getComputedStyle(el).color);
}

// The gear carries `transition: all 0.2s ease`, so a colour read the instant
// the class lands catches a value part-way between grey and the state colour.
// Poll until it settles rather than asserting once.
async function expectGearColor(page, family) {
  const want = await expected(page, family);
  await expect.poll(() => gearColor(page), { timeout: 5_000 }).toBe(want);
}

// The theme is whatever the browser profile last stored, so resolve the
// expected colour against the theme actually in force rather than pinning one.
async function expected(page, family) {
  const theme = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
  return COLORS[family][theme === 'light' ? 'light' : 'dark'];
}

async function openSettings(page) {
  await gear(page).click();
  await expect(page.locator('#settings-dialog-overlay')).toBeVisible();
}

test.describe('tunnel status colour on the settings gear', () => {
  test('no tunnel configured: plain gear, plain tooltip, no address row', async ({ page }) => {
    // No stub: this is the real server, which boots with no tunnel.
    const status = await page.request.get('/api/server/tunnel');
    expect(status.ok()).toBeTruthy();
    expect((await status.json()).configured).toBe(false);

    await page.goto('/');
    await expect(gear(page)).toBeVisible();
    // Give the first poll room to have tinted the gear if it were going to.
    await page.waitForTimeout(1500);

    expect(await gear(page).evaluate((el) => el.className)).toBe('settings-gear');
    await expect(gear(page)).toHaveAttribute('title', 'Settings');
    // The state colours must be absent, not merely unnoticeable.
    expect(await gearColor(page)).not.toBe(await expected(page, 'connected'));
    expect(await gearColor(page)).not.toBe(await expected(page, 'red'));

    await openSettings(page);
    await expect(page.locator('#server-tunnel-url')).toBeHidden();
  });

  test('connected: green gear, and the address moves into the Settings pane with Copy', async ({ page }) => {
    await stubTunnel(page, {
      configured: true,
      state: 'connected',
      url: 'https://1977.my-box-tunnel.example.com',
      serverUrl: 'https://tunnel.example.com',
      unique: 'my-box',
    });
    await page.goto('/');

    await expect(gear(page)).toHaveClass(/settings-gear--tunnel-connected/);
    await expectGearColor(page, 'connected');
    await expect(gear(page)).toHaveAttribute('title', 'Settings - Tunnel connected');

    await openSettings(page);
    await expect(page.locator('#server-tunnel-url')).toBeVisible();
    await expect(page.locator('#server-tunnel-state')).toHaveText('Connected:');
    const link = page.locator('#server-tunnel-link');
    await expect(link).toHaveText('https://1977.my-box-tunnel.example.com');
    await expect(link).toHaveAttribute('href', 'https://1977.my-box-tunnel.example.com');
    await expect(page.locator('#server-tunnel-copy')).toBeVisible();
    await expect(page.locator('#server-tunnel-detail')).toBeHidden();

    // The old strip is gone -- assert it, or a stray copy could come back
    // unnoticed alongside the tinted gear.
    await expect(page.locator('#server-tunnel-strip, .tunnel-strip')).toHaveCount(0);
    // ...and so is the badge the tint replaced.
    await expect(page.locator('#server-tunnel-dot, .settings-gear-dot')).toHaveCount(0);
  });

  test('connected: the tint survives hover, which sets a colour of its own', async ({ page }) => {
    await stubTunnel(page, {
      configured: true,
      state: 'connected',
      url: 'https://1977.my-box-tunnel.example.com',
      serverUrl: 'https://tunnel.example.com',
      unique: 'my-box',
    });
    await page.goto('/');
    await expect(gear(page)).toHaveClass(/settings-gear--tunnel-connected/);

    await gear(page).hover();
    await expectGearColor(page, 'connected');
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

  test('connecting: amber gear, pulsing, and the pane says the address is not there yet', async ({ page }) => {
    await stubTunnel(page, {
      configured: true,
      state: 'connecting',
      serverUrl: 'https://tunnel.example.com',
      unique: 'my-box',
    });
    await page.goto('/');

    await expect(gear(page)).toHaveClass(/settings-gear--tunnel-connecting/);
    await expectGearColor(page, 'amber');
    // A still amber gear reads the same as a stuck one, so the pulse is part
    // of the design, not decoration.
    expect(await gear(page).locator('svg').evaluate((el) => getComputedStyle(el).animationName))
      .toBe('tunnel-gear-pulse');
    await expect(gear(page)).toHaveAttribute(
      'title',
      'Settings - Tunnel connecting to https://tunnel.example.com as my-box...',
    );

    await openSettings(page);
    await expect(page.locator('#server-tunnel-link')).toBeHidden();
    await expect(page.locator('#server-tunnel-copy')).toBeHidden();
    await expect(page.locator('#server-tunnel-detail'))
      .toHaveText('The public address appears here once the tunnel connects.');
  });

  test('reconnecting: amber gear naming the countdown', async ({ page }) => {
    await stubTunnel(page, {
      configured: true,
      state: 'reconnecting',
      serverUrl: 'https://tunnel.example.com',
      unique: 'my-box',
      reason: 'dial tcp: connection refused',
      retryAfterMs: 12000,
    });
    await page.goto('/');

    await expect(gear(page)).toHaveClass(/settings-gear--tunnel-reconnecting/);
    await expectGearColor(page, 'amber');
    await expect(gear(page)).toHaveAttribute(
      'title',
      'Settings - Tunnel reconnecting to https://tunnel.example.com (dial tcp: connection refused) in 12s',
    );
  });

  for (const state of ['error', 'fatal', 'disconnected']) {
    test(`${state}: red gear carrying the reason and what to do`, async ({ page }) => {
      await stubTunnel(page, {
        configured: true,
        state,
        serverUrl: 'https://tunnel.example.com',
        unique: 'my-box',
        reason: 'identity rejected',
      });
      await page.goto('/');

      await expect(gear(page)).toHaveClass(new RegExp(`settings-gear--tunnel-${state}`));
      await expectGearColor(page, 'red');
      const words = `Tunnel ${state} (identity rejected). Fix the Tunnel secrets below and Apply again.`;
      await expect(gear(page)).toHaveAttribute('title', `Settings - ${words}`);

      await openSettings(page);
      await expect(page.locator('#server-tunnel-state')).toHaveText(words);
      await expect(page.locator('#server-tunnel-link')).toBeHidden();
    });
  }

  test('the tint does not swallow the click: the gear still opens Settings', async ({ page }) => {
    await stubTunnel(page, {
      configured: true,
      state: 'connected',
      url: 'https://1977.my-box-tunnel.example.com',
      serverUrl: 'https://tunnel.example.com',
      unique: 'my-box',
    });
    await page.goto('/');
    await expect(gear(page)).toHaveClass(/settings-gear--tunnel-connected/);

    await gear(page).click();
    await expect(page.locator('#settings-dialog-overlay')).toBeVisible();
  });

  test('the gear follows the state without a reload, and drops the tint when the tunnel goes away', async ({ page }) => {
    let state = 'connected';
    let configured = true;
    await page.route('**/api/server/tunnel', async (route) => {
      if (route.request().method() !== 'GET') return route.continue();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          configured,
          state,
          url: state === 'connected' ? 'https://1977.my-box-tunnel.example.com' : '',
          serverUrl: 'https://tunnel.example.com',
          unique: 'my-box',
          reason: state === 'connected' ? '' : 'identity rejected',
        }),
      });
    });
    await page.goto('/');
    await expectGearColor(page, 'connected');

    // The poll runs every 3s; the next one must repaint without a navigation.
    state = 'error';
    const red = await expected(page, 'red');
    await expect.poll(() => gearColor(page), { timeout: 15_000 }).toBe(red);

    // ...and the pane it feeds drops the now-stale address.
    await openSettings(page);
    await expect(page.locator('#server-tunnel-link')).toBeHidden();
    await expect(page.locator('#server-tunnel-detail')).toBeVisible();
    await page.locator('#settings-close').click();

    // Unconfiguring must strip the class, not leave the last colour stuck on.
    configured = false;
    await expect
      .poll(() => gear(page).evaluate((el) => el.className), { timeout: 15_000 })
      .toBe('settings-gear');
    await expect(gear(page)).toHaveAttribute('title', 'Settings');
  });
});
