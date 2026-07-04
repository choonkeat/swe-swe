import { test, expect, chromium } from '@playwright/test';
import crypto from 'crypto';

// Agent View over a REMOTE browser-backend (scripts/e2e-agent-view-remote.sh):
// the dockerless instance runs with SWE_AGENT_VIEW=<backend url>, so opening
// Agent View must allocate a browser on the backend (POST /sessions), wire the
// local CDP reverse-proxy + VNC proxy to it, turn vnc-ready 200, and render
// the noVNC viewer in the UI.
//
// The lazy-start trigger is exercised the way a real agent does it: the shell
// PTY has $SWE_SERVER_PORT/$SESSION_UUID/$MCP_AUTH_KEY in its env, and the
// spec types the same browser/start curl that mcp-lazy-init fires on first
// playwright-MCP use.

const BASE_URL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;
const BACKEND_URL = process.env.E2E_BACKEND_URL || '';
const SHOT_DIR = process.env.E2E_SCREENSHOT_DIR || 'test-results/agent-view';

// Dockerless runs open (no password): empty cookie jar.
test.use({ storageState: { cookies: [], origins: [] } });

test.describe('agent view via remote backend', () => {
  test.skip(!process.env.E2E_AGENT_VIEW, 'agent-view-only: set E2E_AGENT_VIEW=1');

  test('remote backend serves Agent View end-to-end', async ({ page, request }) => {
    test.setTimeout(180_000);
    const uuid = crypto.randomUUID();
    await page.goto(`/session/${uuid}?assistant=shell&session=chat`);
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });

    // WS init: per-session ports + agentViewAvailable (remote => true even on
    // a lean host).
    await page.waitForFunction(() => {
      const ui = window.terminalUI;
      return ui && ui.sessionUUID && ui.vncProxyPort;
    }, null, { timeout: 60_000 });
    const available = await page.evaluate(() => window.terminalUI.agentViewAvailable !== false);
    expect(available).toBe(true);
    await page.screenshot({ path: `${SHOT_DIR}/01-session-before-start.png` });

    // Backend is idle before the lazy start.
    if (BACKEND_URL) {
      const before = await (await request.get(`${BACKEND_URL}/health`)).json();
      expect(before.sessions).toBe(0);
    }

    // Trigger the lazy start from inside the PTY -- the agent's own path.
    await page.locator('.terminal-ui__terminal').first().click();
    await page.keyboard.type(
      'curl -s -X POST "http://localhost:$SWE_SERVER_PORT/api/session/$SESSION_UUID/browser/start?key=$MCP_AUTH_KEY"; echo');
    await page.keyboard.press('Enter');

    // The server pushes browserStarted over the WS, which auto-adds the
    // Agent View pane.
    await page.waitForFunction(() => window.terminalUI.browserStarted === true,
      null, { timeout: 60_000 });
    await page.screenshot({ path: `${SHOT_DIR}/02-browser-started.png` });

    // Backend allocated exactly one browser for this session.
    if (BACKEND_URL) {
      const after = await (await request.get(`${BACKEND_URL}/health`)).json();
      expect(after.sessions).toBe(1);
    }

    // vnc-ready flips 200 -- probing the REMOTE websockify (the Phase A fix).
    await expect.poll(async () => {
      return page.evaluate(async (u) => {
        const r = await fetch(`/api/session/${u}/vnc-ready`);
        return r.status;
      }, uuid);
    }, { timeout: 60_000, intervals: [1000] }).toBe(200);

    // The Agent View iframe points at the VNC proxy and the noVNC viewer
    // actually renders its canvas over the proxied remote websockify.
    const src = await page.waitForFunction(() => {
      const iframe = window.terminalUI.querySelector('.terminal-ui__iframe[data-pane="browser"]');
      return iframe?.getAttribute('src') || null;
    }, null, { timeout: 30_000 }).then(h => h.jsonValue());
    expect(src).toBeTruthy();
    const vncProxyPort = await page.evaluate(() => window.terminalUI.vncProxyPort);
    expect(src).toContain(`:${vncProxyPort}`);

    const vncFrame = page.frameLocator('.terminal-ui__iframe[data-pane="browser"]');
    await expect(vncFrame.locator('canvas').first()).toBeVisible({ timeout: 60_000 });
    // Give noVNC a beat to paint the first framebuffer update before the shot.
    await page.waitForTimeout(2000);
    await page.screenshot({ path: `${SHOT_DIR}/03-agent-view-canvas.png` });

    // localhost resolution (--host-resolver-rules): drive the REMOTE chromium
    // over CDP through the session's local reverse-proxy and load a page the
    // swe-swe HOST serves at localhost. On the image tier the backend's own
    // localhost is a different network namespace, so this only passes if the
    // resolver mapping points chromium back at the swe-swe host (the harness
    // serves a marker page for it -- E2E_LOCALHOST_NAV_PORT -- because the
    // instance itself binds loopback by design).
    const cdpPort = await page.evaluate(() => window.terminalUI.cdpPort);
    expect(cdpPort).toBeTruthy();
    const cdpBrowser = await chromium.connectOverCDP(`http://127.0.0.1:${cdpPort}`);
    try {
      const ctx = cdpBrowser.contexts()[0];
      const remotePage = ctx.pages()[0] || await ctx.newPage();
      const navPort = process.env.E2E_LOCALHOST_NAV_PORT || new URL(BASE_URL).port;
      await remotePage.goto(`http://localhost:${navPort}/`, { timeout: 30_000 });
      await expect(remotePage).toHaveTitle(/swe-swe/, { timeout: 15_000 });
      // The navigation is visible through the VNC pane too -- capture it.
      await page.waitForTimeout(1500);
      await page.screenshot({ path: `${SHOT_DIR}/04-remote-localhost-nav.png` });

      // Wildcard loopback domains: *.lvh.me (subdomain dev DNS) must resolve
      // to the swe-swe host too. The MAP rule bypasses real DNS entirely, so
      // this asserts the wildcard mapping itself -- works even offline.
      await remotePage.goto(`http://app.lvh.me:${navPort}/`, { timeout: 30_000 });
      await expect(remotePage).toHaveTitle(/swe-swe/, { timeout: 15_000 });
      await page.waitForTimeout(1500);
      await page.screenshot({ path: `${SHOT_DIR}/05-remote-lvh-me-nav.png` });
    } finally {
      await cdpBrowser.close();
    }
  });
});
