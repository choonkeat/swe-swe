/**
 * Browser tests for the setup page (www/index.html).
 *
 * The page had none: every answer to "My browser can reach it by" changed the
 * generated script and which consequence boxes showed, with nothing checking
 * either. These drive the real page in a real browser -- the logic is inline
 * <script> in the HTML, so there is no module to unit-test.
 *
 * Run with: node --test www/setup-page.test.mjs   (or `make test-www`)
 *
 * Skips, rather than fails, when Playwright or a launchable chromium is
 * missing, so `make test` stays green on a machine set up only for Go.
 */

import { test, before, after } from 'node:test';
import assert from 'node:assert';
import { spawn } from 'node:child_process';
import net from 'node:net';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const CHROMIUM = process.env.CHROMIUM_BIN || '/usr/bin/chromium';

// index.mjs, not index.js: the CJS entry has no named exports.
let chromium = null;
try {
    const pw = await import(pathToFileURL(path.join(__dirname, '..', 'e2e', 'node_modules', 'playwright', 'index.mjs')).href);
    chromium = pw.chromium || (pw.default && pw.default.chromium) || null;
} catch {
    chromium = null;
}

async function freePort() {
    return new Promise((resolve, reject) => {
        const srv = net.createServer();
        srv.on('error', reject);
        srv.listen(0, '127.0.0.1', () => {
            const { port } = srv.address();
            srv.close(() => resolve(port));
        });
    });
}

let server = null;
let browser = null;
let base = '';
let skipReason = chromium ? '' : 'playwright not installed (run npm install in e2e/)';

before(async () => {
    if (skipReason) return;
    const port = await freePort();
    base = `http://127.0.0.1:${port}/`;
    server = spawn(process.execPath, [path.join(__dirname, 'serve.cjs')], {
        env: { ...process.env, PORT: String(port) },
        stdio: 'ignore',
    });
    // serve.cjs binds synchronously on start; poll until it answers.
    for (let i = 0; i < 50; i++) {
        try {
            const r = await fetch(base);
            if (r.ok) break;
        } catch { /* not up yet */ }
        await new Promise((r) => setTimeout(r, 100));
    }
    try {
        browser = await chromium.launch({ executablePath: CHROMIUM, args: ['--no-sandbox', '--disable-gpu'] });
    } catch (e) {
        skipReason = `no launchable chromium at ${CHROMIUM}: ${e.message}`;
    }
});

after(async () => {
    if (browser) await browser.close();
    if (server) server.kill();
});

// One page per test: the password is generated per load, so a test that
// compares two scripts has to compare them within the same load.
async function openPage() {
    const page = await browser.newPage();
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForFunction(() => !!document.getElementById('script-out').textContent);
    return page;
}

async function pick(page, value) {
    await page.click(`#reach-${value}`);
}

async function snapshot(page) {
    return page.evaluate(() => ({
        script: document.getElementById('script-out').textContent,
        note: document.getElementById('script-note').textContent,
        versionNote: !document.getElementById('version-note').hidden,
        wildcard: !document.getElementById('wildcard-yes').hidden,
        oneport: !document.getElementById('oneport-yes').hidden,
        tunnelSteps: !document.getElementById('tunnel-steps').hidden,
    }));
}

test('anyport: the plain four-line laptop script, no extra notes', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    const s = await snapshot(page);
    assert.match(s.script, /swe-swe up/);
    assert.ok(!s.script.includes('swe-swe init'), 'the ordinary path needs no init line');
    assert.ok(!s.script.includes('SWE_PUBLIC_HOSTNAME'), 'no wildcard hostname on the ordinary path');
    assert.strictEqual(s.versionNote, false);
    assert.strictEqual(s.wildcard, false);
    assert.strictEqual(s.oneport, false);
    assert.strictEqual(s.tunnelSteps, false);
    await page.close();
});

test('wildcard: adds SWE_PUBLIC_HOSTNAME and shows its own box', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    await pick(page, 'wildcard');
    const s = await snapshot(page);
    assert.match(s.script, /SWE_PUBLIC_HOSTNAME=/);
    assert.strictEqual(s.wildcard, true);
    assert.strictEqual(s.oneport, false);
    assert.strictEqual(s.tunnelSteps, false);
    await page.close();
});

test('tailscale: nothing added to the script, no consequence boxes', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    await pick(page, 'tailscale');
    const s = await snapshot(page);
    assert.ok(!s.script.includes('SWE_PUBLIC_HOSTNAME'));
    assert.strictEqual(s.wildcard, false);
    assert.strictEqual(s.oneport, false);
    assert.strictEqual(s.tunnelSteps, false);
    await page.close();
});

test('tunnel: the once-per-boot browser steps appear', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    await pick(page, 'tunnel');
    const s = await snapshot(page);
    assert.strictEqual(s.tunnelSteps, true);
    assert.strictEqual(s.wildcard, false);
    assert.strictEqual(s.oneport, false);
    await page.close();
});

// The new answer. swe-swe finds the reachable route by itself (every pane
// falls back to a same-origin path), so the script must be exactly the
// any-port one -- if it ever grows a flag, this catches it.
test('oneport: same script as anyport, plus the consequence box', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    const anyport = await snapshot(page);
    await pick(page, 'oneport');
    const s = await snapshot(page);

    assert.strictEqual(s.script, anyport.script, 'one fixed port needs no extra configuration');
    assert.strictEqual(s.note, anyport.note);
    assert.strictEqual(s.oneport, true, 'the consequence box must be shown');
    assert.strictEqual(s.wildcard, false);
    assert.strictEqual(s.tunnelSteps, false);
    await page.close();
});

test('oneport: the consequence box names what is lost, not just what works', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    await pick(page, 'oneport');
    const text = await page.evaluate(() => document.getElementById('oneport-yes').textContent);
    assert.match(text, /Agent View/, 'the live Agent View is the pane this answer turns on');
    assert.match(text, /without logging in|no-login|login-free/i, 'the public app address is the thing you give up');
    await page.close();
});

test('oneport on a host-native box: same script as anyport there too', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    await page.click('#have-agents');
    const anyport = await snapshot(page);
    await pick(page, 'oneport');
    const s = await snapshot(page);
    assert.strictEqual(s.script, anyport.script);
    await page.close();
});
