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

// Helper: create session and wait for port info via WebSocket status message
// Use session=chat to get agentChatProxyPort (server only sends it for chat sessions)
async function createSessionAndGetPorts(page, { chatMode = false } = {}) {
  const uuid = crypto.randomUUID();
  const qs = chatMode ? '?assistant=opencode&session=chat' : '?assistant=opencode';
  await page.goto(`/session/${uuid}${qs}`);

  // Wait for the terminal UI to receive port info via WebSocket
  // terminal-ui.js stores the instance at window.terminalUI (line 190)
  const ports = await page.waitForFunction((wantChat) => {
    const ui = window.terminalUI;
    if (!ui || !ui.previewProxyPort) return null;
    // For chat mode, wait until agentChatProxyPort is set too
    if (wantChat && !ui.agentChatProxyPort) return null;
    return {
      previewProxyPort: ui.previewProxyPort,
      vncProxyPort: ui.vncProxyPort,
      agentChatProxyPort: ui.agentChatProxyPort || null,
    };
  }, chatMode, { timeout: 60_000 });

  return ports.jsonValue();
}

// Helper: fetch a port with retries (proxy servers may take a moment to start)
// Uses server-side fetch via the base URL to avoid browser CORS/connectivity issues
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

test.describe('Port Connectivity', () => {
  test.beforeEach(async ({ page }) => {
    await login(page);
  });

  test('preview proxy port responds', async ({ page }) => {
    const ports = await createSessionAndGetPorts(page);
    expect(ports.previewProxyPort).toBeTruthy();
    console.log(`Testing preview proxy port: ${ports.previewProxyPort}`);

    // Fetch the preview proxy port -- 502 Bad Gateway is OK (means proxy is
    // running but no backend yet). Any HTTP response means the port is alive.
    // With no-cors mode, an opaque response (type=opaque, status=0) also means success.
    const resp = await fetchPortWithRetry(page, ports.previewProxyPort, '/', 5);
    console.log(`Preview proxy port ${resp.port}: ok=${resp.ok}, status=${resp.status}, type=${resp.type}`);
    expect(resp.ok).toBe(true);
  });

  test('VNC proxy port responds after browser start', async ({ page }) => {
    const ports = await createSessionAndGetPorts(page);
    expect(ports.vncProxyPort).toBeTruthy();
    console.log(`Testing VNC proxy port: ${ports.vncProxyPort}`);

    // VNC proxy (websockify) only starts after the session browser is started.
    // Trigger browser start via the API endpoint.
    const uuid = await page.evaluate(() => window.terminalUI?.sessionUUID);
    const url = new URL(BASE_URL);
    await page.evaluate(async (startUrl) => {
      await fetch(startUrl, { method: 'POST', credentials: 'include' });
    }, `${url.origin}/start-browser/${uuid}`);

    // Wait a moment for Xvfb + x11vnc + websockify to start
    await page.waitForTimeout(3000);

    // VNC proxy should serve vnc_lite.html (or at least accept connections)
    const resp = await fetchPortWithRetry(page, ports.vncProxyPort, '/vnc_lite.html', 5);
    console.log(`VNC proxy port ${resp.port}: ok=${resp.ok}, status=${resp.status}, type=${resp.type}`);
    expect(resp.ok).toBe(true);
  });

  test('agent chat proxy port responds', async ({ page }) => {
    // Use chatMode to get agentChatProxyPort (server only sends it for session=chat)
    const ports = await createSessionAndGetPorts(page, { chatMode: true });
    expect(ports.agentChatProxyPort).toBeTruthy();

    console.log(`Testing agent chat proxy port: ${ports.agentChatProxyPort}`);
    const resp = await fetchPortWithRetry(page, ports.agentChatProxyPort, '/', 5);
    console.log(`Agent chat proxy port ${resp.port}: ok=${resp.ok}, status=${resp.status}, type=${resp.type}`);
    expect(resp.ok).toBe(true);
  });
});
