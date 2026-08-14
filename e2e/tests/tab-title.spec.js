import { test, expect } from './_helpers/reaper.js';
import { endSessions, openSessionViaPost } from './_helpers/sessions.js';

// The browser tab is the only part of a session visible when the session is
// not, so it carries the turn. What it puts in front of the session name --
// hourglass and clock while the agent works, a dot for a run that finished
// while you were looking elsewhere, nothing when nothing is waiting -- is
// agent-chat's to decide, and arrives already rendered as `titlePrefix`.
//
// So these tests assert the contract, not the format: whatever prefix arrives
// is used verbatim, and the mark is retired on each of the three ways the user
// can come back. A deliberately unreal prefix keeps them honest -- an
// assertion on the hourglass would be this repo re-stating a format it no
// longer owns, and would start failing the day agent-chat changed its mind.

const BUSY_PREFIX = 'BUSY42 - ';
const DONE_PREFIX = 'DOT ';

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

test('the prefix agent-chat sends is used verbatim', async ({ page }) => {
  await openSession(page);
  const name = await expectedName(page);

  await reportTurn(page, { busy: true, titlePrefix: BUSY_PREFIX });
  expect(await page.title()).toBe(`${BUSY_PREFIX}${name}`);

  // The clock advances because agent-chat re-sends it, not because anything
  // here knows how to draw the next tick.
  await reportTurn(page, { busy: true, titlePrefix: 'BUSY43 - ' });
  expect(await page.title()).toBe(`BUSY43 - ${name}`);

  await reportTurn(page, { busy: false, titlePrefix: '' });
  expect(await page.title()).toBe(name);
});

test('an agent-chat too old to send a prefix leaves the plain session name', async ({ page }) => {
  await openSession(page);
  const name = await expectedName(page);

  // The pre-titlePrefix message shape, which older installs still post.
  await reportTurn(page, { busy: true, since: Date.now() - 92_000, finished: false });
  expect(await page.title()).toBe(name);
});

test('a run that finishes while the window is unfocused leaves the mark', async ({ page }) => {
  await openSession(page);
  const name = await expectedName(page);

  // Headless Chromium reports the only page as focused, so the "you were
  // looking elsewhere" condition has to be stated outright.
  await page.evaluate(() => { document.hasFocus = () => false; });
  await reportTurn(page, { busy: true, titlePrefix: BUSY_PREFIX });
  await reportTurn(page, { busy: false, finished: true, titlePrefix: DONE_PREFIX });
  expect(await page.title()).toBe(`${DONE_PREFIX}${name}`);

  // Coming back to the window IS reading the reply.
  await page.evaluate(() => {
    document.hasFocus = () => true;
    window.dispatchEvent(new Event('focus'));
  });
  await page.waitForTimeout(100);
  expect(await page.title()).toBe(name);
});

// The clear used to hang off this window's focus event alone, which is the one
// arrival that does NOT happen when the user goes back to the chat: focus
// fires on the browsing context that gained it, so focus landing in the
// cross-origin chat iframe reaches the iframe and blur reaches us. The mark
// then outlived the reading of the reply it pointed at.
test('focus taken by the chat iframe clears the mark', async ({ page }) => {
  await openSession(page);
  const name = await expectedName(page);

  await page.evaluate(() => { document.hasFocus = () => false; });
  await reportTurn(page, { busy: true, titlePrefix: BUSY_PREFIX });
  await reportTurn(page, { busy: false, finished: true, titlePrefix: DONE_PREFIX });
  expect(await page.title()).toBe(`${DONE_PREFIX}${name}`);

  // What agent-chat posts from its own focus handler: cleared prefix, plus the
  // flag. No top-window focus event accompanies it -- that is the whole point.
  await page.evaluate(() => { document.hasFocus = () => true; });
  await reportTurn(page, { busy: false, focused: true, titlePrefix: '' });
  expect(await page.title()).toBe(name);
});

test('returning to the tab clears the mark without a focus event', async ({ page }) => {
  await openSession(page);
  const name = await expectedName(page);

  await page.evaluate(() => { document.hasFocus = () => false; });
  await reportTurn(page, { busy: true, titlePrefix: BUSY_PREFIX });
  await reportTurn(page, { busy: false, finished: true, titlePrefix: DONE_PREFIX });
  expect(await page.title()).toBe(`${DONE_PREFIX}${name}`);

  // visibilitychange fires when focus comes back to browser chrome, or to a
  // pane iframe, and nothing in this document is focused at all.
  await page.evaluate(() => {
    document.hasFocus = () => true;
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await page.waitForTimeout(100);
  expect(await page.title()).toBe(name);
});

test('coming back mid-run leaves the running clock alone', async ({ page }) => {
  await openSession(page);
  const name = await expectedName(page);

  await reportTurn(page, { busy: true, titlePrefix: BUSY_PREFIX });
  // Clicking into the chat while the agent is still working must not be read
  // as the run ending, on any of the three arrivals.
  await reportTurn(page, { busy: true, focused: true, titlePrefix: BUSY_PREFIX });
  await page.evaluate(() => {
    window.dispatchEvent(new Event('focus'));
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await page.waitForTimeout(100);
  expect(await page.title()).toBe(`${BUSY_PREFIX}${name}`);
});

test('a finish read with the cursor in the terminal leaves no mark', async ({ page }) => {
  await openSession(page);
  const name = await expectedName(page);

  // agent-chat asks whether ITS document had focus, and inside swe-swe it is
  // one pane of several: focus resting in the terminal makes it report the
  // finish as unseen. This window knows better.
  await page.evaluate(() => { document.hasFocus = () => true; });
  await reportTurn(page, { busy: true, titlePrefix: BUSY_PREFIX });
  await reportTurn(page, { busy: false, finished: true, titlePrefix: DONE_PREFIX });
  expect(await page.title()).toBe(name);
});
