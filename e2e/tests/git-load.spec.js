import { test, expect } from './_helpers/reaper.js';
import { execSync } from 'child_process';

// Live load check for tasks/2026-09-25-git-one-at-a-time.md, phase E.
// Opt-in (slow, disk-heavy): E2E_GIT_LOAD=1 ./scripts/e2e-test.sh simple tests/git-load.spec.js
//
// A repo with 90 branches and 40 branch folders, 5 of them holding 50k
// ignored files. A wrapper around the container's git logs every call with
// start/end times, so the checks can count git commands and see whether two
// ever ran at once on the same project. Measurements go to the test output.

const REPO = '/repos/e2e-load-repo/workspace';
const WT = '/repos/e2e-load-repo/worktrees';
const OTHER = '/repos/e2e-load-other/workspace';
const GITLOG = '/tmp/git-calls.log';
const BIG = ['big-1', 'big-2', 'big-3', 'big-4', 'big-5'];

test.skip(!process.env.E2E_GIT_LOAD, 'opt-in: set E2E_GIT_LOAD=1');
test.describe.configure({ mode: 'serial', retries: 0, timeout: 900_000 });

function container() {
  return execSync('docker ps --format "{{.Names}}" | grep e2e | grep swe-swe | grep -v traefik | head -1').toString().trim();
}
// Runs a shell script in the container (fed on stdin, so it can span lines).
function sh(script, user) {
  const u = user ? `-u ${user} ` : '';
  return execSync(`docker exec -i ${u}${container()} sh`, { input: script, maxBuffer: 64 << 20 }).toString().trim();
}

// Every git call as {start, end, dir, args}; dir is the -C argument.
function gitCalls() {
  const lines = sh(`cat ${GITLOG} 2>/dev/null || true`).split('\n').filter(Boolean);
  const byPid = new Map();
  for (const l of lines) {
    const [kind, t, pid, ...rest] = l.split(' ');
    if (kind === 'S') byPid.set(pid, { start: +t, end: Infinity, args: rest.join(' ') });
    else if (kind === 'E' && byPid.has(pid)) byPid.get(pid).end = +t;
  }
  return [...byPid.values()].map((c) => {
    const m = c.args.match(/^-C (\S+)/);
    return Object.assign(c, { dir: m ? m[1] : '' });
  });
}
function resetGitLog() { sh(`: > ${GITLOG}`); }
// Most git commands running at the same moment among `calls`.
function maxOverlap(calls) {
  const ev = [];
  for (const c of calls) { ev.push([c.start, 1]); ev.push([c.end, -1]); }
  ev.sort((a, b) => a[0] - b[0] || a[1] - b[1]);
  let cur = 0, max = 0;
  for (const [, d] of ev) { cur += d; max = Math.max(max, cur); }
  return max;
}
const inLoadRepo = (c) => c.dir.startsWith('/repos/e2e-load-repo/') && !/\b(fetch|ls-remote)\b/.test(c.args);

async function openRepo(page, repo) {
  await page.goto('/');
  await page.click('#btn-new-session');
  await page.waitForFunction((v) => Array.from(document.getElementById('new-session-mode').options).some((o) => o.value === v), repo, { timeout: 15_000 });
  const t0 = Date.now();
  await page.evaluate((v) => {
    const sel = document.getElementById('new-session-mode');
    sel.value = v;
    sel.dispatchEvent(new Event('change'));
  }, repo);
  await expect(page.locator('#branch-card-workspace-slot .branch-card')).toBeVisible({ timeout: 60_000 });
  return Date.now() - t0;
}
function card(page, name) {
  return page.locator('#branch-cards-sections .branch-card-row', {
    has: page.locator('button.branch-card .branch-card__name', { hasText: new RegExp('^' + name + '$') }),
  });
}
function report(name, value) {
  test.info().annotations.push({ type: name, description: String(value) });
  console.log(`[git-load] ${name}: ${typeof value === 'string' ? value : JSON.stringify(value)}`);
}

test.describe('git load (phase E)', () => {
  test.beforeAll(() => {
    // Log every git call, start and end. The wrapper sits in /usr/local/bin,
    // ahead of /usr/bin on the server's PATH; the real git is left untouched.
    sh(`cat > /usr/local/bin/git <<'EOF'
#!/bin/sh
echo "S $(date +%s.%N) $$ $*" >> ${GITLOG}
/usr/bin/git "$@"
rc=$?
echo "E $(date +%s.%N) $$" >> ${GITLOG}
exit $rc
EOF
chmod 755 /usr/local/bin/git; touch ${GITLOG}; chmod 666 ${GITLOG}`, 'root');

    sh(`set -e
rm -rf /repos/e2e-load-repo /repos/e2e-load-other /tmp/e2e-load-origin.git
mkdir -p ${REPO} && cd ${REPO}
git init -q -b main && git config user.email e2e@test.invalid && git config user.name e2e
echo node_modules/ > .gitignore && git add . && git commit -q -m init
git init -q --bare -b main /tmp/e2e-load-origin.git
git remote add origin /tmp/e2e-load-origin.git && git push -q origin main && git fetch -q origin
git remote add slow https://10.255.255.1/acme/x.git
for i in $(seq 1 50); do git branch b-$i; done
for i in $(seq 1 35); do git worktree add -q -b w-$i ${WT}/w-$i; mkdir ${WT}/w-$i/node_modules; (cd ${WT}/w-$i/node_modules && seq 1 2000 | xargs touch); done
for b in ${BIG.join(' ')}; do git worktree add -q -b $b ${WT}/$b; mkdir ${WT}/$b/node_modules; (cd ${WT}/$b/node_modules && seq 1 50000 | xargs touch); done
mkdir -p ${OTHER} && cd ${OTHER} && git init -q -b main && git config user.email e2e@test.invalid && git config user.name e2e && git commit -q --allow-empty -m init && git branch x
`);
  });

  test.afterAll(() => {
    sh('rm -rf /repos/e2e-load-repo /repos/e2e-load-other /tmp/e2e-load-origin.git');
    sh('rm -f /usr/local/bin/git', 'root');
  });

  test('1. opening New Session runs a handful of git commands, one at a time', async ({ page }) => {
    const n = sh(`cd ${REPO} && git branch | wc -l`);
    report('local branches', n);
    resetGitLog();
    const ms = await openRepo(page, REPO);
    await page.waitForTimeout(2000);
    const calls = gitCalls().filter((c) => c.dir.startsWith('/repos/e2e-load-repo/'));
    report('open: time to list (ms)', ms);
    report('open: git commands', calls.length);
    report('open: git subcommands', calls.map((c) => c.args.split(' ')[2]).join(','));
    report('open: most at once', maxOverlap(calls));
    expect(calls.length).toBeLessThanOrEqual(12);
    expect(maxOverlap(calls)).toBe(1);
  });

  test('2. picking through 10 cards quickly never runs two git commands at once', async ({ page }) => {
    await openRepo(page, REPO);
    const responses = [];
    page.on('requestfinished', (r) => { if (r.url().includes('/api/repo/branch-check')) responses.push(r); });
    let failed = 0;
    page.on('requestfailed', (r) => { if (r.url().includes('/api/repo/branch-check')) failed++; });
    resetGitLog();
    const names = Array.from({ length: 10 }, (_, i) => 'w-' + (i + 1));
    for (const name of names) {
      await card(page, name).locator('button.branch-card').click();
      await page.waitForTimeout(40);
    }
    const last = card(page, 'w-10');
    await expect(last.getByRole('button', { name: 'Delete w-10', exact: true })).toBeVisible();
    await expect(last).not.toContainText('Checking');
    await page.waitForTimeout(1000);
    const calls = gitCalls().filter(inLoadRepo);
    report('picks: requests finished / dropped', `${responses.length} / ${failed}`);
    report('picks: git commands', calls.length);
    report('picks: most at once in this project', maxOverlap(calls));
    report('picks: last card says', (await last.locator('.branch-card__line').innerText()).replace(/\s+/g, ' '));
    expect(maxOverlap(calls)).toBe(1);
  });

  test('3. deleting 5 large branch folders back to back, while other projects stay fast', async ({ page, browser }) => {
    await openRepo(page, REPO);
    resetGitLog();
    const fileNr = [];
    const sampler = setInterval(() => {
      try { fileNr.push(+sh('cut -f1 /proc/sys/fs/file-nr')); } catch { /* sample missed */ }
    }, 250);

    const t0 = Date.now();
    for (const b of BIG) {
      await card(page, b).locator('button.branch-card').click();
      await card(page, b).getByRole('button', { name: 'Delete ' + b, exact: true }).click({ timeout: 60_000 });
    }

    // Meanwhile: another project, and the same project, in a second tab.
    const other = await browser.newPage();
    const otherMs = await openRepo(other, OTHER);
    const sameT0 = Date.now();
    const same = await other.request.get('/api/repo/branches?path=' + encodeURIComponent(REPO));
    const sameMs = Date.now() - sameT0;
    await other.close();

    for (const b of BIG) {
      await expect(page.locator('.branch-card-row--deleted', { hasText: 'Deleted ' + b })).toBeVisible({ timeout: 300_000 });
    }
    const totalMs = Date.now() - t0;
    clearInterval(sampler);

    const calls = gitCalls().filter(inLoadRepo);
    const removes = calls.filter((c) => /worktree remove/.test(c.args));
    report('delete: total time for 5 (ms)', totalMs);
    report('delete: each folder removal (ms)', removes.map((c) => Math.round((c.end - c.start) * 1000)));
    report('delete: most git at once in this project', maxOverlap(calls));
    report('delete: open files on the box, min/max', `${Math.min(...fileNr)} / ${Math.max(...fileNr)}`);
    report('during delete: other project opened in (ms)', otherMs);
    report('during delete: same project branch list', `${same.status()} in ${sameMs} ms`);
    expect(maxOverlap(calls)).toBe(1);
    expect(removes.length).toBe(5);
    expect(otherMs).toBeLessThan(5000);
  });

  test('4. an unreachable remote gives up within about 10 seconds', async ({ page }) => {
    await openRepo(page, REPO);
    const section = page.locator('details.branch-cards__online', { hasText: 'Online only: slow' });
    const t0 = Date.now();
    await section.locator('summary').click();
    await expect(section.getByRole('button', { name: 'Retry' })).toBeVisible({ timeout: 30_000 });
    const sectionMs = Date.now() - t0;
    report('unreachable: section gave up after (ms)', sectionMs);
    expect(sectionMs).toBeLessThan(13_000);

    sh(`cd ${REPO} && git remote set-url origin https://10.255.255.1/acme/y.git`);
    try {
      await page.fill('#new-session-branch', 'load-new-idea');
      await page.locator('.dialog__agent').first().click();
      const t1 = Date.now();
      await page.click('#new-session-start-chat');
      await expect(page.locator('#branch-cards-msg').getByRole('button', { name: 'Start anyway' })).toBeVisible({ timeout: 30_000 });
      const nameMs = Date.now() - t1;
      report('unreachable: name check gave up after (ms)', nameMs);
      expect(nameMs).toBeLessThan(13_000);
    } finally {
      sh(`cd ${REPO} && git remote set-url origin /tmp/e2e-load-origin.git`);
    }
  });
});
