import { test, expect } from './_helpers/reaper.js';
import { execSync } from 'child_process';
import crypto from 'crypto';
import { endSessions, openSessionViaPost } from './_helpers/sessions.js';

// New Session dialog behavior:
//   1. Opening the dialog only lists branches from local git refs: no
//      download from the remote and no per-branch checks. Picking a branch
//      card checks that one branch and offers Delete.
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

// A repo on an HTTPS remote: the case where downloading an online section
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
// must hand its saved token to the section download. The host resolves
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
    git init -q --bare -b main /tmp/e2e-cards-origin.git &&
    git init -q --bare -b main /tmp/e2e-cards-upstream.git &&
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

// The whole card row (pick button, Delete line, confirm line) for branch `name`.
function cardRow(page, name) {
  const exact = new RegExp(`^${name.replace(/[/.]/g, '\\$&')}$`);
  return page.locator('#branch-cards-sections .branch-card-row', {
    has: page.locator('button.branch-card .branch-card__name', { hasText: exact }),
  });
}

// Run a shell command inside the cards repo, in the test container.
function inCards(cmd) {
  return execSync(`docker exec ${getContainerName()} sh -c 'cd ${CARDS_REPO} && ${cmd}'`).toString().trim();
}

function cardsBranchExists(name) {
  try {
    inCards(`git rev-parse --verify -q refs/heads/${name}`);
    return true;
  } catch (e) {
    return false;
  }
}

function cardsPathExists(path) {
  try {
    inCards(`test -e ${path}`);
    return true;
  } catch (e) {
    return false;
  }
}

// A session only materializes once its page connects; wait until the server
// sees it, via the card facts the dialog itself reads.
async function waitUntilInUse(page, kind, name) {
  await expect.poll(async () => {
    const r = await page.request.get('/api/repo/branches?path=' + encodeURIComponent(CARDS_REPO));
    const body = await r.json();
    const card = (body.cards || []).find((c) => c.kind === kind && (kind === 'workspace' || c.name === name));
    return !!(card && card.inUse);
  }, { timeout: 45_000, intervals: [500, 1_000, 2_000] }).toBe(true);
}

// Remove a branch and its folder even if a just-ended session still sits in it.
function dropCardsBranch(name) {
  inCards(`git worktree remove --force --force ${CARDS_WORKTREES}/${name} 2>/dev/null; rm -rf ${CARDS_WORKTREES}/${name}; git worktree prune; git branch -D ${name} 2>/dev/null; true`);
}

// dropCardsBranch for a branch a session just used: the ended session's
// processes linger a second or two in the folder, which can make the first
// removal miss. Retry until the branch is really gone.
async function dropSessionBranch(name) {
  await expect.poll(() => {
    dropCardsBranch(name);
    return cardsBranchExists(name) || cardsPathExists(`${CARDS_WORKTREES}/${name}`);
  }, { timeout: 15_000, intervals: [500, 1_000, 2_000] }).toBe(false);
}

// Pick a local branch card, then press its Delete.
async function pickAndDelete(page, name) {
  await branchCard(page, name).click();
  await cardRow(page, name).getByRole('button', { name: 'Delete ' + name, exact: true }).click();
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

  // A large repo exhausted a box's open files just by opening this dialog:
  // it downloaded from the remote and checked every branch (~180 git
  // commands). Opening now only lists branches.
  test('opening the dialog neither downloads nor checks branches', async ({ page }) => {
    const calls = [];
    page.on('request', (req) => {
      const u = req.url();
      if (u.includes('/api/repo/branch-check') || (u.includes('/api/repo/branches') && req.method() === 'POST')) {
        calls.push(req.method() + ' ' + u);
      }
    });
    await openCardsRepo(page);
    await expect(branchCard(page, 'feat-a')).toBeVisible();
    await expect(page.locator('.dialog__agent--disabled')).toHaveCount(0);
    await page.waitForTimeout(1000);
    expect(calls).toEqual([]);
    // No [x] and no count badges on branch cards.
    await expect(cardRow(page, 'feat-a').locator('.branch-card__x')).toHaveCount(0);
    await expect(page.locator('.branch-card__count')).toHaveCount(0);
  });

  // Before the tests that start sessions: those can leave branches behind
  // in the cards repo, which would change the count.
  test('many branches show a tip to ask the agent to clean up', async ({ page }) => {
    await openCardsRepo(page);
    await expect(page.locator('.branch-cards__tip')).toHaveCount(0);
    inCards('for i in 1 2 3 4 5 6; do git branch tip-$i; done');
    try {
      await openCardsRepo(page);
      const tip = page.locator('#branch-cards-sections .branch-cards__tip');
      await expect(tip).toContainText('10 branches and folders here.');
      await expect(tip.locator('.branch-cards__tip-prompt-text')).toHaveText("Discuss: which worktrees & branches can we clean up?");
      await page.context().grantPermissions(['clipboard-read', 'clipboard-write']).catch(() => {});
      const copy = tip.getByRole('button', { name: 'Copy the clean-up prompt' });
      await copy.click();
      await expect(copy).toHaveText('Copied');
      const copied = await page.evaluate(() => navigator.clipboard ? navigator.clipboard.readText().catch(() => null) : null);
      if (copied !== null) expect(copied).toBe("Discuss: which worktrees & branches can we clean up?");
    } finally {
      inCards('for i in 1 2 3 4 5 6; do git branch -D tip-$i; done 2>/dev/null; true');
    }
  });

  // --- Online sections download on open; new names are checked online ---

  function onlineSection(page, remote) {
    return page.locator('details.branch-cards__online', { hasText: 'Online only: ' + remote });
  }

  test('opening an online section downloads that remote only', async ({ page }) => {
    inCards('git push -q /tmp/e2e-cards-origin.git main:fresh-on-origin && git push -q /tmp/e2e-cards-upstream.git main:fresh-on-upstream');
    const posts = [];
    page.on('request', (req) => {
      if (req.url().includes('/api/repo/branches') && req.method() === 'POST') posts.push(JSON.parse(req.postData() || '{}'));
    });
    try {
      await openCardsRepo(page);
      await expect(onlineSection(page, 'origin').locator('summary')).toHaveText('Online only: origin (1)');
      await onlineSection(page, 'origin').locator('summary').click();
      await expect(onlineSection(page, 'origin').locator('summary')).toHaveText('Online only: origin (2)');
      await expect(branchCard(page, 'fresh-on-origin')).toBeVisible();
      expect(posts.map((p) => p.remote)).toEqual(['origin']);
      // Closing and reopening does not download again.
      await onlineSection(page, 'origin').locator('summary').click();
      await onlineSection(page, 'origin').locator('summary').click();
      await page.waitForTimeout(300);
      expect(posts.length).toBe(1);
      await expect(onlineSection(page, 'upstream').locator('summary')).toHaveText('Online only: upstream (1)');
    } finally {
      inCards('git push -q /tmp/e2e-cards-origin.git :fresh-on-origin; git push -q /tmp/e2e-cards-upstream.git :fresh-on-upstream; git update-ref -d refs/remotes/origin/fresh-on-origin; git update-ref -d refs/remotes/upstream/fresh-on-upstream; true');
    }
  });

  test('a failed section download says so inside the section, with Retry', async ({ page }) => {
    let fail = true;
    await page.route('**/api/repo/branches', async (route) => {
      if (route.request().method() === 'POST' && fail) return route.abort();
      return route.continue();
    });
    await openCardsRepo(page);
    const section = onlineSection(page, 'upstream');
    await section.locator('summary').click();
    await expect(section.locator('.branch-card__line--error')).toHaveText(/Couldn't reach upstream\./);
    await expect(page.locator('#new-session-warning')).toBeHidden();
    fail = false;
    await section.getByRole('button', { name: 'Retry' }).click();
    await expect(section.locator('.branch-card__line--error')).toHaveCount(0);
  });

  // The server runs the download outside any session, so it owns no
  // credentials: the browser hands over its saved token -- in the body,
  // never the URL, where access logs would keep it.
  test('an online section download carries the saved HTTPS token in the body', async ({ page }) => {
    await page.goto('/');
    await page.evaluate(([host, bag]) => {
      localStorage.setItem('swe-swe-creds:' + host, JSON.stringify(bag));
    }, [HTTPS_HOST, { username: 'e2e-user', token: 'e2e-secret-token' }]);
    const posts = [];
    page.on('request', (req) => {
      if (req.url().includes('/api/repo/branches') && req.method() === 'POST') {
        posts.push({ url: req.url(), body: JSON.parse(req.postData() || '{}') });
      }
    });
    try {
      await openDialog(page);
      await selectWhere(page, HTTPS_REPO);
      const section = onlineSection(page, 'origin');
      await expect(section.locator('summary')).toHaveText('Online only: origin (0)', { timeout: 10_000 });
      await section.locator('summary').click();
      await expect(section.locator('.branch-card__line--error')).toContainText("Couldn't reach origin.", { timeout: 20_000 });
      expect(posts.length).toBe(1);
      expect(posts[0].body).toMatchObject({ path: HTTPS_REPO, fetch: true, remote: 'origin',
        credHost: HTTPS_HOST, credUsername: 'e2e-user', credToken: 'e2e-secret-token' });
      expect(posts[0].url).not.toContain('e2e-secret-token');
      await expect(section.getByRole('button', { name: 'Retry' })).toBeVisible();
    } finally {
      await page.evaluate((host) => localStorage.removeItem('swe-swe-creds:' + host), HTTPS_HOST);
    }
  });

  test('a new name that is already online picks the online branch instead', async ({ page }) => {
    inCards('git push -q /tmp/e2e-cards-origin.git main:was-online');
    try {
      await openCardsRepo(page);
      await page.fill('#new-session-branch', 'was-online');
      await expect(page.locator('#branch-new-card')).toHaveClass(/branch-card--picked/);
      await page.locator('.dialog__agent').first().click();
      await page.click('#new-session-start-chat');
      await expect(page.locator('#branch-cards-msg')).toContainText('"was-online" is already on origin.');
      await expect(branchCard(page, 'was-online')).toHaveAttribute('aria-pressed', 'true');
      expect(page.url()).not.toMatch(/\/session\//);
    } finally {
      inCards('git push -q /tmp/e2e-cards-origin.git :was-online; git update-ref -d refs/remotes/origin/was-online; true');
    }
  });

  // Only the card the user stops on is checked; picking another drops the
  // check still running.
  test('picking a card checks that branch only; picking another drops it', async ({ page }) => {
    inCards(`git worktree add -q -b pick-sized ${CARDS_WORKTREES}/pick-sized && head -c 2000000 /dev/zero > ${CARDS_WORKTREES}/pick-sized/big.bin`);
    const checks = [];
    let releaseFirst;
    const held = new Promise((resolve) => { releaseFirst = resolve; });
    await page.route('**/api/repo/branch-check', async (route) => {
      const body = JSON.parse(route.request().postData() || '{}');
      checks.push(body.branch);
      if (checks.length === 1) await held;
      await route.continue().catch(() => {});
    });
    try {
      await openCardsRepo(page);
      await branchCard(page, 'feat-a').click();
      await expect(cardRow(page, 'feat-a')).toContainText('Checking what deleting would lose...');
      await branchCard(page, 'pick-sized').click();
      const row = cardRow(page, 'pick-sized');
      // big.bin is a new file, so it counts as an unsaved edit.
      await expect(row).toContainText('1 unsaved edit in its folder.');
      await expect(row).toContainText(/Folder is 2\.\d MB\./);
      await expect(row.getByRole('button', { name: 'Delete pick-sized', exact: true })).toBeVisible();
      releaseFirst();
      await page.waitForTimeout(300);
      // The dropped check never draws on the card that is no longer picked.
      await expect(cardRow(page, 'feat-a').locator('.branch-card__line')).toHaveCount(0);
      expect(checks).toEqual(['feat-a', 'pick-sized']);
    } finally {
      releaseFirst();
      inCards(`git worktree remove --force ${CARDS_WORKTREES}/pick-sized 2>/dev/null; git branch -D pick-sized 2>/dev/null; true`);
    }
  });

  // Sketch screen A: one card per branch, grouped, tagged; picking a card and
  // pressing Start sends the same branch value the old dropdown sent.
  test('branch cards show every group, and Start sends the picked branch', async ({ page }) => {
    await openCardsRepo(page);

    const ws = page.locator('#branch-card-workspace-slot .branch-card');
    await expect(ws).toContainText('Workspace (main branch)');
    await expect(ws).toHaveAttribute('aria-pressed', 'true');

    const sections = page.locator('#branch-cards-sections .branch-cards__section');
    await expect(sections).toHaveText([
      'Leftover folders (1)',
      'Online only: origin (1)',
      'Online only: upstream (1)',
    ]);
    await expect(branchCard(page, 'origin/typo')).toContainText('odd name');
    await expect(page.locator('#branch-cards-sections button.branch-card:disabled'))
      .toContainText(`${CARDS_WORKTREES}/stray`);

    // Order: workspace, "+ New" (it filters what follows), then the
    // branches, leftovers and online groups.
    const order = await page.evaluate(() => {
      const newCard = document.getElementById('branch-new-card');
      const slot = document.getElementById('branch-card-workspace-slot');
      const sections = document.getElementById('branch-cards-sections');
      return {
        newAfterWorkspace: !!(slot.compareDocumentPosition(newCard) & Node.DOCUMENT_POSITION_FOLLOWING),
        newBeforeBranches: !!(newCard.compareDocumentPosition(sections) & Node.DOCUMENT_POSITION_FOLLOWING),
        firstBranch: sections.querySelector('.branch-card__name').textContent,
      };
    });
    expect(order).toEqual({ newAfterWorkspace: true, newBeforeBranches: true, firstBranch: 'feat-a' });

    // Keyboard: from the workspace card, down goes to "+ New", then to the
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
    await expect(msg).toContainText("is the workspace's branch");
    await expect(page.locator('#branch-card-workspace-slot .branch-card')).toHaveAttribute('aria-pressed', 'true');

    await page.fill('#new-session-branch', 'feat-a');
    await expect(msg).toContainText('already on this box');
    await expect(branchCard(page, 'feat-a')).toHaveAttribute('aria-pressed', 'true');

    // Typing filters the lists; the workspace card always stays.
    await page.fill('#new-session-branch', 'feat');
    await expect(branchCard(page, 'feat-a')).toBeVisible();
    await expect(branchCard(page, 'feat-b')).toBeVisible();
    await expect(branchCard(page, 'origin/typo')).toHaveCount(0);
    await expect(page.locator('#branch-card-workspace-slot .branch-card')).toBeVisible();
    await expect(msg).toContainText('Start makes a new branch "feat".');
    await page.fill('#new-session-branch', '');
    await expect(page.locator('#branch-cards-sections .branch-cards__section').first()).toBeVisible();

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


  // --- Pick, Delete, confirm, Undo, Switch back (sketch screens B, C, E) ---

  test('Delete removes a plain branch; Undo brings it back and it can still start a session', async ({ page }) => {
    inCards('git branch del-plain');
    try {
      await openCardsRepo(page);
      await pickAndDelete(page, 'del-plain');
      const strip = page.locator('.branch-card-row--deleted', { hasText: 'Deleted del-plain' });
      await expect(strip).toBeVisible();
      expect(cardsBranchExists('del-plain')).toBe(false);

      await strip.getByRole('button', { name: 'Undo' }).click();
      await expect(branchCard(page, 'del-plain')).toBeVisible();
      expect(cardsBranchExists('del-plain')).toBe(true);

      await branchCard(page, 'del-plain').click();
      await page.locator('.dialog__agent').first().click();
      await page.click('#new-session-start-chat');
      await page.waitForURL(/\/session\/[a-f0-9-]{36}\?/, { timeout: 30_000 });
      const url = new URL(page.url());
      testSessions.push(url.pathname.split('/')[2]);
      expect(url.searchParams.get('branch')).toBe('del-plain');
    } finally {
      await endSessions(page, testSessions.splice(0));
      dropCardsBranch('del-plain');
    }
  });

  test('Delete on a branch with a folder removes the folder and the branch', async ({ page }) => {
    inCards(`git worktree add -q -b del-folder ${CARDS_WORKTREES}/del-folder`);
    await openCardsRepo(page);
    await expect(branchCard(page, 'del-folder')).toBeVisible();
    await pickAndDelete(page, 'del-folder');
    await expect(page.locator('.branch-card-row--deleted', { hasText: 'Deleted del-folder' })).toBeVisible();
    expect(cardsPathExists(`${CARDS_WORKTREES}/del-folder`)).toBe(false);
    expect(cardsBranchExists('del-folder')).toBe(false);
  });

  test('saved work that exists nowhere else asks inside the card; Keep leaves it', async ({ page }) => {
    inCards('git checkout -q -b del-lose && git commit -q --allow-empty -m only-here && git checkout -q main');
    try {
      await openCardsRepo(page);
      await branchCard(page, 'del-lose').click();
      const row = cardRow(page, 'del-lose');
      await expect(row).toContainText('1 saved change exists only here.');
      await row.getByRole('button', { name: 'Delete del-lose', exact: true }).click();
      const confirm = row.locator('.branch-card__line--confirm');
      await expect(confirm).toContainText('1 saved change(s) exist only on this branch.');

      await confirm.getByRole('button', { name: 'Keep' }).click();
      await expect(confirm).toHaveCount(0);
      expect(cardsBranchExists('del-lose')).toBe(true);

      await row.getByRole('button', { name: 'Delete del-lose', exact: true }).click();
      await row.locator('.branch-card__line--confirm').getByRole('button', { name: 'Delete anyway' }).click();
      const strip = page.locator('.branch-card-row--deleted', { hasText: 'Deleted del-lose' });
      await expect(strip.getByRole('button', { name: 'Undo' })).toBeVisible();
      expect(cardsBranchExists('del-lose')).toBe(false);
    } finally {
      inCards('git branch -D del-lose 2>/dev/null; true');
    }
  });

  test('picking a branch in use by a live session says why it has no Delete', async ({ page }) => {
    const uuid = await openSessionViaPost(page, { branch: 'busy-x', pwd: CARDS_REPO });
    testSessions.push(uuid);
    try {
      await waitUntilInUse(page, 'local', 'busy-x');
      await openCardsRepo(page);
      const row = cardRow(page, 'busy-x');
      await expect(branchCard(page, 'busy-x')).toContainText('in use');
      await branchCard(page, 'busy-x').click();
      await expect(row.locator('.branch-card__line')).toHaveText('A live session is using this branch.');
      await expect(row.getByRole('button', { name: 'Delete busy-x', exact: true })).toHaveCount(0);
      expect(cardsBranchExists('busy-x')).toBe(true);
    } finally {
      await endSessions(page, testSessions.splice(0));
      dropCardsBranch('busy-x');
    }
  });

  test('a leftover folder is removed after confirming, with no Undo', async ({ page }) => {
    inCards(`mkdir -p ${CARDS_WORKTREES}/stray2`);
    await openCardsRepo(page);
    const row = page.locator('#branch-cards-sections .branch-card-row', { hasText: `${CARDS_WORKTREES}/stray2` });
    await row.locator('.branch-card__x').click();
    const confirm = row.locator('.branch-card__line--confirm');
    await expect(confirm).toContainText("can't be undone");
    await confirm.getByRole('button', { name: 'Delete anyway' }).click();
    await expect(page.locator('.branch-card-row--deleted', { hasText: `Removed ${CARDS_WORKTREES}/stray2` })).toBeVisible();
    expect(cardsPathExists(`${CARDS_WORKTREES}/stray2`)).toBe(false);
  });

  test('Switch back to main is greyed while a session uses the workspace, then works', async ({ page }) => {
    inCards('git checkout -q -b side');
    try {
      const uuid = await openSessionViaPost(page, { pwd: CARDS_REPO });
      testSessions.push(uuid);
      await waitUntilInUse(page, 'workspace');

      await openCardsRepo(page);
      const ws = page.locator('#branch-card-workspace-slot');
      await expect(ws).toContainText('Workspace (side branch)');
      await expect(ws).toContainText('not main');
      await expect(ws.getByRole('button', { name: 'Switch back to main' })).toBeDisabled();

      await endSessions(page, testSessions.splice(0));
      await expect.poll(async () => {
        const r = await page.request.get('/api/repo/branches?path=' + encodeURIComponent(CARDS_REPO));
        return ((await r.json()).cards || [])[0].inUse || false;
      }, { timeout: 30_000 }).toBe(false);
      await openCardsRepo(page);
      await page.locator('#branch-card-workspace-slot').getByRole('button', { name: 'Switch back to main' }).click();
      await expect(page.locator('#branch-card-workspace-slot')).toContainText('Workspace (main branch)');
      await expect(branchCard(page, 'side')).toBeVisible();
      expect(inCards('git symbolic-ref --short HEAD')).toBe('main');
    } finally {
      await endSessions(page, testSessions.splice(0));
      inCards('git checkout -q main 2>/dev/null; git branch -D side 2>/dev/null; true');
    }
  });

  test('a slow delete keeps the card open with a greyed Deleting... button, and the dialog stays usable', async ({ page }) => {
    inCards('git branch del-slow');
    let release;
    const held = new Promise((resolve) => { release = resolve; });
    await page.route('**/api/repo/branch-delete', async (route) => {
      await held;
      await route.continue();
    });
    try {
      await openCardsRepo(page);
      const row = cardRow(page, 'del-slow');
      await branchCard(page, 'del-slow').click();
      const delBtn = row.getByRole('button', { name: 'Delete del-slow', exact: true });
      await expect(delBtn).toBeVisible();
      const openHeight = (await row.boundingBox()).height;
      await delBtn.click();
      await expect(row).toContainText('Deleting folder...');
      // Double-tap guard: the button is greyed, not gone.
      await expect(row.getByRole('button', { name: 'Deleting del-slow' })).toBeDisabled();
      expect(Math.abs((await row.boundingBox()).height - openHeight)).toBeLessThanOrEqual(2);
      // The rest of the dialog still works, and the deleting card stays open.
      await branchCard(page, 'feat-a').click();
      await expect(branchCard(page, 'feat-a')).toHaveAttribute('aria-pressed', 'true');
      await expect(row).toContainText('Deleting folder...');

      release();
      await expect(page.locator('.branch-card-row--deleted', { hasText: 'Deleted del-slow' })).toBeVisible();
    } finally {
      release();
      inCards('git branch -D del-slow 2>/dev/null; true');
    }
  });

  test('deleting the picked card puts the pick back on the workspace', async ({ page }) => {
    inCards('git branch del-picked');
    await openCardsRepo(page);
    await pickAndDelete(page, 'del-picked');
    await expect(page.locator('.branch-card-row--deleted', { hasText: 'Deleted del-picked' })).toBeVisible();
    await expect(page.locator('#branch-card-workspace-slot .branch-card')).toHaveAttribute('aria-pressed', 'true');
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

  // Last: these start sessions, whose lingering processes can leave
  // branches or folders in the cards repo for a moment after they end.
  test('a new name not online starts right away after the check', async ({ page }) => {
    const checks = [];
    page.on('request', (req) => { if (req.url().includes('/api/repo/remote-branch')) checks.push(JSON.parse(req.postData() || '{}')); });
    try {
      await openCardsRepo(page);
      await page.fill('#new-session-branch', 'brand-new-idea');
      await page.locator('.dialog__agent').first().click();
      await page.click('#new-session-start-chat');
      await page.waitForURL(/\/session\/[a-f0-9-]{36}\?/, { timeout: 30_000 });
      const url = new URL(page.url());
      testSessions.push(url.pathname.split('/')[2]);
      expect(url.searchParams.get('branch')).toBe('brand-new-idea');
      expect(checks.map((c) => [c.remote, c.name])).toEqual([['origin', 'brand-new-idea']]);
    } finally {
      await endSessions(page, testSessions.splice(0));
      await dropSessionBranch('brand-new-idea');
    }
  });

  test('when the name check cannot reach the remote: Cancel, Back, Start anyway', async ({ page }) => {
    let release;
    const held = new Promise((resolve) => { release = resolve; });
    let calls = 0;
    await page.route('**/api/repo/remote-branch', async (route) => {
      calls++;
      if (calls === 1) { await held; return route.abort().catch(() => {}); }
      return route.fulfill({ status: 502, contentType: 'application/json',
        body: JSON.stringify({ error: "Couldn't reach origin to check if this name is taken." }) });
    });
    try {
      await openCardsRepo(page);
      await page.fill('#new-session-branch', 'offline-idea');
      await page.locator('.dialog__agent').first().click();
      await page.click('#new-session-start-chat');
      const msg = page.locator('#branch-cards-msg');
      await expect(msg).toContainText('Checking if "offline-idea" is already on origin...');
      await expect(page.locator('#new-session-start-chat')).toHaveText('Checking origin...');
      await expect(page.locator('#new-session-start-chat')).toBeDisabled();

      await msg.getByRole('button', { name: 'Cancel' }).click();
      await expect(msg).toContainText("Couldn't reach origin to check if this name is taken.");
      await expect(page.locator('#new-session-start-chat')).toHaveText('Start Agent Chat');
      await msg.getByRole('button', { name: 'Back' }).click();
      await expect(msg).toHaveText('');

      await page.click('#new-session-start-chat');
      await expect(msg).toContainText("Couldn't reach origin to check if this name is taken.");
      await msg.getByRole('button', { name: 'Start anyway' }).click();
      await page.waitForURL(/\/session\/[a-f0-9-]{36}\?/, { timeout: 30_000 });
      const url = new URL(page.url());
      testSessions.push(url.pathname.split('/')[2]);
      expect(url.searchParams.get('branch')).toBe('offline-idea');
    } finally {
      release();
      await endSessions(page, testSessions.splice(0));
      await dropSessionBranch('offline-idea');
    }
  });

});
