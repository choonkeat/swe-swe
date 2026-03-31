import { test, expect, request as apiRequest } from '@playwright/test';
import { execSync } from 'child_process';
import crypto from 'crypto';

const PASSWORD = process.env.SWE_SWE_PASSWORD || 'changeme';
const BASE_URL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;

// Helper: login and return cookie string for API requests
async function loginAndGetCookie(page) {
  await page.goto('/swe-swe-auth/login');
  await page.fill('input[type="password"]', PASSWORD);
  await Promise.all([
    page.waitForNavigation(),
    page.click('button[type="submit"]'),
  ]);
  const cookies = await page.context().cookies();
  const authCookie = cookies.find(c => c.name === 'swe_swe_session');
  return authCookie ? `swe_swe_session=${authCookie.value}` : '';
}

// Helper: get the e2e swe-swe container name (works for both simple and compose modes)
function getContainerName() {
  const name = execSync(
    `docker ps --format "{{.Names}}" | grep "e2e" | grep "swe-swe" | grep -v "traefik" | head -1`
  ).toString().trim();
  if (!name) throw new Error('No e2e swe-swe container found');
  return name;
}

// Helper: get the MCP auth key from a running session's process cmdline
function getMcpAuthKey(containerName) {
  const key = execSync(
    `docker exec ${containerName} sh -c 'cat /proc/[0-9]*/cmdline 2>/dev/null | tr "\\0" "\\n" | grep -oP "key=\\K[a-f0-9]{64}" | head -1'`
  ).toString().trim();
  if (!key) throw new Error('Could not extract MCP_AUTH_KEY from container (no session running?)');
  return key;
}

// Helper: parse SSE response to extract JSON-RPC result
function parseSseResponse(text) {
  const lines = text.split('\n');
  for (const line of lines) {
    if (line.startsWith('data: ')) {
      try {
        return JSON.parse(line.slice(6));
      } catch {
        // skip non-JSON data lines
      }
    }
  }
  throw new Error(`No JSON-RPC response found in SSE: ${text.substring(0, 200)}`);
}

// Helper: send a JSON-RPC request to the MCP endpoint
async function mcpRequest(ctx, mcpKey, method, params, id = 1) {
  const resp = await ctx.post(`${BASE_URL}/mcp?key=${mcpKey}`, {
    headers: {
      'Content-Type': 'application/json',
      'Accept': 'application/json, text/event-stream',
    },
    data: { jsonrpc: '2.0', id, method, params },
  });
  const text = await resp.text();
  let body;
  try {
    body = JSON.parse(text);
  } catch {
    body = parseSseResponse(text);
  }
  return { status: resp.status(), body };
}

// Helper: initialize MCP session, then call a tool
async function callMcpTool(ctx, mcpKey, toolName, args) {
  await mcpRequest(ctx, mcpKey, 'initialize', {
    protocolVersion: '2025-03-26',
    capabilities: {},
    clientInfo: { name: 'e2e-test', version: '1.0' },
  }, 0);
  return mcpRequest(ctx, mcpKey, 'tools/call', {
    name: toolName,
    arguments: args,
  });
}

test.describe('MCP create_session', () => {
  let mcpKey;
  let mcpCtx; // APIRequestContext with auth cookie baked in

  test.beforeAll(async ({ browser }) => {
    const containerName = getContainerName();

    // Create a browser session so that a child process with MCP_AUTH_KEY exists,
    // and extract the auth cookie for API requests (needed in compose mode where
    // Traefik's forwardauth middleware protects /mcp)
    const context = await browser.newContext();
    const page = await context.newPage();
    const cookie = await loginAndGetCookie(page);
    const uuid = crypto.randomUUID();
    await page.goto(`/session/${uuid}?assistant=opencode&session=terminal`);
    await page.locator('.terminal-ui__terminal').waitFor({ timeout: 30_000 });
    await page.waitForTimeout(2_000);
    await context.close();

    mcpKey = getMcpAuthKey(containerName);

    // Create an APIRequestContext with the auth cookie for all MCP requests
    mcpCtx = await apiRequest.newContext({
      extraHTTPHeaders: { 'Cookie': cookie },
    });
  });

  test.afterAll(async () => {
    if (mcpCtx) await mcpCtx.dispose();
  });

  test('rejects create_session without repo_path', async () => {
    const { status, body } = await callMcpTool(mcpCtx, mcpKey, 'create_session', {
      assistant: 'opencode',
    });

    const hasError =
      status >= 400 ||
      (body.error && body.error.message && body.error.message.includes('repo_path')) ||
      (body.result && body.result.isError);

    expect(hasError).toBe(true);
  });

  test('accepts create_session with repo_path', async () => {
    const { body } = await callMcpTool(mcpCtx, mcpKey, 'create_session', {
      assistant: 'opencode',
      repo_path: '/workspace',
    });

    expect(body.error).toBeUndefined();

    const resultText = body.result?.content?.[0]?.text;
    expect(resultText).toBeTruthy();
    const sessionInfo = JSON.parse(resultText);
    expect(sessionInfo.uuid).toBeTruthy();
    expect(sessionInfo.assistant).toBe('opencode');
  });
});
