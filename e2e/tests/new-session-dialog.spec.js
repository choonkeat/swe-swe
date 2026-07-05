import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import crypto from 'crypto';
import { endSessions, openSessionViaPost } from './_helpers/sessions.js';

// New Session dialog behavior:
//   1. The dialog becomes interactive from local git refs alone -- the
//      remote `git fetch` runs in the background (via
//      /api/repo/branches?fetch=1) and its failure soft-fails into a
//      warning without ever disabling the dialog.
//   2. A recording's "+ New" button opens the dialog PRE-FILLED with the
//      recording's settings (assistant, repo, branch, name, extra args)
//      instead of creating a session directly.
//
// Scenarios cover the default workspace AND an external /repos checkout
// (whose recording's workdir is a /repos/.../worktrees/... path that must
// map back to the repo root), and both session modes (agent terminal and
// agent chat).

// Auth cookie comes from the suite-wide storageState (see playwright.config.js
// + global-setup.js); no per-test login is needed.

const EXTERNAL_REPO = '/repos/e2e-dialog-repo/workspace';

// Helper: get the e2e swe-swe container name (works for both simple and
// compose modes). Same lookup mcp-create-session.spec.js uses.
function getContainerName() {
  const name = execSync(
    `docker ps --format "{{.Names}}" | grep "e2e" | grep "swe-swe" | grep -v "traefik" | head -1`
  ).toString().trim();
  if (!name) throw new Error('No e2e swe-swe container found');
  return name;
}

// Create an external repo under /repos with one commit on main and an
// UNREACHABLE remote: any `git fetch` against it fails fast, which is
// exactly what the background-fetch soft-fail scenario needs.
function setupExternalRepo(containerName) {
  execSync(`docker exec ${containerName} sh -c '
    rm -rf /repos/e2e-dialog-repo &&
    mkdir -p ${EXTERNAL_REPO} &&
    cd ${EXTERNAL_REPO} &&
    git init -q -b main &&
    git config user.email e2e@test.invalid &&
    git config user.name e2e &&
    git commit -q --allow-empty -m init &&
    git remote add origin /repos/no-such-remote.git
  '`);
}

function removeExternalRepo(containerName) {
  execSync(`docker exec ${containerName} sh -c 'rm -rf /repos/e2e-dialog-repo'`);
}

async function openDialog(page) {
  await page.goto('/');
  await page.click('#btn-new-session');
}

// Select a Where option. The visible control is a <combo-box> that mirrors
// the hidden #new-session-mode select; committing a combo choice sets the
// select's value and fires change, so the test drives the select the same
// way. Waits for the option to exist first (dynamic /repos options arrive
// async via /api/repos).
async function selectWhere(page, value) {
  await page.waitForFunction((v) => {
    const sel = document.getElementById('new-session-mode');
    return !!sel && Array.from(sel.options).some((o) => o.value === v);
  }, value, { timeout: 15_000 });
  await page.evaluate((v) => {
    const sel = document.getElementById('new-session-mode');
    sel.value = v;
    sel.dispatchEvent(new Event('change'));
  }, value);
}

// Create a session with a unique display name, wait for it to materialize
// (terminal pane up, plus a beat so the `script` recording has flushed a
// .log), end it, then wait for its recording's "+ New" button to appear on
// the homepage. The recording is keyed by a fresh RecordingUUID -- NOT the
// session UUID -- so we locate the card by the unique name we passed, which
// the "+ New" button carries as data-name. Returns { name, button }.
async function recordingNewButton(page, sessionOpts) {
  const name = sessionOpts.name || `e2e-rec-${crypto.randomBytes(4).toString('hex')}`;
  const uuid = await openSessionViaPost(page, { ...sessionOpts, name });
  testSessions.push(uuid);
  await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
  // Give `script -c <agent>` a moment to create the .log; a recording is only
  // listed once its process has exited, so we also poll below.
  await page.waitForTimeout(3_000);
  await endSessions(page, [uuid]);

  // The name is unique per test run, so match the button by it directly.
  const btnSel = `[data-action="new-from-recording"][data-name="${name}"]`;
  await expect
    .poll(async () => {
      // A just-ended session page can fire its own redirect to "/", which
      // races our explicit navigation; tolerate the interruption and retry
      // on the next poll tick.
      try {
        await page.goto('/', { waitUntil: 'domcontentloaded' });
      } catch (e) {
        return 0;
      }
      return page.locator(btnSel).count();
    }, { timeout: 45_000, intervals: [1_000, 2_000, 3_000] })
    .toBe(1);
  return { name, button: page.locator(btnSel) };
}

// Per-test session tracker: afterEach ends them on pass; failed tests skip
// cleanup so broken state stays in the container for inspection (same
// convention as terminal-ui-tabs.spec.js).
let testSessions = [];

test.describe('new-session dialog', () => {
  test.beforeAll(() => {
    setupExternalRepo(getContainerName());
  });

  test.afterAll(() => {
    removeExternalRepo(getContainerName());
  });

  test.beforeEach(() => {
    testSessions = [];
  });

  test.afterEach(async ({ page }, testInfo) => {
    if (testInfo.status === 'passed' && testSessions.length > 0) {
      await endSessions(page, testSessions);
    }
  });

  test('dialog is interactive before the remote fetch completes (external repo)', async ({ page }) => {
    // Hold the background fetch=1 request so the test can PROVE the dialog
    // enabled itself while the remote fetch was still in flight.
    let releaseFetch;
    const held = new Promise((resolve) => { releaseFetch = resolve; });
    let fetchStarted = false;
    await page.route('**/api/repo/branches*', async (route) => {
      if (route.request().url().includes('fetch=1')) {
        fetchStarted = true;
        await held;
      }
      await route.continue();
    });

    await openDialog(page);
    await selectWhere(page, EXTERNAL_REPO);

    // Branch + agent selection enable from local refs alone.
    await expect(page.locator('#new-session-branch')).toBeEnabled({ timeout: 10_000 });
    await expect(page.locator('.dialog__agent--disabled')).toHaveCount(0);

    // Local branches are already listed and no warning is shown -- prepare
    // itself no longer fetches.
    const branches = await page.evaluate(() =>
      Array.from(document.querySelectorAll('#branch-list option')).map((o) => o.value)
    );
    expect(branches).toContain('main');
    await expect(page.locator('#new-session-warning')).toBeHidden();

    // The background refresh did start (the repo has a remote)...
    await expect.poll(() => fetchStarted, { timeout: 10_000 }).toBe(true);

    // ...and when it completes against the unreachable remote it soft-fails
    // into a warning, with the dialog still enabled.
    releaseFetch();
    await expect(page.locator('#new-session-warning')).toHaveText(
      /Using cached branches/, { timeout: 15_000 }
    );
    await expect(page.locator('#new-session-branch')).toBeEnabled();
  });

  test('default workspace prepares without a warning and lists branches', async ({ page }) => {
    await openDialog(page);
    await selectWhere(page, 'workspace');

    await expect(page.locator('#new-session-branch')).toBeEnabled({ timeout: 10_000 });
    await expect(page.locator('.dialog__agent--disabled')).toHaveCount(0);
    await expect(page.locator('#new-session-warning')).toBeHidden();
  });

  test('recording + New prefills the dialog; Start Agent Terminal reproduces the settings (external repo)', async ({ page }) => {
    const { name, button: btn } = await recordingNewButton(page, {
      assistant: 'opencode',
      branch: 'e2e-prefill',
      pwd: EXTERNAL_REPO,
      extra_args: '--from-recording',
    });

    // The button carries the recording's settings. pwd must be the repo
    // ROOT the dialog lists, not the /repos/.../worktrees/e2e-prefill
    // directory the session actually ran in.
    const ds = await btn.evaluate((el) => Object.assign({}, el.dataset));
    expect(ds.assistant).toBe('opencode');
    expect(ds.branch).toBe('e2e-prefill');
    expect(ds.pwd).toBe(EXTERNAL_REPO);
    expect(ds.extraArgs).toBe('--from-recording');
    expect(ds.name).toBe(name);

    // Clicking opens the dialog pre-filled with Start enabled -- no session
    // is created yet.
    await btn.click();
    await expect(page.locator('#new-session-start-terminal')).toBeEnabled({ timeout: 15_000 });
    await expect(page.locator('#new-session-start-chat')).toBeEnabled();
    expect(await page.locator('#new-session-mode').inputValue()).toBe(EXTERNAL_REPO);
    expect(await page.locator('#new-session-branch').inputValue()).toBe('e2e-prefill');
    await expect(page.locator('.dialog__agent--selected')).toHaveAttribute('data-agent', 'opencode');
    expect(await page.locator('#new-session-extra-args').inputValue()).toBe('--from-recording');

    // Start Agent Terminal creates the session with the same settings.
    await page.click('#new-session-start-terminal');
    await page.waitForURL(/\/session\/[a-f0-9-]{36}\?/, { timeout: 30_000 });
    const url = new URL(page.url());
    testSessions.push(url.pathname.split('/')[2]);
    expect(url.searchParams.get('assistant')).toBe('opencode');
    expect(url.searchParams.get('branch')).toBe('e2e-prefill');
    expect(url.searchParams.get('pwd')).toBe(EXTERNAL_REPO);
    expect(url.searchParams.get('extra_args')).toBe('--from-recording');
    expect(url.searchParams.get('name')).toBe(name);
    expect(url.searchParams.get('session')).toBeNull();
  });

  test('recording + New from an agent-chat session can start agent chat again (default workspace)', async ({ page }) => {
    const { button: btn } = await recordingNewButton(page, {
      assistant: 'opencode',
      session: 'chat',
      branch: 'e2e-chat-prefill',
    });

    // Default workspace: no pwd carried.
    const ds = await btn.evaluate((el) => Object.assign({}, el.dataset));
    expect(ds.assistant).toBe('opencode');
    expect(ds.branch).toBe('e2e-chat-prefill');
    expect(ds.pwd).toBeUndefined();

    await btn.click();
    await expect(page.locator('#new-session-start-chat')).toBeEnabled({ timeout: 15_000 });
    expect(await page.locator('#new-session-mode').inputValue()).toBe('workspace');
    expect(await page.locator('#new-session-branch').inputValue()).toBe('e2e-chat-prefill');

    await page.click('#new-session-start-chat');
    await page.waitForURL(/\/session\/[a-f0-9-]{36}\?/, { timeout: 30_000 });
    const url = new URL(page.url());
    testSessions.push(url.pathname.split('/')[2]);
    expect(url.searchParams.get('session')).toBe('chat');
    expect(url.searchParams.get('branch')).toBe('e2e-chat-prefill');
    expect(url.searchParams.get('pwd')).toBeNull();
  });
});
