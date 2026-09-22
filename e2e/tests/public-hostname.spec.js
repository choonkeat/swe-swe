import { test, expect } from './_helpers/reaper.js';
import { openSessionViaPost } from './_helpers/sessions.js';

// Wildcard host-demux, live.
//
// tunnel.spec.js asserts the OTHER half: that the frontend WRITES
// "{port}.{apex}" addresses. It never sends one. This spec sends them, because
// the two halves fail independently -- the address can be perfect while
// nothing answers it, which is the state -public-hostname shipped in (unit
// tests for the demuxer, one hand-run live proof in a doc, nothing automated).
//
// Run it with `make test-e2e-wildcard`. That brings the stack up with
// SWE_PUBLIC_HOSTNAME=lvh.me AND reaches it at lvh.me -- both matter. The
// frontend only builds subdomain links when the page it runs in was itself
// loaded from the apex (terminal-ui.js, effectivePublicHostname), and the
// login cookie is only issued with Domain=.lvh.me on that path. Without the
// apex there is nothing here to assert, so every test skips.
//
// The browser is told where lvh.me lives by playwright.config.js
// (--host-resolver-rules); no DNS is involved, and the suite works offline.

const APEX = process.env.SWE_PUBLIC_HOSTNAME || '';
const BASE_URL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;
const base = new URL(BASE_URL);
const MAIN_PORT = Number(base.port || (base.protocol === 'https:' ? 443 : 80));

// A signature of what a page actually is, independent of whichever app is
// behind a port. We never assert "the Files pane looks like X"; we assert the
// subdomain and the direct port produce the SAME page, and that it is not the
// homepage. That is the whole contract, and it survives a pane redesign.
async function pageSignature(page, url) {
  const resp = await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 30_000 });
  const title = await page.title();
  const text = ((await page.locator('body').innerText().catch(() => '')) || '')
    .replace(/\s+/g, ' ')
    .trim()
    .slice(0, 200);
  return { status: resp ? resp.status() : 0, title, text };
}

// Poll a pane until it answers. The agent-chat sidecar boots with the session
// (~15s) and the Files pane is a per-session md-serve that `npx` may have to
// cold-fetch from the registry on a fresh container. Without this the control
// leg below races the pane and the whole spec fails for a reason that has
// nothing to do with routing.
async function waitForPane(page, url, label) {
  let last = { status: 0, title: '', text: '' };
  for (let i = 0; i < 45; i++) {
    last = await pageSignature(page, url);
    if (last.status < 400) return last;
    await page.waitForTimeout(2000);
  }
  throw new Error(`${label} never answered at ${url} (last status ${last.status})`);
}

test.describe('wildcard host-demux serves {port}.{apex} on the main listener', () => {
  test.skip(APEX === '', 'needs SWE_PUBLIC_HOSTNAME (run `make test-e2e-wildcard`)');
  // Wildcard mode routes {proxyPort}.{apex} to the per-port listeners, so the
  // server refuses to start with both settings at once.
  test.skip(!!process.env.E2E_SINGLE_PORT, 'wildcard mode and single-port mode are mutually exclusive');

  test('the panes answer on their subdomains, and nothing else does', async ({ page }) => {
    // Two panes, each with its own cold start, plus a dozen navigations.
    test.setTimeout(300_000);
    // The page must be served from the apex or the frontend stays in
    // port-based mode and the subdomain links are never built.
    expect(base.hostname, 'suite must reach the app at the apex').toBe(APEX);

    // opencode + session=chat, matching ports.spec.js: the agent-chat sidecar
    // is what serves that band, and it is only started for a real agent -- a
    // `shell` session allocates the port but leaves it answering 502.
    const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'chat' });
    expect(uuid).toBeTruthy();

    const ports = await page.waitForFunction(() => {
      const ui = window.terminalUI;
      if (!ui || !ui.previewProxyPort) return null;
      if (!ui.agentChatProxyPort) return null;
      if (!ui.filesProxyPort) return null;
      return {
        publicHostname: ui.publicHostname,
        effective: ui.effectivePublicHostname,
        previewProxyPort: ui.previewProxyPort,
        agentChatProxyPort: ui.agentChatProxyPort,
        filesProxyPort: ui.filesProxyPort,
        previewBaseUrl: ui.getPreviewBaseUrl(),
      };
    }, { timeout: 60_000 }).then(h => h.jsonValue());

    // Both must hold: the server is in subdomain mode, and the page agrees it
    // was reached that way. Otherwise the rest of this spec would be asserting
    // against plain fall-through.
    expect(ports.publicHostname).toBe(APEX);
    expect(ports.effective).toBe(APEX);

    // The address the UI hands the user is the PROXY port (the band the
    // allowlist opens), carrying the listener's port -- not the raw target
    // port, and not port 80.
    expect(ports.previewBaseUrl).toBe(`${base.protocol}//${ports.previewProxyPort}.${APEX}:${MAIN_PORT}`);

    const homepage = await pageSignature(page, `${base.protocol}//${base.host}/`);
    expect(homepage.status).toBe(200);

    // --- 1. Two different bands, so one hardcoded route cannot pass ---
    // Files is a per-session md-serve, agent-chat the chat sidecar. Each is
    // reached through its proxy port, which is what the demuxer forwards to
    // and what checks the login cookie.
    for (const [band, port] of [['agent-chat', ports.agentChatProxyPort], ['files', ports.filesProxyPort]]) {
      const direct = `${base.protocol}//${base.hostname}:${port}/`;
      const wildcard = `${base.protocol}//${port}.${APEX}:${MAIN_PORT}/`;

      // The direct route is the control: if the pane is not up yet, the
      // comparison below would be green for the wrong reason.
      const viaPort = await waitForPane(page, direct, `${band} pane`);
      expect(viaPort.status, `${band} pane should answer on :${port}`).toBeLessThan(400);

      const viaHost = await pageSignature(page, wildcard);
      expect(viaHost.status, `${band} via ${port}.${APEX}`).toBe(viaPort.status);
      expect(viaHost.title, `${band} via ${port}.${APEX}`).toBe(viaPort.title);
      expect(viaHost.text, `${band} via ${port}.${APEX}`).toBe(viaPort.text);

      // ...and it is genuinely a different page from the one the apex serves,
      // i.e. the request was routed, not fallen through.
      expect(
        viaHost.title !== homepage.title || viaHost.text !== homepage.text,
        `${band} subdomain must not land on the homepage`,
      ).toBe(true);

      // One login covers every subdomain: the cookie is issued with
      // Domain=.{apex}. A bounce to the login form here would mean the cookie
      // scope regressed, and the pane would be unreachable in practice even
      // though the demuxer routed it correctly.
      expect(page.url(), `${band} must not bounce to the login page`).not.toContain('/swe-swe-auth/login');
    }

    // --- 2. The allowlist: a port swe-swe does not serve is refused ---
    // This is the reason the allowlist exists. A public wildcard that
    // forwarded to any 127.0.0.1:<n> would be a door into every service on
    // the box, swe-swe's own credential broker included. 1977 is swe-swe's
    // default port, 5432 a database, 22 ssh; none is in a session band.
    // Refusal is a 404 from the demuxer, before auth.
    for (const stranger of [22, 1977, 5432]) {
      const resp = await page.goto(`${base.protocol}//${stranger}.${APEX}:${MAIN_PORT}/`, {
        waitUntil: 'domcontentloaded',
        timeout: 30_000,
      });
      expect(resp.status(), `${stranger}.${APEX} must be refused`).toBe(404);
    }

    // --- 3. The server's own port falls through instead of looping ---
    // The landing page advertises "{server port}.{apex}"; proxying that to
    // ourselves would arrive with the same Host and match again, forever.
    const ownPort = await pageSignature(page, `${base.protocol}//${MAIN_PORT}.${APEX}:${MAIN_PORT}/`);
    expect(ownPort.status).toBe(200);
    expect(ownPort.title).toBe(homepage.title);

    // --- 4. A name that merely CONTAINS the apex is not a match ---
    // "{port}.{apex}.evil.test" must miss: the apex has to be the whole
    // remainder of the name, not a suffix of it. Sent as a Host header rather
    // than navigated, so no resolver rule is needed for the impostor domain.
    const evilResp = await page.request.get(`${base.protocol}//${base.host}/`, {
      headers: { Host: `${ports.filesProxyPort}.${APEX}.evil.test:${MAIN_PORT}` },
      maxRedirects: 0,
    });
    // Falls through to the main handler: the homepage, or its login redirect.
    // Anything but the Files pane.
    expect([200, 302, 401, 403]).toContain(evilResp.status());
    expect(await evilResp.text().catch(() => '')).not.toContain('md-serve');
  });
});
