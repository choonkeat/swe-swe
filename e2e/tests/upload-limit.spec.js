import { test, expect } from './_helpers/reaper.js';
import { openSessionViaPost } from './_helpers/sessions.js';

// Terminal paperclip uploads: streamed to disk in pieces, capped per file by
// SWE_SWE_UPLOAD_MAX_MB (upload_limit.go). The cap is set through the
// server-wide session env (/api/server/env), which applies to sessions
// spawned afterwards, and is always cleared again.

async function setServerEnv(page, raw) {
  const resp = await page.request.post('/api/server/env', { data: { raw } });
  expect(resp.ok()).toBeTruthy();
}

async function openShell(page) {
  await openSessionViaPost(page, { assistant: 'shell', session: 'terminal' });
  await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
  await page.waitForFunction(() => window.terminalUI && window.terminalUI.sessionUUID, null, { timeout: 60_000 });
  // Record every status notification for assertions.
  await page.evaluate(() => {
    window.__notes = [];
    const ui = window.terminalUI;
    const orig = ui.showStatusNotification.bind(ui);
    ui.showStatusNotification = (text, ms) => { window.__notes.push(text); return orig(text, ms); };
  });
}

async function attach(page, name, size) {
  await page.locator('.mobile-keyboard__file-input').setInputFiles({
    name, mimeType: 'application/octet-stream', buffer: Buffer.alloc(size, 7),
  });
}

async function waitForNote(page, re) {
  await expect.poll(() => page.evaluate(() => window.__notes.join('\n')), { timeout: 60_000 })
    .toMatch(re);
}

test.describe('terminal upload size limit', () => {
  test.afterEach(async ({ page }) => {
    await setServerEnv(page, '');
  });

  test('over the limit: refused in the page, and by the server', async ({ page }) => {
    await setServerEnv(page, 'SWE_SWE_UPLOAD_MAX_MB=1');
    await openShell(page);
    await page.waitForFunction(() => window.terminalUI.uploadMaxBytes === 1024 * 1024, null, { timeout: 10_000 });

    // Page refuses before sending anything.
    await attach(page, 'big.bin', 2 * 1024 * 1024);
    await waitForNote(page, /Too large: big\.bin is 2\.0 MB; the upload limit is 1\.0 MB/);

    // With the page check bypassed, the server refuses at upload_start.
    await page.evaluate(() => { window.terminalUI.uploadMaxBytes = 0; });
    await attach(page, 'big2.bin', 2 * 1024 * 1024);
    await waitForNote(page, /Upload failed: big2\.bin is too large \(2\.0 MB\); the upload limit is 1\.0 MB \(SWE_SWE_UPLOAD_MAX_MB\)/);
  });

  test('no limit: a multi-piece file streams through and is saved', async ({ page }) => {
    await openShell(page);
    await page.waitForFunction(() => window.terminalUI.uploadMaxBytes === 0, null, { timeout: 10_000 });

    // 3.5 MB = four 1 MB pieces.
    await attach(page, 'streamed.bin', 3.5 * 1024 * 1024);
    await waitForNote(page, /Saved: streamed\.bin/);
    // The server types the saved path into the terminal.
    await expect.poll(() => page.evaluate(() => {
      const buf = window.terminalUI.term.buffer.active;
      let out = '';
      for (let i = 0; i < buf.length; i++) out += buf.getLine(i).translateToString(true);
      return out;
    }), { timeout: 10_000 }).toContain('.swe-swe/uploads/streamed.bin');
  });
});
