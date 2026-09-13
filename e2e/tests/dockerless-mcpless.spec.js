import { test, expect } from './_helpers/reaper.js';
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';

// MCP-less mode, live: `swe-swe init --runtime=host --without-mcp`.
//
// The mode exists for a host whose agent ignores (or refuses) MCP config. So
// the only claim worth testing is the one the mode makes: with no .mcp.json
// anywhere, a session's tools are still reachable -- through the `mcp` CLI,
// over unix sockets, served by a proxy fleet swe-swe-server launched itself.
// Everything up to that point was unit-tested and none of it was ever run.
//
// The assertions live in a spec rather than in the shell harness because they
// have to happen while a session is ALIVE: the fleet starts when the session
// websocket connects and its sockets are removed on teardown, so a check after
// the browser phase would find an empty directory and pass for the wrong
// reason.
//
// Driven by scripts/e2e-dockerless.sh with E2E_WITHOUT_MCP=1, which passes
// E2E_SWE_BIN_DIR (the dumped payload's bin dir) and E2E_PROJECT_DIR.

const BIN_DIR = process.env.E2E_SWE_BIN_DIR || '';
const PROJECT_DIR = process.env.E2E_PROJECT_DIR || '';

// Mirrors mcpLessSocketRoot() in swe-swe-server: deliberately short and under
// the temp dir, because unix socket paths cap at 108 bytes and the metadata
// dir this once lived under blew past it -- every proxy then died on bind.
const SOCKET_ROOT = path.join(os.tmpdir(), `swe-swe-${process.getuid()}`, 'mcp');

// sun_path is 108 on Linux, 104 on macOS; the server asserts against the
// smaller number, so match it.
const UNIX_SOCKET_PATH_MAX = 104;

test.use({ storageState: { cookies: [], origins: [] } });

function runMcp(args, sockDir, timeout = 60_000) {
  return execFileSync(path.join(BIN_DIR, 'mcp'), args, {
    env: { ...process.env, SWE_MCP_DIR: sockDir },
    encoding: 'utf8',
    timeout,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
}

test.describe('dockerless MCP-less mode', () => {
  test.skip(!process.env.E2E_WITHOUT_MCP, 'MCP-less only: set E2E_WITHOUT_MCP=1');

  test('a live session reaches its tools through the mcp CLI, with no .mcp.json', async ({ page }) => {
    // The proxy children are npx packages; a cold cache fetches them.
    test.setTimeout(300_000);

    expect(BIN_DIR, 'harness must pass E2E_SWE_BIN_DIR').toBeTruthy();

    // A chat session, so the agent-chat proxy is in the fleet too (it is the
    // only member gated on session mode).
    const resp = await page.request.post('/api/session/new', {
      form: { assistant: 'opencode', session: 'chat' },
      maxRedirects: 0,
    });
    expect(resp.status()).toBe(302);
    const uuid = resp.headers()['location'].match(/\/session\/([0-9a-f-]+)/)[1];
    await page.goto(resp.headers()['location']);

    // Wait for the session websocket to be live -- that is what launches the
    // fleet. Port delivery is the same signal the other dockerless specs use.
    await page.waitForFunction(() => window.terminalUI?.previewProxyPort, { timeout: 60_000 });

    // --- 1. The fleet put its sockets where the agent will look ---
    const sockDir = path.join(SOCKET_ROOT, uuid);
    const want = ['swe-swe-agent-chat', 'swe-swe-playwright', 'swe-swe-preview', 'swe-swe'];
    for (let i = 0; i < 60; i++) {
      if (fs.existsSync(sockDir) && want.every(n => fs.existsSync(path.join(sockDir, `${n}.sock`)))) break;
      await page.waitForTimeout(1000);
    }
    const present = fs.existsSync(sockDir) ? fs.readdirSync(sockDir) : [];
    for (const name of want) {
      expect(present, `${name}.sock should be in ${sockDir}`).toContain(`${name}.sock`);
    }

    // --- 2. Every socket path fits in sun_path ---
    // Not a detail: over the cap, bind() fails with "invalid argument" and the
    // whole fleet dies silently. This is why the root is under the temp dir
    // and not in the project's metadata dir.
    for (const name of present) {
      const full = path.join(sockDir, name);
      expect(Buffer.byteLength(full), `${full} must fit in sun_path`).toBeLessThan(UNIX_SOCKET_PATH_MAX);
    }

    // --- 3. The CLI the agent is told to use actually lists the fleet ---
    // `mcp -h` dumps every reachable server. A proxy whose child is still
    // cold-fetching from npm is not listed yet, so poll.
    let help = '';
    for (let i = 0; i < 60; i++) {
      try {
        help = runMcp(['-h'], sockDir);
        if (want.every(n => help.includes(n))) break;
      } catch (e) {
        help = String(e.stdout || '') + String(e.stderr || '');
      }
      await page.waitForTimeout(2000);
    }
    for (const name of want) {
      expect(help, `mcp -h should list ${name}`).toContain(name);
    }

    // --- 4. A real round-trip, not just a listing ---
    // `mcp swe-swe` dumps that server's tools, which means the CLI opened the
    // socket, the proxy relayed to its child, and the child answered. A named
    // tool is asserted so a reachable-but-empty server cannot pass.
    //
    // A proxy whose child is still coming up answers "mcp server restarting"
    // (-32000) rather than blocking, so poll rather than take the first word.
    let dump = '';
    for (let i = 0; i < 45; i++) {
      try {
        dump = runMcp(['swe-swe'], sockDir, 120_000);
        if (dump.includes('list_sessions')) break;
      } catch (e) {
        dump = String(e.stdout || '') + String(e.stderr || '');
      }
      await page.waitForTimeout(2000);
    }
    expect(dump, 'the swe-swe server should list its tools').toContain('list_sessions');

    // And there is genuinely no MCP config it could have read instead.
    expect(fs.existsSync(path.join(PROJECT_DIR, '.mcp.json'))).toBe(false);
  });

  // The steering block is claude-only (mcpLessSteeringFile returns a path for
  // no other assistant), so it needs its own claude session. With no .mcp.json
  // to read, this block is the ONLY thing that tells claude the `mcp` CLI
  // exists -- without it the mode ships an agent that cannot find its tools.
  test('a claude session gets the mcp-less steering in CLAUDE.local.md', async ({ page }) => {
    test.setTimeout(120_000);
    expect(PROJECT_DIR, 'harness must pass E2E_PROJECT_DIR').toBeTruthy();

    const resp = await page.request.post('/api/session/new', {
      form: { assistant: 'claude', session: 'chat' },
      maxRedirects: 0,
    });
    expect(resp.status()).toBe(302);
    await page.goto(resp.headers()['location']);
    await page.waitForFunction(() => window.terminalUI?.previewProxyPort, { timeout: 60_000 });

    const steering = path.join(PROJECT_DIR, 'CLAUDE.local.md');
    for (let i = 0; i < 30; i++) {
      if (fs.existsSync(steering)) break;
      await page.waitForTimeout(1000);
    }
    expect(fs.existsSync(steering), `${steering} should exist`).toBe(true);
    const body = fs.readFileSync(steering, 'utf8');
    expect(body).toContain('<!-- swe-swe:mcp-less begin -->');
    expect(body).toContain('<!-- swe-swe:mcp-less end -->');
    expect(body, 'the steering must name the CLI the agent has to use').toContain('mcp');
  });
});
