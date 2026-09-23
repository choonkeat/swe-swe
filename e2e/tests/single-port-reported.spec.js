// Three problems reported from a real one-port box (Cloudflare Access in
// front, every pane reached through /proxy/{uuid}/...). Each test here
// reproduces one of them and is expected to FAIL until it is fixed:
//
//   1. An image uploaded in Agent Chat shows as a broken image in the bubble.
//   2. Preview never shows an app started after the pane was opened, while
//      opening the same app in its own browser tab works.
//   3. Files shows the agent's ad-hoc web app instead of the file list.
//
// Run it with `./scripts/e2e-up.sh single-port` then
// `./scripts/e2e-test.sh single-port tests/single-port-reported.spec.js`.

import { execSync } from 'child_process';
import { test, expect } from './_helpers/reaper.js';
import { endSessions, openSessionViaPost } from './_helpers/sessions.js';

const LAYOUT_STATE_KEY = 'swe-swe-layout-v1';
// The layout a first-time visitor gets: Agent Chat + Files on the left, Agent
// Terminal + Preview on the right. Pinned so a saved layout cannot hide a pane.
const CLASSIC_LAYOUT = {
  preset: 'classic',
  activeBySlot: {
    a: { tabs: ['agent-chat', 'files'], active: 'agent-chat' },
    b: { tabs: ['agent-terminal', 'preview'], active: 'agent-terminal' },
  },
};

// A 1x1 red PNG.
const PNG_1X1 = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFBQIAX8jx0gAAAABJRU5ErkJggg==',
  'base64'
);

const APP_MARKER = 'ADHOC-WEBAPP-MARKER';

function containerName() {
  const name = execSync(
    `docker ps --format "{{.Names}}" | grep "e2e-single-port" | grep "swe-swe" | head -1`
  ).toString().trim();
  if (!name) throw new Error('No e2e single-port swe-swe container found');
  return name;
}

// Serve a one-page web app from `dir` on `port`, the way an agent does when
// told to "start a web app on $PORT": an index.html in a folder, and a static
// server pointed at it. Returns a function that stops it and removes the page.
function startAdhocApp(dir, port) {
  const cn = containerName();
  execSync(`docker exec -u app ${cn} sh -c 'printf "<h1>${APP_MARKER}</h1>" > ${dir}/index.html'`);
  execSync(`docker exec -d -u app ${cn} sh -c 'cd ${dir} && exec python3 -m http.server ${port}'`);
  return () => {
    // Remove the page first: the "[h]" keeps pkill -f from matching (and
    // killing) this very sh -c, whose command line contains the pattern.
    execSync(`docker exec ${cn} sh -c 'rm -f ${dir}/index.html; pkill -f "[h]ttp.server ${port}"' || true`);
  };
}

async function openClassicSession(page) {
  await page.addInitScript(([key, layout]) => {
    try { localStorage.setItem(key, JSON.stringify(layout)); } catch {}
  }, [LAYOUT_STATE_KEY, CLASSIC_LAYOUT]);
  const uuid = await openSessionViaPost(page, { assistant: 'opencode', session: 'chat' });
  await page.locator('.terminal-ui__terminal').waitFor({ timeout: 40_000 });
  await page.waitForFunction(
    () => window.terminalUI && window.terminalUI.sessionUUID
      && window.terminalUI.previewPort && window.terminalUI.workDir,
    null,
    { timeout: 60_000 }
  );
  return uuid;
}

async function clickPaneTab(page, pane) {
  await page.locator(`.terminal-ui__slot-tab[data-pane="${pane}"] .terminal-ui__slot-tab-label`).click();
}

let testSessions = [];
let cleanups = [];

test.describe('single-port: reported pane problems', () => {
  test.skip(!process.env.E2E_SINGLE_PORT, 'needs a stack brought up with `./scripts/e2e-up.sh single-port`');

  test.beforeEach(async ({ page }) => {
    testSessions = [];
    cleanups = [];
    await page.setViewportSize({ width: 1600, height: 900 });
  });

  test.afterEach(async ({ page }, testInfo) => {
    for (const fn of cleanups) {
      try { fn(); } catch {}
    }
    if (testInfo.status === 'passed' && testSessions.length > 0) {
      await endSessions(page, testSessions);
    }
  });

  // Problem 1. agent-chat hands back "/uploads/<name>" for an upload and the
  // bubble uses it as-is. That is right when agent-chat owns the whole origin
  // (its own port), but here the chat page lives at /proxy/{uuid}/agentchat/,
  // so "/uploads/<name>" asks swe-swe's main page for the file instead.
  test('an image uploaded in Agent Chat displays in the chat bubble', async ({ page }) => {
    const uuid = await openClassicSession(page);
    testSessions.push(uuid);

    const chat = page.frameLocator('.terminal-ui__agent-chat-iframe');
    await expect(chat.locator('#chat-input')).toBeEnabled({ timeout: 120_000 });

    await chat.locator('#file-picker').setInputFiles({
      name: 'red-dot.png',
      mimeType: 'image/png',
      buffer: PNG_1X1,
    });
    // The chip stops spinning once the upload answered.
    await expect(chat.locator('#file-staging .uploading')).toHaveCount(0, { timeout: 30_000 });
    await chat.locator('#chat-input').fill('here is a picture');
    await chat.locator('#btn-send').click();

    const thumb = chat.locator('.file-attachments img.file-thumb').last();
    await expect(thumb).toBeVisible({ timeout: 30_000 });
    const img = await thumb.evaluate(async (el) => {
      if (!el.complete) await new Promise((r) => { el.onload = el.onerror = r; });
      return { src: el.src, width: el.naturalWidth };
    });
    expect(img.width, `image at ${img.src} did not load`).toBeGreaterThan(0);
  });

  // Problem 2. Before the app is up, the preview proxy answers with its own
  // "start a web app" page -- status 502, carrying swe-swe's marker header --
  // whose script polls and reloads once the app answers. The pane only loads
  // it after a readiness probe sees that marker. A gateway in front that shows
  // its own page for an origin's 502 (Cloudflare does) strips the marker, so
  // the probe keeps failing and, after 10 tries, gives up for good: the pane
  // sits on its placeholder even once the app is up. Opening the app in its
  // own tab works because that is a fresh load.
  //
  // The stand-in below does what such a gateway does: an origin 502/504 is
  // swapped for the gateway's own page, and "deny"/"script-src 'self'" are
  // added to documents that lack them (as in single-port.spec.js).
  test('Preview shows an app started after the pane was opened, behind a gateway that replaces 502 pages', async ({ page, context }) => {
    test.setTimeout(600_000);
    let previewProbes = 0;
    const gateway = async (route) => {
      const req = route.request();
      if (req.resourceType() === 'fetch' && /\/proxy\/[^/]+\/preview\/$/.test(req.url())) previewProbes++;
      const response = await route.fetch({ maxRedirects: 0 });
      const status = response.status();
      if (status === 502 || status === 504) {
        return route.fulfill({
          status,
          contentType: 'text/html',
          body: '<html><body><h1>Bad gateway</h1><p>Gateway error page</p></body></html>',
        });
      }
      if (route.request().resourceType() !== 'document') return route.fulfill({ response });
      const headers = response.headers();
      if (!('x-frame-options' in headers)) headers['x-frame-options'] = 'deny';
      if (!('content-security-policy' in headers)) headers['content-security-policy'] = "script-src 'self';";
      await route.fulfill({ response, headers });
    };
    // A request still in flight when its page goes away (a live-reload poll,
    // the app's own polling) has nothing left to fulfil; that is not a failure.
    await context.route('**/proxy/**', (route) => gateway(route).catch(() => {}));

    const uuid = await openClassicSession(page);
    testSessions.push(uuid);
    const { port, workDir } = await page.evaluate(() => ({
      port: window.terminalUI.previewPort,
      workDir: window.terminalUI.workDir,
    }));

    // Open Preview with nothing running yet, as when the agent is asked to
    // start the app after the session came up -- and take longer than the
    // pane is willing to wait: its readiness probe gives up after 10 tries
    // (about 4 minutes of backoff).
    await clickPaneTab(page, 'preview');
    await expect.poll(() => previewProbes, {
      message: 'Preview readiness probe never made its 10 attempts',
      timeout: 300_000,
      intervals: [5000],
    }).toBeGreaterThanOrEqual(10);
    // The last retry waits out its full 30s backoff; past that, it has quit.
    const probesSoFar = previewProbes;
    await page.waitForTimeout(45_000);
    expect(previewProbes, 'readiness probe still retrying; the app would start too early').toBe(probesSoFar + 1);
    await page.waitForTimeout(35_000);
    expect(previewProbes, 'readiness probe did not give up').toBe(probesSoFar + 1);

    const appDir = `${workDir}/.e2e-adhoc-app-${port}`;
    execSync(`docker exec -u app ${containerName()} mkdir -p ${appDir}`);
    cleanups.push(() => execSync(`docker exec ${containerName()} rm -rf ${appDir}`));
    cleanups.push(startAdhocApp(appDir, port));

    // The same app opened on its own (the pane's "open in new tab" address)
    // answers -- the proxy and the app are fine.
    await expect.poll(async () => page.evaluate(async (u) => {
      const r = await fetch(`/proxy/${u}/preview/`);
      return r.status === 200 && (await r.text()).includes('ADHOC-WEBAPP-MARKER');
    }, uuid), { timeout: 30_000, intervals: [1000] }).toBe(true);

    // ...but the pane has to show it too.
    await expect.poll(async () => {
      for (const f of page.frames()) {
        if (!f.url().includes(`/proxy/${uuid}/preview/`)) continue;
        try {
          const text = await f.evaluate(() => document.body ? document.body.innerText : '');
          if (text.includes(APP_MARKER)) return true;
        } catch {}
      }
      return false;
    }, { message: 'Preview pane never showed the app', timeout: 90_000, intervals: [2000] }).toBe(true);
  });

  // Problem 3. md-serve, which backs the Files pane, answers a folder that
  // holds an index.html with that page rather than the folder listing. An
  // agent asked for an ad-hoc web app typically writes index.html into the
  // project folder -- the very folder Files opens on -- so Files turns into
  // a second copy of the app.
  test('Files lists the project folder even when it holds a web app index.html', async ({ page }) => {
    const uuid = await openClassicSession(page);
    testSessions.push(uuid);
    const { port, workDir } = await page.evaluate(() => ({
      port: window.terminalUI.previewPort,
      workDir: window.terminalUI.workDir,
    }));
    cleanups.push(startAdhocApp(workDir, port));

    await clickPaneTab(page, 'files');
    const files = page.frameLocator('.terminal-ui__iframe[data-pane="files"]');
    await expect(files.locator('body')).not.toBeEmpty({ timeout: 60_000 });
    // Let the pane settle on whatever md-serve answers for the folder.
    await page.waitForTimeout(3000);
    const text = await files.locator('body').innerText();
    expect(text, 'Files pane shows the web app instead of the project folder').not.toContain(APP_MARKER);
    expect(text).toContain('index.html');
  });
});
