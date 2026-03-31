import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';

const PASSWORD = process.env.SWE_SWE_PASSWORD || 'changeme';
const BASE_URL = process.env.E2E_BASE_URL || `http://localhost:${process.env.PORT || 3000}`;

// Helper: login and get auth cookie
async function getAuthCookie(request) {
  const loginResp = await request.post(`${BASE_URL}/swe-swe-auth/login`, {
    form: { password: PASSWORD },
    maxRedirects: 0,
  });
  const cookies = loginResp.headers()['set-cookie'] || '';
  const match = cookies.match(/swe-swe-auth=([^;]+)/);
  return match ? match[1] : null;
}

// Helper: get the MCP auth key from the running container
function getMcpAuthKey() {
  // Find the e2e container
  const containerName = execSync(
    `docker ps --filter "name=e2e" --format "{{.Names}}" | head -1`
  ).toString().trim();
  if (!containerName) throw new Error('No e2e container found');

  // Read MCP_AUTH_KEY from the swe-swe-server process environment
  const key = execSync(
    `docker exec ${containerName} sh -c "cat /proc/1/environ | tr '\\0' '\\n' | grep MCP_AUTH_KEY | cut -d= -f2"`
  ).toString().trim();
  if (!key) throw new Error('Could not extract MCP_AUTH_KEY from container');
  return key;
}

// Helper: call an MCP tool via the Streamable HTTP endpoint
async function callMcpTool(request, mcpKey, authCookie, toolName, args) {
  const resp = await request.post(`${BASE_URL}/mcp?key=${mcpKey}`, {
    headers: {
      'Content-Type': 'application/json',
      'Cookie': `swe-swe-auth=${authCookie}`,
    },
    data: {
      jsonrpc: '2.0',
      id: 1,
      method: 'tools/call',
      params: {
        name: toolName,
        arguments: args,
      },
    },
  });
  return resp;
}

test.describe('MCP create_session', () => {
  let mcpKey;
  let authCookie;

  test.beforeAll(async ({ request }) => {
    mcpKey = getMcpAuthKey();
    authCookie = await getAuthCookie(request);
  });

  test('rejects create_session without repo_path', async ({ request }) => {
    const resp = await callMcpTool(request, mcpKey, authCookie, 'create_session', {
      assistant: 'opencode',
    });

    // MCP errors can come as HTTP 200 with JSON-RPC error, or as HTTP error
    const body = await resp.json();
    const hasError =
      resp.status() >= 400 ||
      (body.error && body.error.message && body.error.message.includes('repo_path')) ||
      (body.result && body.result.isError);

    expect(hasError).toBe(true);
    if (body.error) {
      expect(body.error.message).toContain('repo_path');
    }
  });

  test('accepts create_session with repo_path', async ({ request }) => {
    const resp = await callMcpTool(request, mcpKey, authCookie, 'create_session', {
      assistant: 'opencode',
      repo_path: '/workspace',
    });

    const body = await resp.json();
    // Should succeed -- no JSON-RPC error
    expect(body.error).toBeUndefined();

    // The result should contain session info
    const resultText = body.result?.content?.[0]?.text;
    expect(resultText).toBeTruthy();
    const sessionInfo = JSON.parse(resultText);
    expect(sessionInfo.uuid).toBeTruthy();
    expect(sessionInfo.assistant).toBe('opencode');
  });
});
