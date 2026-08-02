import { test, expect } from './_helpers/reaper.js';
import { endSessions, openSessionViaPost } from './_helpers/sessions.js';

// Regression cover for tasks/2026-08-02-ipad-input-bar-below-fold.md.
//
// da301a045 + 17dcef13b sized the whole app from --app-viewport-height, a
// number TerminalUI.syncVisibleViewport() publishes from window.visualViewport.
// It is used verbatim (the only guard is `height <= 0`) and is only rewritten
// when Safari delivers a viewport event. When Safari drops the final settling
// event the stale -- larger -- value stays published, the app paints taller
// than the screen, and because html/body are overflow:hidden and terminal-ui is
// position:fixed the overhanging bottom strip -- which holds the agent-chat
// input bar and Send button -- cannot be scrolled into view. Only a reload
// clears it.
//
// We cannot make Chromium go stale the way Safari does, so the test forces the
// state directly: publish a height 200px taller than the window and assert the
// app is still bounded by the window. The CSS ceiling (min(..., 100dvh)) is
// what makes that true, and 100dvh cannot go stale because no JS writes it.

const OVERSHOOT_PX = 200;

let testSessions = [];

async function waitForUi(page, predicate) {
  return page.waitForFunction(predicate, null, { timeout: 90_000 });
}

async function openChatSession(page) {
  const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'chat' });
  testSessions.push(uuid);
  await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
  return uuid;
}

test.describe('visible-viewport height ceiling', () => {
  test.beforeEach(async () => {
    testSessions = [];
  });

  test.afterEach(async ({ page }, testInfo) => {
    if (testInfo.status === 'passed' && testSessions.length > 0) {
      await endSessions(page, testSessions);
    }
  });

  test('a stale-tall --app-viewport-height cannot push the app below the fold', async ({ page }) => {
    await openChatSession(page);
    await waitForUi(page, () => window.terminalUI?._agentChatAvailable === true);

    const measured = await page.evaluate((overshoot) => {
      const root = document.documentElement;
      // Reproduce the stale-tall publish. Offset stays 0: the reported failure
      // is height-only (the app's top is correct in both of the reporter's
      // screenshots).
      root.style.setProperty('--app-viewport-offset', '0px');
      root.style.setProperty('--app-viewport-height', `${window.innerHeight + overshoot}px`);
      // Keep syncVisibleViewport() from immediately rewriting our value if an
      // incidental viewport event fires between the write and the measurement.
      window.terminalUI.syncVisibleViewport = () => {};

      const app = document.querySelector('terminal-ui');
      const paneHost = document.querySelector('.terminal-ui__pane-host[data-pane="agent-chat"]');
      return {
        innerHeight: window.innerHeight,
        appBottom: Math.round(app.getBoundingClientRect().bottom),
        paneBottom: paneHost ? Math.round(paneHost.getBoundingClientRect().bottom) : null,
        bodyHeight: Math.round(document.body.getBoundingClientRect().height),
      };
    }, OVERSHOOT_PX);

    // 1px of slack for subpixel rounding of dvh against innerHeight.
    expect(measured.appBottom,
      `terminal-ui bottom ${measured.appBottom} overhangs the ${measured.innerHeight}px window`)
      .toBeLessThanOrEqual(measured.innerHeight + 1);

    expect(measured.bodyHeight).toBeLessThanOrEqual(measured.innerHeight + 1);

    // The agent-chat pane is the box whose bottom edge carries the input row
    // and Send button (agent-chat's #chat-footer is position:sticky;bottom:0
    // inside the iframe, so it pins to whatever height the host hands it).
    if (measured.paneBottom !== null) {
      expect(measured.paneBottom,
        `agent-chat pane bottom ${measured.paneBottom} overhangs the ${measured.innerHeight}px window`)
        .toBeLessThanOrEqual(measured.innerHeight + 1);
    }
  });

  test('a viewport event re-measures once more after Safari settles', async ({ page }) => {
    await openChatSession(page);
    await waitForUi(page, () => window.terminalUI?._agentChatAvailable === true);

    // Chromium settles visualViewport before it fires the event, so this can
    // only prove the delayed re-measure is wired -- not that it helps on
    // Safari. The device re-test is what settles that.
    const calls = await page.evaluate(async () => {
      const ui = window.terminalUI;
      const original = ui.syncVisibleViewport.bind(ui);
      let count = 0;
      ui.syncVisibleViewport = () => { count += 1; original(); };

      window.visualViewport.dispatchEvent(new Event('resize'));
      const immediate = count;
      await new Promise((r) => setTimeout(r, 600));
      return { immediate, settled: count };
    });

    expect(calls.immediate).toBe(1);
    expect(calls.settled,
      'expected a second syncVisibleViewport() after the settle delay')
      .toBeGreaterThanOrEqual(2);
  });
});
