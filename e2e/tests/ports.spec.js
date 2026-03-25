import { test, expect } from '@playwright/test';
import crypto from 'crypto';

const PASSWORD = process.env.SWE_SWE_PASSWORD || 'changeme';
const BASE_URL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;

// Helper: login
async function login(page) {
  await page.goto('/swe-swe-auth/login');
  await page.fill('input[type="password"]', PASSWORD);
  await Promise.all([
    page.waitForNavigation(),
    page.click('button[type="submit"]'),
  ]);
}

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
    return {
      previewProxyPort: ui.previewProxyPort,
      vncProxyPort: ui.vncProxyPort,
      agentChatProxyPort: ui.agentChatProxyPort,
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
  test('preview, VNC, and agent chat proxy ports respond', async ({ page }) => {
    await login(page);
    const ports = await createChatSessionAndGetPorts(page);

    // --- Preview proxy ---
    expect(ports.previewProxyPort).toBeTruthy();
    console.log(`Testing preview proxy port: ${ports.previewProxyPort}`);
    const previewResp = await fetchPortWithRetry(page, ports.previewProxyPort, '/', 5);
    console.log(`Preview proxy port ${previewResp.port}: ok=${previewResp.ok}, status=${previewResp.status}, type=${previewResp.type}`);
    expect(previewResp.ok).toBe(true);

    // --- VNC proxy (needs browser started first) ---
    expect(ports.vncProxyPort).toBeTruthy();
    console.log(`Testing VNC proxy port: ${ports.vncProxyPort}`);
    const url = new URL(BASE_URL);
    await page.evaluate(async (startUrl) => {
      await fetch(startUrl, { method: 'POST', credentials: 'include' });
    }, `${url.origin}/start-browser/${ports.sessionUUID}`);
    await page.waitForTimeout(3000);

    const vncResp = await fetchPortWithRetry(page, ports.vncProxyPort, '/vnc_lite.html', 5);
    console.log(`VNC proxy port ${vncResp.port}: ok=${vncResp.ok}, status=${vncResp.status}, type=${vncResp.type}`);
    expect(vncResp.ok).toBe(true);

    // --- Agent chat proxy ---
    expect(ports.agentChatProxyPort).toBeTruthy();
    console.log(`Testing agent chat proxy port: ${ports.agentChatProxyPort}`);
    const chatResp = await fetchPortWithRetry(page, ports.agentChatProxyPort, '/', 5);
    console.log(`Agent chat proxy port ${chatResp.port}: ok=${chatResp.ok}, status=${chatResp.status}, type=${chatResp.type}`);
    expect(chatResp.ok).toBe(true);
  });
});
