import dns from 'node:dns/promises';

// Chromium launch arguments that pin the wildcard apex, shared by
// playwright.config.js and global-setup.js.
//
// Wildcard mode (`make test-e2e-wildcard`) reaches the app at {apex} and its
// panes at {port}.{apex}, but nothing in DNS points those names at the machine
// running the stack -- lvh.me's real records point at 127.0.0.1, which is this
// runner's own loopback, not the container host. Pinning both shapes inside
// the browser keeps the addresses reading exactly as they would in production
// while the suite depends on no DNS at all, and so still works offline.
//
// MAP's target is not itself put through these rules, so it must be a literal
// address. E2E_RESOLVE_IP carries the host e2e-test.sh used; a name there (or
// a hand-run with neither variable) is resolved once here.
//
// Both launch paths need this. global-setup.js creates its own browser to
// capture the login cookie, and a rule applied only in the config would leave
// that first navigation pointed at nothing.
export async function wildcardResolverArgs(baseURL) {
  const apex = process.env.SWE_PUBLIC_HOSTNAME || '';
  if (!apex) return [];
  const want = process.env.E2E_RESOLVE_IP || new URL(baseURL).hostname;
  const ip = /^\d+\.\d+\.\d+\.\d+$/.test(want) ? want : (await dns.lookup(want)).address;
  return [`--host-resolver-rules=MAP ${apex} ${ip},MAP *.${apex} ${ip}`];
}
