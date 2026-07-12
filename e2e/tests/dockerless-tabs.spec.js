import { test, expect } from '@playwright/test';

// Stage a session via POST /api/session/new (the no-ghost-session gate: the
// WS handler only materializes sessions with a staged creation intent --
// navigating straight to /session/{random-uuid} is refused), then follow the
// 302 to the session page. Returns the minted UUID.
async function openNewSession(page, params) {
  const resp = await page.request.post('/api/session/new', {
    form: params,
    maxRedirects: 0,
  });
  expect(resp.status()).toBe(302);
  const loc = resp.headers()['location'];
  expect(loc).toBeTruthy();
  await page.goto(loc);
  return loc.match(/\/session\/([0-9a-f-]+)/)[1];
}

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
    const uuid = await openNewSession(page, { assistant: 'opencode', session: 'chat' });
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
    await openNewSession(page, { assistant: 'opencode', session: 'chat' });
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });

    await page.waitForFunction(() => {
      const ui = window.terminalUI;
      return ui && ui.filesProxyPort && ui.sessionUUID;
    }, null, { timeout: 60_000 });
    const filesProxyPort = await page.evaluate(() => window.terminalUI.filesProxyPort);
    expect(filesProxyPort).toBeTruthy();

    // Files ships in the classic preset defaults; its tab renders once
    // filesProxyPort makes the pane "known". Click to (re)activate -- the
    // chat probe-success handoff may have focused Agent Chat by now.
    const filesTab = page.locator('.terminal-ui__slot-tab[data-pane="files"]');
    await filesTab.waitFor({ timeout: 10_000 });
    await filesTab.click();

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

  // Files tab follows the swe-swe theme: md-serve is launched with
  // -theme-cookie swe-swe-theme, and the files reverse proxy forwards the
  // inbound Cookie header verbatim. So a request carrying swe-swe-theme=dark
  // must pin the dark stylesheet server-side, and =light the light one --
  // overriding the browser's prefers-color-scheme. Without the flag md-serve
  // would ignore the cookie and emit its auto page (no single pinned sheet).
  test('Files tab: swe-swe-theme cookie pins md-serve stylesheet', async ({ page }) => {
    await openNewSession(page, { assistant: 'opencode', session: 'chat' });
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });

    await page.waitForFunction(() => {
      const ui = window.terminalUI;
      return ui && ui.filesProxyPort && ui.sessionUUID;
    }, null, { timeout: 60_000 });
    const filesProxyPort = await page.evaluate(() => window.terminalUI.filesProxyPort);
    expect(filesProxyPort).toBeTruthy();

    const url = new URL(BASE_URL);
    const filesUrl = `${url.protocol}//${url.hostname}:${filesProxyPort}/`;

    // dark cookie -> dark stylesheet pinned, light absent.
    const dark = await page.request.get(filesUrl, { headers: { Cookie: 'swe-swe-theme=dark' } });
    expect(dark.ok()).toBeTruthy();
    const darkBody = await dark.text();
    expect(darkBody).toContain('github-markdown-dark.css');
    expect(darkBody).not.toContain('github-markdown-light.css');

    // light cookie -> light stylesheet pinned, dark absent.
    const light = await page.request.get(filesUrl, { headers: { Cookie: 'swe-swe-theme=light' } });
    expect(light.ok()).toBeTruthy();
    const lightBody = await light.text();
    expect(lightBody).toContain('github-markdown-light.css');
    expect(lightBody).not.toContain('github-markdown-dark.css');
  });

  // Preview tab wiring: the iframe src resolves to the previewProxyPort once
  // the WS delivers it (the proxy answers even before a dev server runs).
  test('Preview tab: iframe src is wired to the preview proxy', async ({ page }) => {
    await openNewSession(page, { assistant: 'opencode', session: 'chat' });
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
