// A box that was TOLD it has one port: SWE_SINGLE_PORT=1.
//
// proxy-fallback.spec.js covers the other half -- swe-swe finding the path
// form on its own, with nothing configured, which is what almost every user is
// in. That spec blocks the other ports AT THE BROWSER, so it can never see
// whether the server went on listening on them. This one runs against an
// instance where they were never opened, and asserts both halves: nothing
// answers on the proxy bands, and every pane still works, in `path` mode,
// without waiting on a reachability probe.
//
// Run it with `make test-e2e-single-port`.

import { test, expect } from './_helpers/reaper.js';
import { endSessions, openSessionViaPost } from './_helpers/sessions.js';

const BASE_URL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;
const HOST = new URL(BASE_URL).hostname;
const OFFSET = 20000;

// First port of each band the tier reserved, as written into the state file by
// scripts/e2e-up.sh. A session takes the first free slot, so the first number
// is the one a single session would have used.
function bandStart(spec, fallback) {
  const raw = (spec || '').split('-')[0];
  const n = parseInt(raw, 10);
  return Number.isFinite(n) ? n : fallback;
}
const PREVIEW_BAND = bandStart(process.env.E2E_PREVIEW_PORTS, 3400);
const AGENT_CHAT_BAND = bandStart(process.env.E2E_AGENT_CHAT_PORTS, 4400);
const VNC_BAND = bandStart(process.env.E2E_VNC_PORTS, 7400);
// Files is derived as preview+6000 (filesPortFromPreview), not configured.
const FILES_BAND = PREVIEW_BAND + 6000;

let testSessions = [];

test.describe('single-port mode', () => {
  test.skip(!process.env.E2E_SINGLE_PORT, 'needs a stack brought up with `./scripts/e2e-up.sh single-port`');

  test.beforeEach(async () => {
    testSessions = [];
  });

  test.afterEach(async ({ page }, testInfo) => {
    if (testInfo.status === 'passed' && testSessions.length > 0) {
      await endSessions(page, testSessions);
    }
  });

  // The assertion no browser-side test can make. A session is created first,
  // because the listeners this is about are per-session: without one there
  // would be nothing to NOT bind.
  test('the per-session proxy ports are not listening', async ({ page, request }) => {
    const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'chat' });
    testSessions.push(uuid);
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 40_000 });

    for (const [pane, base] of [
      ['preview', PREVIEW_BAND],
      ['agent chat', AGENT_CHAT_BAND],
      ['vnc', VNC_BAND],
      ['files', FILES_BAND],
    ]) {
      const url = `http://${HOST}:${base + OFFSET}/__probe__`;
      let answered = true;
      try {
        await request.get(url, { timeout: 5000 });
      } catch {
        answered = false;
      }
      expect(answered, `${pane} proxy port ${base + OFFSET} answered; in single-port mode nothing should be listening on it`).toBe(false);
    }
  });

  // The other half: with no proxy port advertised, the frontend must not treat
  // the panes as missing. Agent View and Files both used to read their own
  // existence off that port.
  test('every pane is present and resolves to its same-origin path form', async ({ page }) => {
    const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'chat' });
    testSessions.push(uuid);
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 40_000 });

    // No proxy ports at all: that is what sends every pane to its path form.
    const advertised = await page.waitForFunction(() => {
      const ui = window.terminalUI;
      if (!ui || !ui.sessionUUID) return null;
      return {
        preview: ui.previewProxyPort,
        chat: ui.agentChatProxyPort,
        vnc: ui.vncProxyPort,
        files: ui.filesProxyPort,
        filesPort: ui.filesPort,
      };
    }, null, { timeout: 60_000 }).then((h) => h.jsonValue());
    expect(advertised.preview).toBeFalsy();
    expect(advertised.chat).toBeFalsy();
    expect(advertised.vnc).toBeFalsy();
    expect(advertised.files).toBeFalsy();
    // ...while the real md-serve port, which says the Files pane exists, is
    // still there. Without it the Files tab would simply vanish.
    expect(advertised.filesPort).toBeTruthy();

    await page.waitForFunction(
      () => window.terminalUI && window.terminalUI._acProxyMode,
      null,
      { timeout: 120_000 }
    );

    await page.evaluate(() => {
      const ui = window.terminalUI;
      for (const pane of ['preview', 'files']) {
        const slot = ui._slotForPane(pane);
        if (slot) ui.setActiveInSlot(slot, pane, { persist: false });
        ui._loadPaneIfNeeded(pane);
      }
    });
    await page.waitForFunction(
      () => window.terminalUI && window.terminalUI._proxyMode,
      null,
      { timeout: 120_000 }
    );

    const state = await page.evaluate(() => ({
      preview: window.terminalUI._proxyMode,
      chat: window.terminalUI._acProxyMode,
      files: window.terminalUI._filesProxyMode,
      filesBase: window.terminalUI._filesBaseUrl(),
      filesKnown: window.terminalUI._isPaneKnown('files'),
    }));
    expect(state.preview).toBe('path');
    expect(state.chat).toBe('path');
    // Files never resolves a cross-origin base here, so _filesProxyMode stays
    // unset; what matters is the base it hands out and that the tab exists.
    expect(state.filesBase).toBe(`${BASE_URL}/proxy/${uuid}/files`);
    expect(state.filesKnown).toBe(true);
  });

  // Agent View is the pane that decided "do I exist?" from vncProxyPort, so it
  // is the one that would disappear rather than fall back. It also carries a
  // WebSocket, which the path route has to proxy, not just the page.
  test('Agent View appears without its proxy port and noVNC connects over the path route', async ({ page }) => {
    test.setTimeout(300_000);

    const uuid = await openSessionViaPost(page, { assistant: 'shell', session: 'chat' });
    testSessions.push(uuid);
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 40_000 });

    // Wait for the first status frame before touching the terminal: the PTY
    // is spawned with the session, and a line typed before the shell has a
    // prompt is simply lost. proxy-fallback.spec.js gets this wait for free by
    // waiting on vncProxyPort, which does not exist here -- so ask for the
    // signal that does, and then retype until the command actually lands.
    await page.waitForFunction(
      () => window.terminalUI && window.terminalUI.agentViewAvailable === true,
      null,
      { timeout: 60_000 }
    );
    await page.locator('.terminal-ui__terminal').first().click();
    await expect.poll(async () => {
      await page.keyboard.type(
        'curl -s -X POST "http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY"; echo');
      await page.keyboard.press('Enter');
      try {
        await page.waitForFunction(
          () => window.terminalUI.browserStarted === true,
          null,
          { timeout: 45_000 }
        );
        return true;
      } catch {
        return false;
      }
    }, { timeout: 200_000, intervals: [1000] }).toBe(true);

    await expect.poll(async () => page.evaluate(async (u) => {
      const r = await fetch(`/api/session/${u}/vnc-ready`);
      return r.status;
    }, uuid), { timeout: 120_000, intervals: [1000] }).toBe(200);

    const view = await page.evaluate(() => ({
      known: window.terminalUI._isPaneKnown('browser'),
      url: window.terminalUI.getBrowserViewUrl(),
      port: window.terminalUI.vncProxyPort,
    }));
    expect(view.port).toBeFalsy();
    expect(view.known).toBe(true);
    expect(view.url).toContain(`/proxy/${uuid}/vnc/vnc_lite.html`);
    expect(view.url).toContain(`path=proxy/${uuid}/vnc/websockify`);

    await page.evaluate(() => {
      const ui = window.terminalUI;
      const slot = ui._slotForPane('browser');
      if (slot) ui.setActiveInSlot(slot, 'browser', { persist: false });
      ui._loadPaneIfNeeded('browser');
    });

    // A canvas only appears once RFB has negotiated over the proxied
    // websockify -- the path route carried the WebSocket, not just the page.
    const vncFrame = page.frameLocator('.terminal-ui__iframe[data-pane="browser"]');
    await expect(vncFrame.locator('canvas').first()).toBeVisible({ timeout: 120_000 });
  });

  // A front proxy that stamps "X-Frame-Options: deny" on every response that
  // lacks one -- seen live behind a Cloudflare Access gateway -- blanked every
  // pane, because in this mode each pane is a same-origin /proxy/{uuid}/...
  // page inside an iframe. Nothing above could see it: they check which URL a
  // pane resolved to, never whether the browser agreed to show it. Stand the
  // gateway up in the browser and require the frames to actually load.
  test('panes load behind a front proxy that adds X-Frame-Options: deny when absent', async ({ page, context }) => {
    await context.route('**/proxy/**', async (route) => {
      if (route.request().resourceType() !== 'document') return route.continue();
      const response = await route.fetch({ maxRedirects: 0 });
      const headers = response.headers();
      if (!('x-frame-options' in headers)) headers['x-frame-options'] = 'deny';
      if (!('content-security-policy' in headers)) headers['content-security-policy'] = "script-src 'self';";
      await route.fulfill({ response, headers });
    });

    const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'chat' });
    testSessions.push(uuid);
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 40_000 });

    await page.waitForFunction(
      () => window.terminalUI && window.terminalUI._acProxyMode,
      null,
      { timeout: 120_000 }
    );
    await page.evaluate(() => {
      const ui = window.terminalUI;
      const slot = ui._slotForPane('files');
      if (slot) ui.setActiveInSlot(slot, 'files', { persist: false });
      ui._loadPaneIfNeeded('files');
    });

    // A frame the browser refused sits on chrome-error://chromewebdata/, so
    // the pane's own URL showing up as a frame is the proof it was displayed.
    for (const pane of ['agentchat', 'files']) {
      await expect.poll(
        () => page.frames().some((f) => f.url().includes(`/proxy/${uuid}/${pane}/`)),
        { message: `${pane} pane never displayed behind the deny-stamping proxy`, timeout: 120_000, intervals: [1000] }
      ).toBe(true);
    }
  });
});
