import { test, expect } from '@playwright/test';
import crypto from 'crypto';

// Helper: login and return authenticated page
async function login(page) {
  await page.goto('/swe-swe-auth/login');
  const password = process.env.SWE_SWE_PASSWORD || 'changeme';
  await page.fill('input[type="password"]', password);
  await page.click('button[type="submit"]');
  await expect(page).toHaveURL(/\/$/);
}

test.describe('Agent Browser E2E', () => {
  test('OpenCode chat session: playwright visits example.com and takes screenshot', async ({ page }) => {
    await login(page);

    // Create a chat session with OpenCode by navigating directly
    const uuid = crypto.randomUUID();
    await page.goto(`/session/${uuid}?assistant=opencode&session=chat`);

    // Wait for the Agent Chat tab to become visible and active
    const chatTab = page.locator('button[data-left-tab="chat"]');
    await expect(chatTab).toBeVisible({ timeout: 30_000 });

    // Wait for the agent-chat iframe to load
    const chatIframe = page.frameLocator('.terminal-ui__agent-chat-iframe');

    // Wait for the chat input to be enabled (agent-chat app is ready)
    const chatInput = chatIframe.locator('#chat-input');
    await expect(chatInput).toBeEnabled({ timeout: 60_000 });

    // Type the prompt and send
    await chatInput.fill('use playwright to visit example.com and take a screenshot');
    await chatIframe.locator('#btn-send').click();

    // Wait for a reply bubble with a screenshot image
    // The agent processes the request: OpenCode → Playwright MCP → mcp-lazy-init
    // triggers browser start → Chrome boots → navigates → screenshot → image in chat
    const screenshotImg = chatIframe.locator('.bubble.agent.canvas-bubble img');
    await expect(screenshotImg).toBeVisible({ timeout: 120_000 });

    // Verify the image has a valid src (data URI or URL)
    const src = await screenshotImg.getAttribute('src');
    expect(src).toBeTruthy();
    expect(src.length).toBeGreaterThan(100); // data URIs are long

    // Also verify the Agent View tab appeared (browser was started)
    const browserTab = page.locator('.terminal-ui__panel-option[value="browser"]');
    await expect(browserTab).toBeVisible({ timeout: 5_000 });
  });
});
