import { test, expect } from './_helpers/reaper.js';
import { endSessions, openSessionViaPost } from './_helpers/sessions.js';

// The browser tab is the only part of a session visible when the session is
// not, so it carries the turn: hourglass + elapsed while the agent works, a
// green circle for a run that finished while you were looking elsewhere, and
// the bare session name when nothing is waiting on you.
//
// The turn itself is reported by the agent-chat iframe, which is cross-origin
// and therefore only reachable by postMessage. These tests post the same
// messages agent-chat posts, which is the contract the two halves share --
// driving the real chat pane would test agent-chat's release, not this one.

const HOURGLASS = '⏳';
const GREEN = '🟢';

let testSessions = [];

async function openSession(page) {
  const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'terminal' });
  testSessions.push(uuid);
  await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
  // The name arrives on the first status frame; until then the title is still
  // whatever the server rendered.
  await page.waitForFunction(() => {
    const ui = document.querySelector('terminal-ui');
    return ui && (ui.sessionName || ui.uuidShort);
  }, null, { timeout: 30_000 });
  return uuid;
}

// Post exactly what agent-chat posts, and wait for the title to settle.
async function reportTurn(page, state) {
  await page.evaluate((s) => {
    window.postMessage({ type: 'agent-chat-turn-state', ...s }, '*');
  }, state);
  await page.waitForTimeout(100);
}

async function expectedName(page) {
  return page.evaluate(() => {
    const ui = document.querySelector('terminal-ui');
    return ui.sessionName || ui.uuidShort || 'Session';
  });
}

test.afterEach(async ({ page }, testInfo) => {
  if (testInfo.status === 'passed' && testSessions.length) {
    await endSessions(page, testSessions);
  }
  testSessions = [];
});

test('idle tab title is the session name, not the server-rendered triple', async ({ page }) => {
  await openSession(page);
  const name = await expectedName(page);
  expect(await page.title()).toBe(name);
  expect(await page.title()).not.toContain('swe-swe');
});

test('busy tab title shows the hourglass and the elapsed clock', async ({ page }) => {
  await openSession(page);
  const name = await expectedName(page);

  // 92s ago -> "1m32s". Anchored to agent-chat's own loader start, so a
  // reloaded tab shows the true age of the run instead of restarting at 0s.
  await reportTurn(page, { busy: true, since: Date.now() - 92_000, finished: false });
  expect(await page.title()).toMatch(new RegExp(`^${HOURGLASS}1m3\\ds - ${escapeRe(name)}$`));

  // Under a minute drops the minutes entirely.
  await reportTurn(page, { busy: true, since: Date.now() - 5_000, finished: false });
  expect(await page.title()).toMatch(new RegExp(`^${HOURGLASS}\\ds - ${escapeRe(name)}$`));
});

test('the clock ticks while the agent stays busy', async ({ page }) => {
  await openSession(page);
  await reportTurn(page, { busy: true, since: Date.now(), finished: false });
  const first = await page.title();
  await page.waitForTimeout(2200);
  const later = await page.title();
  expect(later).not.toBe(first);
  expect(later.startsWith(HOURGLASS)).toBe(true);
});

test('a run that finishes while the window is unfocused leaves a green mark', async ({ page }) => {
  await openSession(page);
  const name = await expectedName(page);

  // Headless Chromium reports the only page as focused, so the "you were
  // looking elsewhere" condition has to be stated outright.
  await page.evaluate(() => { document.hasFocus = () => false; });
  await reportTurn(page, { busy: true, since: Date.now(), finished: false });
  await reportTurn(page, { busy: false, since: 0, finished: true });
  expect(await page.title()).toBe(`${GREEN} ${name}`);

  // Coming back to the window IS reading the reply.
  await page.evaluate(() => {
    document.hasFocus = () => true;
    window.dispatchEvent(new Event('focus'));
  });
  await page.waitForTimeout(100);
  expect(await page.title()).toBe(name);
});

test('a run that finishes while you are watching leaves no mark to dismiss', async ({ page }) => {
  await openSession(page);
  const name = await expectedName(page);

  await page.evaluate(() => { document.hasFocus = () => true; });
  await reportTurn(page, { busy: true, since: Date.now(), finished: false });
  await reportTurn(page, { busy: false, since: 0, finished: true });
  expect(await page.title()).toBe(name);
});

test('a replayed history finish never lights the green mark', async ({ page }) => {
  await openSession(page);
  const name = await expectedName(page);

  // agent-chat suppresses `finished` during history replay; this asserts the
  // swe-swe half honours a plain busy -> idle report with no finish edge,
  // which is what a reconnecting tab sees.
  await page.evaluate(() => { document.hasFocus = () => false; });
  await reportTurn(page, { busy: true, since: Date.now() - 60_000, finished: false });
  await reportTurn(page, { busy: false, since: 0, finished: false });
  expect(await page.title()).toBe(name);
});

function escapeRe(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
