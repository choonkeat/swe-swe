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

// swe-swe would FIND the same-origin route by itself here, so this answer was
// once the plain script. It now hands out SWE_SINGLE_PORT=1, which is the
// difference between finding the route and being told: nothing binds the
// per-session proxy ports, and no pane waits out a reachability probe first.
test('oneport: hands out --single-port, which anyport does not', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    const anyport = await snapshot(page);
    assert.ok(!anyport.script.includes('--single-port'), 'a box where every port works must not be told otherwise');

    await pick(page, 'oneport');
    const s = await snapshot(page);
    assert.match(s.script, /swe-swe init --single-port/);
    assert.strictEqual(s.oneport, true, 'the consequence box must be shown');
    assert.strictEqual(s.wildcard, false);
    assert.strictEqual(s.tunnelSteps, false);
    await page.close();
});

// The setting only reaches the server through `swe-swe up`, and on a box that
// was never initialised there is nothing for it to reach. So the answer has to
// carry the init line too, which the plain laptop script does not have.
// The flag belongs to init, not to the run: it decides what the generated
// setup forwards. Setting it at run time would leave a hundred ports
// forwarded to listeners that are no longer there.
test('oneport: the flag is on the init line, not the run line', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    await pick(page, 'oneport');
    const s = await snapshot(page);
    const initLine = s.script.split('\n').find((l) => l.includes('swe-swe init'));
    assert.ok(initLine && initLine.includes('--single-port'), `init line carries the flag, got: ${initLine}`);
    assert.ok(!s.script.includes('SWE_SINGLE_PORT='), 'no run-time variable is needed once init knows');
    await page.close();
});

test('oneport: the consequence box names what is lost, not just what works', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    await pick(page, 'oneport');
    const text = await page.evaluate(() => document.getElementById('oneport-yes').textContent);
    assert.match(text, /Agent View/, 'the live Agent View is the pane this answer turns on');
    assert.match(text, /without logging in|no-login|login-free/i, 'the public app address is the thing you give up');
    assert.match(text, /neither forwarded nor opened|not (opened|bound)/i, 'the flag is what stops the extra ports existing at all');
    await page.close();
});

test('oneport on a host-native box: the flag rides along there too', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    await page.click('#have-agents');
    await pick(page, 'oneport');
    const s = await snapshot(page);
    assert.match(s.script, /--single-port/);
    assert.match(s.script, /--runtime=host/, 'the host-native init flag is still there');
    await page.close();
});

// "Latest code on GitHub" builds swe-swe from the main branch instead of
// fetching the published release, so a fix can be tried before it is released.
async function pickSource(page, source) {
    await page.click(`#source-mode button[data-source="${source}"]`);
}

test('release is the default: the published package, no build from GitHub', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    await pick(page, 'oneport');
    const s = await snapshot(page);
    assert.match(s.script, /npx -y swe-swe/);
    assert.ok(!s.script.includes('git clone'), 'nothing is built by default');
    assert.strictEqual(s.versionNote, true);
    await page.close();
});

test('latest code on GitHub with Docker: Docker builds swe-swe from main', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    await pick(page, 'oneport');
    await pickSource(page, 'head');
    const s = await snapshot(page);
    assert.ok(!s.script.includes('npx -y swe-swe'), 'the published package is not used');
    assert.match(s.script, /docker run [\s\S]*golang:/, 'the build runs inside a Go image');
    assert.match(s.script, /git clone [^\n]*https:\/\/github\.com\/choonkeat\/swe-swe/);
    assert.match(s.script, /go build [^\n]*\.\/cmd\/swe-swe/);
    assert.match(s.script, /swe-swe init --single-port/, 'the answers still shape the rest');
    assert.strictEqual(s.versionNote, false, 'main is always new enough');
    const headNote = await page.evaluate(() => !document.getElementById('head-note').hidden);
    assert.strictEqual(headNote, true, 'says what "latest code" means');
    await page.close();
});

test('latest code on GitHub on the ordinary laptop path still builds from main', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    await pickSource(page, 'head');
    const s = await snapshot(page);
    assert.ok(!s.script.includes('npx -y swe-swe'));
    assert.match(s.script, /go build/);
    assert.match(s.script, /swe-swe up/);
    await page.close();
});

test('latest code on GitHub without Docker: built with the local Go', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    await page.click('#have-agents');
    await pickSource(page, 'head');
    const s = await snapshot(page);
    assert.ok(!s.script.includes('docker run'), 'no Docker on this path');
    assert.ok(!s.script.includes('install.sh'), 'the published download is not used');
    assert.match(s.script, /git clone [^\n]*choonkeat\/swe-swe/);
    assert.match(s.script, /go build [^\n]*\.\/cmd\/swe-swe/);
    const note = await page.evaluate(() => document.getElementById('head-note').textContent);
    assert.match(note, /Go 1\.2\d/, 'names the Go it needs');
    await page.close();
});

test('latest code on GitHub in a start-up script: no published download either', async (t) => {
    if (skipReason) return t.skip(skipReason);
    const page = await openPage();
    await pickSource(page, 'head');
    await page.click('#run-mode button[data-mode="startup"]');
    const s = await snapshot(page);
    assert.ok(!s.script.includes('install.sh'));
    assert.match(s.script, /go build/);
    assert.match(s.script, /nohup swe-swe up/);
    await page.close();
});
