import { test, expect } from '@playwright/test';
import crypto from 'crypto';

const BASE_URL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;

// Auth cookie comes from the suite-wide storageState (see playwright.config.js
// + global-setup.js); no per-test login is needed.

// Helper: create a chat session and wait for all port info via WebSocket
async function createChatSessionAndGetPorts(page) {
  const uuid = crypto.randomUUID();
  await page.goto(`/session/${uuid}?assistant=opencode&session=chat`);

  // Wait for the terminal UI to receive port info via WebSocket
  // terminal-ui.js stores the instance at window.terminalUI (line 190)
  // Use session=chat so agentChatProxyPort is included
  const ports = await page.waitForFunction(() => {
    const ui = window.terminalUI;
    if (!ui || !ui.previewProxyPort) return null;
    if (!ui.agentChatProxyPort) return null;
    if (!ui.filesProxyPort) return null;
    return {
      previewProxyPort: ui.previewProxyPort,
      vncProxyPort: ui.vncProxyPort,
      agentChatProxyPort: ui.agentChatProxyPort,
      filesProxyPort: ui.filesProxyPort,
      sessionUUID: ui.sessionUUID,
    };
  }, { timeout: 60_000 });

  return ports.jsonValue();
}

// Helper: fetch a port with retries (proxy servers may take a moment to start)
async function fetchPortWithRetry(page, port, path, maxRetries) {
  const url = new URL(BASE_URL);
  const targetUrl = `${url.protocol}//${url.hostname}:${port}${path}`;

  for (let i = 0; i < maxRetries; i++) {
    const resp = await page.evaluate(async (fetchUrl) => {
      try {
        const r = await fetch(fetchUrl, {
          signal: AbortSignal.timeout(5000),
          mode: 'no-cors',
        });
        return { status: r.status, ok: true, type: r.type };
      } catch (e) {
        return { status: 0, ok: false, error: e.message };
      }
    }, targetUrl);

    if (resp.ok) {
      return { ...resp, port, targetUrl };
    }

    console.log(`  Retry ${i + 1}/${maxRetries} for port ${port}: ${resp.error}`);
    await page.waitForTimeout(2000);
  }

  return { status: 0, ok: false, error: 'all retries exhausted', port, targetUrl };
}

// Single test that verifies all proxy ports from one session.
// Uses one login + one session to avoid Traefik rate limiting in compose mode.
test.describe('Port Connectivity', () => {
  test('preview, VNC, agent chat, and files proxy ports respond', async ({ page }) => {
    const ports = await createChatSessionAndGetPorts(page);

    // --- Preview proxy ---
    expect(ports.previewProxyPort).toBeTruthy();
    console.log(`Testing preview proxy port: ${ports.previewProxyPort}`);
    const previewResp = await fetchPortWithRetry(page, ports.previewProxyPort, '/', 5);
    console.log(`Preview proxy port ${previewResp.port}: ok=${previewResp.ok}, status=${previewResp.status}, type=${previewResp.type}`);
    expect(previewResp.ok).toBe(true);

    // --- VNC proxy port allocation ---
    // We only assert the port number is allocated; we cannot drive a TCP
    // connection without first starting the browser, and POST
    // /api/session/{uuid}/browser/start is gated by MCP_AUTH_KEY (a
    // server-internal secret unreachable from the test runner). In
    // dockerfile-only mode the published port additionally maps host:PP
    // to container:vncPort (websockify) rather than container:PP, so
    // even a no-op fetch on PP would be testing websockify readiness,
    // not the swe-swe-server VNC proxy. The agent-browser spec exercises
    // the real end-to-end path (agent invokes playwright MCP, which
    // triggers browser/start via mcp-lazy-init's internal auth key).
    expect(ports.vncProxyPort).toBeTruthy();
    console.log(`VNC proxy port allocated: ${ports.vncProxyPort}`);

    // --- Agent chat proxy ---
    expect(ports.agentChatProxyPort).toBeTruthy();
    console.log(`Testing agent chat proxy port: ${ports.agentChatProxyPort}`);
    const chatResp = await fetchPortWithRetry(page, ports.agentChatProxyPort, '/', 5);
    console.log(`Agent chat proxy port ${chatResp.port}: ok=${chatResp.ok}, status=${chatResp.status}, type=${chatResp.type}`);
    expect(chatResp.ok).toBe(true);

    // --- Files proxy (per-session md-serve) ---
    // The files port is derived as previewPort+6000, then wrapped by the same
    // proxyPortOffset as every other proxy band. So independent of the
    // configured offset, filesProxyPort - previewProxyPort === 6000. With the
    // e2e simple stack's preview band (3200-3229) and the default offset
    // (20000), filesProxyPort lands in 29200-29229. Unlike VNC, md-serve
    // answers under the auth cookie without any MCP_AUTH_KEY gate, so we can
    // drive the proxy end-to-end here.
    expect(ports.filesProxyPort).toBeTruthy();
    expect(ports.filesProxyPort - ports.previewProxyPort).toBe(6000);
    expect(ports.filesProxyPort).toBeGreaterThanOrEqual(29200);
    expect(ports.filesProxyPort).toBeLessThanOrEqual(29229);
    console.log(`Testing files proxy port: ${ports.filesProxyPort}`);
    const filesResp = await fetchPortWithRetry(page, ports.filesProxyPort, '/', 5);
    console.log(`Files proxy port ${filesResp.port}: ok=${filesResp.ok}, status=${filesResp.status}, type=${filesResp.type}`);
    expect(filesResp.ok).toBe(true);
  });
});
