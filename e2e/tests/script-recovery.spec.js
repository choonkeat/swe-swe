// index.html loads xterm.js, its fit addon, link-provider.js and
// end-session.js as classic scripts before terminal-ui.js runs as a module.
// A phone waking a backgrounded tab reloads the whole page while the network
// is still coming up, and one of those requests can fail on its own. Before
// the recovery in terminal-ui.js, that killed the session page with a raw
// "FitAddon is not defined" stack trace over the whole viewport.
import { test, expect } from './_helpers/reaper.js';
import { openSessionViaPost, endSessions } from './_helpers/sessions.js';

const FIT_ADDON = '**/xterm-addon-fit.js*';

test.describe('missing-script recovery', () => {
  test('a failed script download is refetched and the session still boots', async ({ page }) => {
    let aborted = 0;
    let retried = 0;
    // Fail the request the page makes on its own; let the refetch (which
    // carries the retry marker) through.
    await page.route(FIT_ADDON, (route) => {
      if (route.request().url().includes('retry=')) {
        retried++;
        return route.continue();
      }
      aborted++;
      return route.abort('failed');
    });

    const uuid = await openSessionViaPost(page, { assistant: 'shell' });
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });

    expect(aborted).toBe(1);
    expect(retried).toBeGreaterThan(0);
    // The addon really is present, and the terminal really was sized by it.
    expect(await page.evaluate(() => typeof window.FitAddon)).not.toBe('undefined');
    expect(await page.evaluate(() => window.terminalUI.term.cols)).toBeGreaterThan(0);
    await expect(page.locator('.swe-startup-card')).toHaveCount(0);

    await endSessions(page, [uuid]);
  });

  test('an unrecoverable download shows a Reload card, not a stack trace', async ({ page }) => {
    await page.route(FIT_ADDON, (route) => route.abort('failed'));

    const uuid = await openSessionViaPost(page, { assistant: 'shell' });

    const card = page.locator('.swe-startup-card');
    // The retry card has no button; waiting on the button waits for the
    // final, give-up state.
    await card.locator('button').waitFor({ timeout: 60_000 });
    await expect(card).toContainText('Something did not load');
    await expect(card.locator('button')).toHaveText('Reload');
    expect(await page.locator('body').innerText()).not.toMatch(/at .*terminal-ui\.js/);

    await Promise.all([
      page.waitForLoadState('load'),
      card.locator('button').click(),
    ]);

    await endSessions(page, [uuid]).catch(() => {});
  });
});
