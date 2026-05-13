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
