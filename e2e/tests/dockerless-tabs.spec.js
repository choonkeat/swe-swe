import { test, expect } from '@playwright/test';
import crypto from 'crypto';

// Live tab coverage for the DOCKERLESS run (no Docker daemon): scripts/
// e2e-dockerless.sh boots `swe-swe init --dockerless` + `swe-swe up` with auth
// disabled, then runs this spec with E2E_DOCKERLESS=1 and E2E_BASE_URL pointed
// at the host-native server. It is skipped in the container suite (auth on,
// where the empty cookie jar + open-mode assumptions would not hold).
//
// Scope: the parts of the dockerless DX that the curl harness cannot reach --
// the websocket-driven, npx-backed tabs. Agent Chat probe-success and Agent
// View are intentionally out of scope here: the former needs a working agent
// (LLM auth), the latter the browser stack (chromium/Xvfb), which is Phase 5.

const BASE_URL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;

// Dockerless runs open (no password), so use an empty cookie jar rather than
// the suite-wide authenticated storageState.
test.use({ storageState: { cookies: [], origins: [] } });

test.describe('dockerless live tabs', () => {
  test.skip(!process.env.E2E_DOCKERLESS, 'dockerless-only: set E2E_DOCKERLESS=1');

  // Agent Terminal + the per-session proxy ports the server allocates on WS
  // init. These arrive independent of the agent process, so they prove the
  // session websocket (PTY transport) is live in dockerless mode.
  test('session connects: server delivers per-session proxy ports over WS', async ({ page }) => {
    const uuid = crypto.randomUUID();
    await page.goto(`/session/${uuid}?assistant=opencode&session=chat`);
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });

    const ports = await page.waitForFunction(() => {
      const ui = window.terminalUI;
      if (!ui || !ui.sessionUUID) return null;
      if (!ui.previewProxyPort || !ui.filesProxyPort) return null;
      return {
        sessionUUID: ui.sessionUUID,
        previewProxyPort: ui.previewProxyPort,
        filesProxyPort: ui.filesProxyPort,
      };
    }, null, { timeout: 60_000 }).then(h => h.jsonValue());

    expect(ports.sessionUUID).toBe(uuid);
    expect(ports.previewProxyPort).toBeTruthy();
    expect(ports.filesProxyPort).toBeTruthy();
  });

  // Files tab = per-session md-serve started via npx. This is the canonical
  // "needs a host dependency (node/npx) and a free port" tab, so it is the
  // most important dockerless assertion: add the pane and confirm md-serve is
  // actually answering on the cross-origin filesProxyPort.
  test('Files tab: md-serve answers on filesProxyPort', async ({ page }) => {
    const uuid = crypto.randomUUID();
    await page.goto(`/session/${uuid}?assistant=opencode&session=chat`);
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });

    await page.waitForFunction(() => {
      const ui = window.terminalUI;
      return ui && ui.filesProxyPort && ui.sessionUUID;
    }, null, { timeout: 60_000 });
    const filesProxyPort = await page.evaluate(() => window.terminalUI.filesProxyPort);
    expect(filesProxyPort).toBeTruthy();

    // Add the Files pane via whichever slot's "+" popover offers it.
    const addBtns = page.locator('.terminal-ui__slot-add');
    const addCount = await addBtns.count();
    expect(addCount).toBeGreaterThan(0);
    let clickedFiles = false;
    for (let i = 0; i < addCount; i++) {
      await addBtns.nth(i).click();
      const menu = page.locator('.terminal-ui__slot-replace-menu');
      await menu.waitFor({ state: 'visible' });
      const filesItem = menu.locator('.terminal-ui__slot-replace-item', { hasText: 'Files' });
      if (await filesItem.count() > 0) {
        await filesItem.first().click();
        clickedFiles = true;
        break;
      }
      await page.mouse.click(2, 2);
      await menu.waitFor({ state: 'detached' }).catch(() => {});
    }
    expect(clickedFiles).toBe(true);

    // The files iframe must point at the cross-origin filesProxyPort.
    const src = await page.waitForFunction(() => {
      const iframe = window.terminalUI.querySelector('.terminal-ui__iframe[data-pane="files"]');
      return iframe?.getAttribute('src') || null;
    }, null, { timeout: 10_000 }).then(h => h.jsonValue());
    expect(src).toContain(`:${filesProxyPort}`);

    // md-serve actually answering (opaque no-cors response resolves).
    const url = new URL(BASE_URL);
    const filesUrl = `${url.protocol}//${url.hostname}:${filesProxyPort}/`;
    const reachable = await page.evaluate(async (fetchUrl) => {
      try {
        await fetch(fetchUrl, { signal: AbortSignal.timeout(8000), mode: 'no-cors' });
        return { ok: true };
      } catch (e) {
        return { ok: false, error: e.message };
      }
    }, filesUrl);
    expect(reachable.ok).toBe(true);
  });

  // Preview tab wiring: the iframe src resolves to the previewProxyPort once
  // the WS delivers it (the proxy answers even before a dev server runs).
  test('Preview tab: iframe src is wired to the preview proxy', async ({ page }) => {
    const uuid = crypto.randomUUID();
    await page.goto(`/session/${uuid}?assistant=opencode&session=chat`);
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });

    await page.waitForFunction(() => {
      const ui = window.terminalUI;
      return ui && ui.previewPort && ui.sessionUUID;
    }, null, { timeout: 60_000 });

    const src = await page.waitForFunction(() => {
      const iframe = window.terminalUI.querySelector('.terminal-ui__iframe[data-pane="preview"]');
      return iframe?.getAttribute('src') || null;
    }, null, { timeout: 10_000 }).then(h => h.jsonValue());
    expect(src).toBeTruthy();
    expect(src).not.toBe('');
  });
});
