// Shared session-management helpers used by specs and globalSetup.

const PASSWORD = process.env.SWE_SWE_PASSWORD || 'changeme';
const BASE_URL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;

// Log in via the password form. Mirrors the per-spec inline `login(page)`
// helpers; consolidated here so globalSetup can reuse the same flow.
export async function login(page) {
  await page.goto('/swe-swe-auth/login');
  await page.fill('input[type="password"]', PASSWORD);
  await Promise.all([
    page.waitForNavigation(),
    page.click('button[type="submit"]'),
  ]);
}

// Open a new session through the real creation flow: POST /api/session/new
// (which stages a creation intent and 302s to /session/{minted-uuid}), then
// navigate to that redirect target. Returns the server-minted UUID.
//
// This mirrors what the New-session dialog / recording "+ New" form do in the
// product. Bare `page.goto('/session/{client-uuid}?...')` no longer creates a
// session by design (no-ghost-session invariant: the WS refuses to materialize
// a UUID with no staged intent), so specs must go through this helper.
export async function openSessionViaPost(page, opts = {}) {
  const { assistant = 'opencode', session, name, branch, pwd, extra_args } = opts;
  const form = { assistant };
  if (session) form.session = session;
  if (name) form.name = name;
  if (branch) form.branch = branch;
  if (pwd) form.pwd = pwd;
  if (extra_args) form.extra_args = extra_args;

  // Don't follow the redirect -- read the minted UUID out of the Location.
  const resp = await page.request.post('/api/session/new', { form, maxRedirects: 0 });
  const loc = resp.headers()['location'];
  if (!loc) {
    throw new Error(`/api/session/new did not redirect (status ${resp.status()})`);
  }
  const m = loc.match(/\/session\/([a-f0-9-]{36})/);
  if (!m) {
    throw new Error(`unexpected /api/session/new redirect location: ${loc}`);
  }
  await page.goto(loc);
  return m[1];
}

// End every session currently visible on the home page. Uses the page's
// auth cookie via page.evaluate so we don't need a separate cookie jar.
//
// Used in two places:
//   - playwright globalSetup, to give the suite a clean baseline
//   - per-test afterEach (on pass), to prevent within-suite accumulation
//
// On failed tests we deliberately skip the afterEach call so the failed
// session sticks around for inspection.
export async function endAllSessions(page) {
  const uuids = await page.evaluate(async () => {
    const r = await fetch('/', { credentials: 'include' });
    const html = await r.text();
    const set = new Set();
    const re = /\/session\/([a-f0-9-]{36})/g;
    let m;
    while ((m = re.exec(html)) !== null) set.add(m[1]);
    return [...set];
  });

  for (const uuid of uuids) {
    await page.evaluate(async (u) => {
      try {
        await fetch(`/api/session/${u}/end`, { method: 'POST', credentials: 'include' });
      } catch (e) {
        // Session may have already ended (recordings list also contains
        // historical UUIDs that return 404). Ignore -- we only care that
        // the live ones are gone.
      }
    }, uuid);
  }
  return uuids.length;
}

// End just the listed UUIDs. Used in afterEach when we know exactly which
// sessions a test created (preserves any sessions another test left).
export async function endSessions(page, uuids) {
  for (const uuid of uuids) {
    await page.evaluate(async (u) => {
      try {
        await fetch(`/api/session/${u}/end`, { method: 'POST', credentials: 'include' });
      } catch (e) { /* ignore */ }
    }, uuid);
  }
}

export { BASE_URL };
