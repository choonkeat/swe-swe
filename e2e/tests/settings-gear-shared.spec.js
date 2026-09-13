import { test, expect } from './_helpers/reaper.js';
import { openSessionViaPost } from './_helpers/sessions.js';

// One settings button, two pages. The session header used to carry a bare text
// glyph and no tunnel state at all, while the homepage carried the drawn gear
// and the colour; both now render the same <settings-gear> component, so this
// spec asserts they really are the same and that the session copy is live.
//
// tunnel-gear.spec.js owns the per-state colour matrix on the homepage. This
// one asserts the session page is wired to the same component -- one connected
// state is enough, since the colours come from the one stylesheet both load.

const CONNECTED = { dark: 'rgb(34, 197, 94)', light: 'rgb(22, 163, 74)' };

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

async function connectedColor(page) {
  const theme = await page.evaluate(() => document.documentElement.getAttribute('data-theme'));
  return CONNECTED[theme === 'light' ? 'light' : 'dark'];
}

function gear(page) {
  return page.locator('settings-gear button').first();
}

async function openSession(page) {
  // A plain bash PTY boots instantly; this spec is about the header, not the
  // agent.
  await openSessionViaPost(page, { assistant: 'shell', session: 'terminal' });
  await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
}

test.describe('the settings gear is one component on both pages', () => {
  test('the session header draws the same gear as the homepage, not a text glyph', async ({ page }) => {
    await page.goto('/');
    await expect(gear(page)).toBeVisible();
    const homepageIcon = await gear(page).innerHTML();
    // A drawn icon, so a font without the gear codepoint cannot turn it into
    // tofu -- which is what the session page's old "gear" character risked.
    expect(homepageIcon).toContain('<svg');

    await openSession(page);
    await expect(gear(page)).toBeVisible();
    expect(await gear(page).innerHTML()).toBe(homepageIcon);
    await expect(gear(page)).toHaveAttribute('title', 'Settings');
  });

  test('the session gear takes the tunnel colour too, and still opens Session Settings', async ({ page }) => {
    await stubTunnel(page, {
      configured: true,
      state: 'connected',
      url: 'https://1977.my-box-tunnel.example.com',
      serverUrl: 'https://tunnel.example.com',
      unique: 'my-box',
    });
    await openSession(page);

    await expect(gear(page)).toHaveClass(/settings-gear--tunnel-connected/);
    // The gear transitions over 0.2s, so a single read can land part-way
    // between grey and green.
    const want = await connectedColor(page);
    await expect
      .poll(() => gear(page).evaluate((el) => getComputedStyle(el).color), { timeout: 5_000 })
      .toBe(want);
    await expect(gear(page)).toHaveAttribute('title', 'Settings - Tunnel connected');

    // The colour must not eat the click the button is there for.
    await gear(page).click();
    await page.locator('.settings-panel:not([hidden])').waitFor({ timeout: 5_000 });
  });
});
