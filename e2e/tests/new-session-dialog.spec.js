import { test, expect } from './_helpers/reaper.js';
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

// A second external repo whose checkout sits on a NON-default branch, with no
// worktree/branch ever passed to the session. This mirrors the real dogfood
// sessions (e.g. "choonkeat/swe-swe@mcp-less"): they run directly in a checkout
// that happens to be on a feature branch, so the branch lives only in the git
// checkout -- never in the session's worktree-branch param. The recording's
// "+ New" prefill surfaces that branch as a HINT and leaves the Branch field
// blank, so starting again reuses the shared checkout instead of forcing a
// worktree on whichever branch the checkout happened to be on.
const DOGFOOD_REPO = '/repos/e2e-dogfood-repo/workspace';
const DOGFOOD_BRANCH = 'dogfood-branch';

// A repo whose remote is a long git URL. The Where dropdown renders each such
// repo as two lines -- a short "org/repo" primary label over the full URL on a
// dimmed detail line -- so long URLs wrap in full instead of clipping to an
// ellipsis, and the full URL stays matchable when filtering. The remote host
// ("gitlab.example.com") is deliberately absent from the short label so a
// filter on it can only match the detail line.
const LONGURL_REPO = '/repos/e2e-longurl-repo/workspace';
const LONGURL_REMOTE = 'git@gitlab.example.com:acme/payments-backend-service.git';
const LONGURL_SHORT = 'acme/payments-backend-service';

// A repo on an HTTPS remote: the case where the background branch refresh
// needs the browser's saved token, because the server holds no credentials
// outside a session.
const HTTPS_REPO = '/repos/e2e-https-repo/workspace';
const HTTPS_HOST = 'git.e2e.invalid';
const HTTPS_REMOTE = `https://${HTTPS_HOST}/acme/private.git`;

// Helper: get the e2e swe-swe container name (works for both simple and
// compose modes). Same lookup mcp-create-session.spec.js uses.
// A repo with one of each branch-card kind (sketch screen A): branches on this
// box (plain, with a folder, odd name), a leftover folder, and branches only on
// origin or only on upstream. The two "online copies" are local bare repos
// outside /repos so they don't show up under Where.
const CARDS_REPO = '/repos/e2e-cards-repo/workspace';
const CARDS_WORKTREES = '/repos/e2e-cards-repo/worktrees';

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

// Create an external repo checked out on a non-default branch. No remote is
// needed; the point is a checkout sitting on DOGFOOD_BRANCH so a session run
// directly in it records that branch nowhere but the checkout itself.
function setupDogfoodRepo(containerName) {
  execSync(`docker exec ${containerName} sh -c '
    rm -rf /repos/e2e-dogfood-repo &&
    mkdir -p ${DOGFOOD_REPO} &&
    cd ${DOGFOOD_REPO} &&
    git init -q -b main &&
    git config user.email e2e@test.invalid &&
    git config user.name e2e &&
    git commit -q --allow-empty -m init &&
    git checkout -q -b ${DOGFOOD_BRANCH}
  '`);
}

function removeDogfoodRepo(containerName) {
  execSync(`docker exec ${containerName} sh -c 'rm -rf /repos/e2e-dogfood-repo'`);
}

// Create a repo carrying a long git URL as its origin remote. Only the remote
// URL matters here (the Where dropdown reads it via /api/repos); the URL points
// nowhere, but listing the repo never fetches it, so it never hangs.
function setupLongUrlRepo(containerName) {
  execSync(`docker exec ${containerName} sh -c '
    rm -rf /repos/e2e-longurl-repo &&
    mkdir -p ${LONGURL_REPO} &&
    cd ${LONGURL_REPO} &&
    git init -q -b main &&
    git config user.email e2e@test.invalid &&
    git config user.name e2e &&
    git commit -q --allow-empty -m init &&
    git remote add origin ${LONGURL_REMOTE}
  '`);
}

// Create a repo whose origin is an HTTPS remote. Those are the ones that need
// a username/token, and the server has none of its own (credentials live per
// session, and the dialog runs before any session exists) -- so the browser
// must hand its saved token to the background refresh. The host resolves
// nowhere, so the fetch fails fast instead of hanging.
function setupHttpsRepo(containerName) {
  execSync(`docker exec ${containerName} sh -c '
    rm -rf /repos/e2e-https-repo &&
    mkdir -p ${HTTPS_REPO} &&
    cd ${HTTPS_REPO} &&
    git init -q -b main &&
    git config user.email e2e@test.invalid &&
    git config user.name e2e &&
    git commit -q --allow-empty -m init &&
    git remote add origin ${HTTPS_REMOTE}
  '`);
}

function removeHttpsRepo(containerName) {
  execSync(`docker exec ${containerName} sh -c 'rm -rf /repos/e2e-https-repo'`);
}

function setupCardsRepo(containerName) {
  execSync(`docker exec ${containerName} sh -c '
    rm -rf /repos/e2e-cards-repo /tmp/e2e-cards-origin.git /tmp/e2e-cards-upstream.git &&
    mkdir -p ${CARDS_REPO} &&
    cd ${CARDS_REPO} &&
    git init -q -b main &&
    git config user.email e2e@test.invalid &&
    git config user.name e2e &&
    git commit -q --allow-empty -m init &&
    git init -q --bare /tmp/e2e-cards-origin.git &&
    git init -q --bare /tmp/e2e-cards-upstream.git &&
    git push -q /tmp/e2e-cards-origin.git main main:foo &&
    git push -q /tmp/e2e-cards-upstream.git main:up-only &&
    git remote add origin /tmp/e2e-cards-origin.git &&
    git remote add upstream /tmp/e2e-cards-upstream.git &&
    git fetch -q --all &&
    git remote set-head origin main &&
    git branch feat-a &&
    git branch origin/typo &&
    git worktree add -q -b feat-b ${CARDS_WORKTREES}/feat-b &&
    mkdir -p ${CARDS_WORKTREES}/stray
  '`);
}

function removeCardsRepo(containerName) {
  execSync(`docker exec ${containerName} sh -c 'rm -rf /repos/e2e-cards-repo /tmp/e2e-cards-origin.git /tmp/e2e-cards-upstream.git'`);
}

// The branch card whose name is exactly `name` (not the workspace card).
function branchCard(page, name) {
  return page.locator('#branch-cards-sections button.branch-card', {
    has: page.locator('.branch-card__name', { hasText: new RegExp(`^${name.replace(/[/.]/g, '\\$&')}$`) }),
  });
}

async function openCardsRepo(page) {
  await openDialog(page);
  await selectWhere(page, CARDS_REPO);
  await expect(page.locator('#branch-card-workspace-slot .branch-card')).toBeVisible({ timeout: 10_000 });
}

function removeLongUrlRepo(containerName) {
  execSync(`docker exec ${containerName} sh -c 'rm -rf /repos/e2e-longurl-repo'`);
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
    // A real pick closes the Where list; setting the select directly leaves
    // it open over the fields below (Agent, Start), so close it the same way.
    const combo = document.getElementById('where-combo');
    if (combo && typeof combo._close === 'function') combo._close();
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
    const c = getContainerName();
    setupExternalRepo(c);
    setupDogfoodRepo(c);
    setupLongUrlRepo(c);
    setupHttpsRepo(c);
    setupCardsRepo(c);
  });

  test.afterAll(() => {
    const c = getContainerName();
    removeExternalRepo(c);
    removeDogfoodRepo(c);
    removeLongUrlRepo(c);
    removeHttpsRepo(c);
    removeCardsRepo(c);
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
      // The refreshing call is the POST (it carries the saved HTTPS token in
      // its body); the instant no-fetch listing is the GET.
      if (route.request().method() === 'POST') {
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

    // Local branches are already shown as cards and no warning is shown --
    // prepare itself no longer fetches.
    await expect(page.locator('#branch-card-workspace-slot .branch-card'))
      .toContainText('on: main');
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

  // The server runs the refresh outside any session, so it owns no
  // credentials: a private HTTPS remote used to fail every refresh with
  // "Using cached branches" even though git worked fine inside a session. The
  // browser holds the token (same localStorage entry Settings > Git writes),
  // so it hands it over on the refresh POST -- in the body, never the URL.
  test('the branch refresh carries the saved HTTPS token for the repo host', async ({ page }) => {
    await page.goto('/');
    await page.evaluate(([host, bag]) => {
      localStorage.setItem('swe-swe-creds:' + host, JSON.stringify(bag));
    }, [HTTPS_HOST, { username: 'e2e-user', token: 'e2e-secret-token' }]);

    const posts = [];
    await page.route('**/api/repo/branches*', async (route) => {
      const req = route.request();
      if (req.method() === 'POST') {
        posts.push({ url: req.url(), body: JSON.parse(req.postData() || '{}') });
      }
      await route.continue();
    });

    await openDialog(page);
    await selectWhere(page, HTTPS_REPO);
    await expect(page.locator('#new-session-branch')).toBeEnabled({ timeout: 10_000 });

    await expect.poll(() => posts.length, { timeout: 15_000 }).toBeGreaterThan(0);
    const post = posts[posts.length - 1];
    expect(post.body.path).toBe(HTTPS_REPO);
    expect(post.body.fetch).toBe(true);
    expect(post.body.credHost).toBe(HTTPS_HOST);
    expect(post.body.credUsername).toBe('e2e-user');
    expect(post.body.credToken).toBe('e2e-secret-token');
    // The token must never ride in the URL, where access logs would keep it.
    expect(post.url).not.toContain('e2e-secret-token');

    // The unreachable host still soft-fails into a warning, dialog usable.
    await expect(page.locator('#new-session-warning')).toHaveText(
      /Using cached branches/, { timeout: 20_000 }
    );
    await expect(page.locator('#new-session-branch')).toBeEnabled();

    await page.evaluate((host) => localStorage.removeItem('swe-swe-creds:' + host), HTTPS_HOST);
  });

  // A refresh landing mid-typing must not move the user: the text typed into
  // "+ New branch" stays, and so does what it picked.
  test('a background branch refresh does not disturb the branch box', async ({ page }) => {
    let releaseFetch;
    const held = new Promise((resolve) => { releaseFetch = resolve; });
    await page.route('**/api/repo/branches*', async (route) => {
      if (route.request().method() === 'POST') await held;
      await route.continue();
    });

    await openCardsRepo(page);
    await page.fill('#new-session-branch', 'my-new-idea');
    await expect(page.locator('#branch-new-card')).toHaveClass(/branch-card--picked/);

    const refreshed = page.waitForResponse((r) =>
      r.url().includes('/api/repo/branches') && r.request().method() === 'POST');
    releaseFetch();
    await refreshed;
    // Let the refresh re-draw the cards.
    await page.waitForTimeout(300);

    expect(await page.locator('#new-session-branch').inputValue()).toBe('my-new-idea');
    await expect(page.locator('#branch-new-card')).toHaveClass(/branch-card--picked/);
    await expect(page.locator('#branch-card-workspace-slot .branch-card')).toHaveAttribute('aria-pressed', 'false');
  });

  // Sketch screen A: one card per branch, grouped, tagged; picking a card and
  // pressing Start sends the same branch value the old dropdown sent.
  test('branch cards show every group, and Start sends the picked branch', async ({ page }) => {
    await openCardsRepo(page);

    const ws = page.locator('#branch-card-workspace-slot .branch-card');
    await expect(ws).toContainText('Workspace as it is');
    await expect(ws).toContainText('on: main');
    await expect(ws).toHaveAttribute('aria-pressed', 'true');

    const sections = page.locator('#branch-cards-sections .branch-cards__section');
    await expect(sections).toHaveText([
      'On this box (3)',
      'Leftover folders (1)',
      'Online only: origin (1)',
      'Online only: upstream (1)',
    ]);
    await expect(branchCard(page, 'feat-b')).toContainText('has folder');
    await expect(branchCard(page, 'origin/typo')).toContainText('odd name');
    await expect(page.locator('#branch-cards-sections button.branch-card:disabled'))
      .toContainText(`${CARDS_WORKTREES}/stray`);

    // Keyboard: from the workspace card, down goes to "+ New", then the
    // first branch card; Enter picks it.
    await ws.focus();
    await page.keyboard.press('ArrowDown');
    await expect(page.locator('#new-session-branch')).toBeFocused();
    await page.keyboard.press('ArrowDown');
    await expect(branchCard(page, 'feat-a')).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(branchCard(page, 'feat-a')).toHaveAttribute('aria-pressed', 'true');
    await expect(ws).toHaveAttribute('aria-pressed', 'false');

    await page.locator('.dialog__agent').first().click();
    await expect(page.locator('#new-session-start-chat')).toBeEnabled();
    await page.click('#new-session-start-chat');
    await page.waitForURL(/\/session\/[a-f0-9-]{36}\?/, { timeout: 30_000 });
    const url = new URL(page.url());
    testSessions.push(url.pathname.split('/')[2]);
    expect(url.searchParams.get('branch')).toBe('feat-a');
    expect(url.searchParams.get('pwd')).toBe(CARDS_REPO);
  });

  // Sketch screen D: "+ New branch" only ever makes something new.
  test('typing into + New branch picks existing cards and blocks online-copy names', async ({ page }) => {
    await openCardsRepo(page);
    await page.locator('.dialog__agent').first().click();
    const msg = page.locator('#branch-cards-msg');

    await page.fill('#new-session-branch', 'main');
    await expect(msg).toContainText('is the workspace as it is');
    await expect(page.locator('#branch-card-workspace-slot .branch-card')).toHaveAttribute('aria-pressed', 'true');

    await page.fill('#new-session-branch', 'feat-a');
    await expect(msg).toContainText('already on this box');
    await expect(branchCard(page, 'feat-a')).toHaveAttribute('aria-pressed', 'true');

    await page.fill('#new-session-branch', 'origin/foo');
    await expect(msg).toContainText('already online');
    await expect(branchCard(page, 'foo')).toBeVisible();
    await expect(branchCard(page, 'foo')).toHaveAttribute('aria-pressed', 'true');

    await page.fill('#new-session-branch', 'origin/bar');
    await expect(msg).toContainText("Names can't start with origin/ or upstream/");
    await expect(page.locator('#new-session-start-chat')).toBeDisabled();

    // A branch only on upstream is sent as upstream/<name>, so the server
    // tracks upstream's copy.
    await page.fill('#new-session-branch', 'up-only');
    await expect(msg).toContainText('already online');
    await expect(page.locator('#new-session-start-chat')).toBeEnabled();
    await page.click('#new-session-start-chat');
    await page.waitForURL(/\/session\/[a-f0-9-]{36}\?/, { timeout: 30_000 });
    const url = new URL(page.url());
    testSessions.push(url.pathname.split('/')[2]);
    expect(url.searchParams.get('branch')).toBe('upstream/up-only');
  });

  test('branch cards fit a phone-width screen', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await openCardsRepo(page);
    await page.locator('.branch-cards__online > summary').first().click();
    const overflow = await page.evaluate(() => {
      const cards = document.getElementById('branch-cards');
      return {
        page: document.documentElement.scrollWidth - document.documentElement.clientWidth,
        cards: cards.scrollWidth - cards.clientWidth,
      };
    });
    expect(overflow.page).toBeLessThanOrEqual(0);
    expect(overflow.cards).toBeLessThanOrEqual(0);
  });

  // A slow refresh must never gate session creation: Start works while the
  // fetch is still in flight, and the abandoned request cannot come back and
  // change anything.
  test('a slow branch refresh does not block Start', async ({ page }) => {
    let releaseFetch;
    const held = new Promise((resolve) => { releaseFetch = resolve; });
    await page.route('**/api/repo/branches*', async (route) => {
      if (route.request().method() === 'POST') await held;
      await route.continue();
    });

    await openDialog(page);
    await selectWhere(page, 'workspace');
    await expect(page.locator('#new-session-branch')).toBeEnabled({ timeout: 10_000 });

    // Pick an agent and start while the refresh is still held open. Agent
    // Chat is the only Start the dialog exposes (Agent Terminal is hidden).
    await page.locator('.dialog__agent').first().click();
    await expect(page.locator('#new-session-start-chat')).toBeEnabled({ timeout: 10_000 });
    await page.click('#new-session-start-chat');

    await page.waitForURL(/\/session\/[a-f0-9-]{36}\?/, { timeout: 30_000 });
    testSessions.push(new URL(page.url()).pathname.split('/')[2]);

    releaseFetch();
  });

  test('default workspace prepares without a warning and lists branches', async ({ page }) => {
    await openDialog(page);
    await selectWhere(page, 'workspace');

    await expect(page.locator('#new-session-branch')).toBeEnabled({ timeout: 10_000 });
    await expect(page.locator('.dialog__agent--disabled')).toHaveCount(0);
    await expect(page.locator('#new-session-warning')).toBeHidden();
  });

  test('recording + New prefills the dialog; Start reproduces the settings (external repo)', async ({ page }) => {
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
    // is created yet. Agent Chat is the only Start the dialog exposes; Agent
    // Terminal is in the DOM but `hidden` (see selection.html), so asserting on
    // it here would only test a button no user can reach.
    await btn.click();
    await expect(page.locator('#new-session-start-chat')).toBeEnabled({ timeout: 15_000 });
    await expect(page.locator('#new-session-start-terminal')).toBeHidden();
    expect(await page.locator('#new-session-mode').inputValue()).toBe(EXTERNAL_REPO);
    expect(await page.locator('#new-session-branch').inputValue()).toBe('e2e-prefill');
    await expect(page.locator('.dialog__agent--selected')).toHaveAttribute('data-agent', 'opencode');
    expect(await page.locator('#new-session-extra-args').inputValue()).toBe('--from-recording');

    // Start creates the session with the same settings.
    await page.click('#new-session-start-chat');
    await page.waitForURL(/\/session\/[a-f0-9-]{36}\?/, { timeout: 30_000 });
    const url = new URL(page.url());
    testSessions.push(url.pathname.split('/')[2]);
    expect(url.searchParams.get('assistant')).toBe('opencode');
    expect(url.searchParams.get('branch')).toBe('e2e-prefill');
    expect(url.searchParams.get('pwd')).toBe(EXTERNAL_REPO);
    expect(url.searchParams.get('extra_args')).toBe('--from-recording');
    expect(url.searchParams.get('name')).toBe(name);
    expect(url.searchParams.get('session')).toBe('chat');
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

  // Regression: real dogfood sessions run directly in a checkout on a feature
  // branch, passing NO worktree-branch param -- so the recording's
  // branch_name is empty and the branch survives only in the git checkout.
  // The "+ New" prefill must recover the branch from the checkout, otherwise
  // the dialog's Branch field opens blank (the reported bug).
  test('recording + New hints the checkout branch without forcing a worktree on it', async ({ page }) => {
    // No `branch` here: the session runs directly in the DOGFOOD_REPO checkout,
    // which is on DOGFOOD_BRANCH. This is exactly how the reported recording
    // ("choonkeat/swe-swe@mcp-less") was created.
    const { name, button: btn } = await recordingNewButton(page, {
      assistant: 'opencode',
      pwd: DOGFOOD_REPO,
    });

    // The "+ New" button carries the checkout's branch as a HINT only. Carrying
    // it as the value would turn "reuse the shared checkout" into "worktree on
    // DOGFOOD_BRANCH", which diverges the moment the checkout moves on.
    const ds = await btn.evaluate((el) => Object.assign({}, el.dataset));
    expect(ds.assistant).toBe('opencode');
    expect(ds.pwd).toBe(DOGFOOD_REPO);
    expect(ds.branch).toBeUndefined();
    expect(ds.branchHint).toBe(DOGFOOD_BRANCH);

    // Opening the dialog leaves Branch BLANK, with the checkout's branch shown
    // as a non-submitting placeholder.
    await btn.click();
    await expect(page.locator('#new-session-start-chat')).toBeEnabled({ timeout: 15_000 });
    expect(await page.locator('#new-session-mode').inputValue()).toBe(DOGFOOD_REPO);
    expect(await page.locator('#new-session-branch').inputValue()).toBe('');
    expect(await page.locator('#new-session-branch').getAttribute('placeholder'))
      .toBe(`Leave blank to reuse ${DOGFOOD_BRANCH}`);
    await expect(page.locator('.dialog__agent--selected')).toHaveAttribute('data-agent', 'opencode');

    // Start reproduces the shared checkout: no branch param is submitted, so
    // no worktree is created and the session runs in the repo root itself.
    await page.click('#new-session-start-chat');
    await page.waitForURL(/\/session\/[a-f0-9-]{36}\?/, { timeout: 30_000 });
    const url = new URL(page.url());
    testSessions.push(url.pathname.split('/')[2]);
    expect(url.searchParams.get('branch')).toBeNull();
    expect(url.searchParams.get('pwd')).toBe(DOGFOOD_REPO);
    expect(url.searchParams.get('name')).toBe(name);
  });

  // A repo with a long remote URL renders in the Where dropdown as two lines:
  // a short "org/repo" primary label over the FULL URL on a dimmed detail line.
  // This is what lets long git URLs show in full (wrapping) instead of clipping
  // to an ellipsis, keeps the full URL matchable while filtering, and draws a
  // divider between rows so wrapped multi-line options stay tellable apart.
  test('long git URLs render two-line (short name + full URL) and stay matchable', async ({ page }) => {
    await openDialog(page);

    // Wait for the dynamic /repos option AND its rendered combo-box row.
    await page.waitForFunction((repo) => {
      const sel = document.getElementById('new-session-mode');
      const combo = document.getElementById('where-combo');
      const opt = combo && combo.shadowRoot &&
        combo.shadowRoot.querySelector(`.option[data-value="${repo}"]`);
      return !!sel && Array.from(sel.options).some((o) => o.value === repo) && !!opt;
    }, LONGURL_REPO, { timeout: 15_000 });

    // The row is two-line: bold short name + dimmed full URL, with the WHOLE URL
    // present in the DOM (never truncated).
    const row = await page.evaluate((repo) => {
      const combo = document.getElementById('where-combo');
      const opt = combo.shadowRoot.querySelector(`.option[data-value="${repo}"]`);
      const name = opt.querySelector('.opt-name');
      const detail = opt.querySelector('.opt-detail');
      const cs = getComputedStyle(opt);
      return {
        name: name && name.textContent,
        detail: detail && detail.textContent,
        // A divider is drawn above non-first rows (workspace is first).
        borderTopWidth: cs.borderTopWidth,
      };
    }, LONGURL_REPO);

    expect(row.name).toBe(LONGURL_SHORT);
    expect(row.detail).toBe(LONGURL_REMOTE);
    expect(row.borderTopWidth).toBe('1px');

    // Filtering by a token that appears ONLY in the URL ("gitlab", absent from
    // the short label) still surfaces the repo -- the full URL is searchable.
    const filtered = await page.evaluate((repo) => {
      const combo = document.getElementById('where-combo');
      const input = combo.shadowRoot.querySelector('input');
      input.value = 'gitlab';
      input.dispatchEvent(new Event('input', { bubbles: true }));
      const rows = [...combo.shadowRoot.querySelectorAll('.option')];
      return {
        values: rows.map((o) => o.dataset.value),
        // The matched substring is highlighted inside the detail line.
        highlighted: !!rows
          .find((o) => o.dataset.value === repo)
          ?.querySelector('.opt-detail mark'),
      };
    }, LONGURL_REPO);

    expect(filtered.values).toEqual([LONGURL_REPO]);
    expect(filtered.highlighted).toBe(true);
  });
});
