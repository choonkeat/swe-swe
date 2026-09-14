// Single-reachable-port boxes: Preview, Agent Chat and Files must all fall
// back to their same-origin path form.
//
// Simulated by refusing, at the browser, every connection to this host on any
// port but the one that served the page -- which is exactly what a VPS with a
// single open port, or a reverse proxy in front, does. Before the shared
// resolver (static/modules/proxy-base.js) the Files pane had no path form at
// all: it loaded its unreachable port and sat on the browser's own "refused to
// connect" page, which still fires `load`, so the retry supervisor re-requested
// the same dead port forever.

import { test, expect } from './_helpers/reaper.js';
import { endSessions, openSessionViaPost } from './_helpers/sessions.js';

const BASE_URL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;
const MAIN_PORT = new URL(BASE_URL).port || (BASE_URL.startsWith('https') ? '443' : '80');
const MAIN_HOST = new URL(BASE_URL).hostname;

// Refuse every port on this host except the one serving the page.
async function onlyMainPortReachable(context) {
  await context.route('**', (route) => {
    const u = new URL(route.request().url());
    const port = u.port || (u.protocol === 'https:' ? '443' : '80');
    if (u.hostname === MAIN_HOST && port !== MAIN_PORT) {
      return route.abort('connectionrefused');
    }
    return route.continue();
  });
}

let testSessions = [];

test.describe('single reachable port', () => {
  test.beforeEach(async () => {
    testSessions = [];
  });

  test.afterEach(async ({ page }, testInfo) => {
    if (testInfo.status === 'passed' && testSessions.length > 0) {
      await endSessions(page, testSessions);
    }
  });

  test('every pane falls back to its same-origin path form', async ({ page, context }) => {
    await onlyMainPortReachable(context);

    const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'chat' });
    testSessions.push(uuid);
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 40_000 });

    // Agent Chat probes on its own at session start; Preview and Files are
    // resolved when their pane loads.
    await page.waitForFunction(
      () => window.terminalUI && window.terminalUI._acProxyMode,
      null,
      { timeout: 120_000 }
    );
    await page.waitForFunction(
      () => window.terminalUI && window.terminalUI._filesResolvedBase,
      null,
      { timeout: 120_000 }
    );
    await page.evaluate(() => {
      const ui = window.terminalUI;
      const slot = ui._slotForPane('preview');
      if (slot) ui.setActiveInSlot(slot, 'preview', { persist: false });
      ui._loadPaneIfNeeded('preview');
    });
    await page.waitForFunction(
      () => window.terminalUI && window.terminalUI._proxyMode,
      null,
      { timeout: 120_000 }
    );

    const modes = await page.evaluate(() => ({
      preview: window.terminalUI._proxyMode,
      chat: window.terminalUI._acProxyMode,
      files: window.terminalUI._filesProxyMode,
      filesBase: window.terminalUI._filesResolvedBase,
      chatSrc: (document.querySelector('.terminal-ui__agent-chat-iframe') || {}).src || '',
    }));

    expect(modes.preview).toBe('path');
    expect(modes.chat).toBe('path');
    expect(modes.files).toBe('path');
    expect(modes.filesBase).toBe(`${BASE_URL}/proxy/${uuid}/files`);
    // The file-link origin handed to agent-chat has to be reachable too,
    // otherwise workspace links open onto a dead port.
    expect(modes.chatSrc).toContain(encodeURIComponent(`${BASE_URL}/proxy/${uuid}/files/`));
  });

  test('the Files path route serves md-serve with its links re-prefixed', async ({ page, context }) => {
    await onlyMainPortReachable(context);

    const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'chat' });
    testSessions.push(uuid);
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 40_000 });

    // md-serve is launched through swe-npx and its cold start outlasts the
    // proxy bind, so a 502 here is "not yet", not "broken".
    const page1 = await page.evaluate(async (u) => {
      const prefix = `/proxy/${u}/files`;
      for (let i = 0; i < 40; i++) {
        const r = await fetch(prefix + '/', { credentials: 'include' });
        const html = await r.text();
        if (r.status === 200) return { status: r.status, html };
        await new Promise((res) => setTimeout(res, 3000));
      }
      return { status: 0, html: '' };
    }, uuid);

    expect(page1.status).toBe(200);
    // The stylesheet md-serve emits at a root-absolute path must come back
    // carrying the prefix, or the pane renders unstyled.
    expect(page1.html).toContain(`/proxy/${uuid}/files/_md-serve-assets/`);

    const css = await page.evaluate(async (u) => {
      const r = await fetch(`/proxy/${u}/files/_md-serve-assets/github-markdown-light.css`, { credentials: 'include' });
      return { status: r.status, type: r.headers.get('content-type') };
    }, uuid);
    expect(css.status).toBe(200);
    expect(css.type).toContain('text/css');
  });
});
