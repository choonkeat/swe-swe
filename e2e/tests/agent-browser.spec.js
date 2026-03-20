import { test, expect } from '@playwright/test';
import crypto from 'crypto';

const PASSWORD = process.env.SWE_SWE_PASSWORD || 'changeme';

// Helper: login
async function login(page) {
  await page.goto('/swe-swe-auth/login');
  await page.fill('input[type="password"]', PASSWORD);
  await Promise.all([
    page.waitForNavigation(),
    page.click('button[type="submit"]'),
  ]);
}

test.describe('Agent Browser E2E', () => {
  test('OpenCode chat session: playwright visits example.com and takes screenshot', async ({ page }) => {
    await login(page);

    // Create a chat session with OpenCode
    const uuid = crypto.randomUUID();
    await page.goto(`/session/${uuid}?assistant=opencode&session=chat`);

    // Agent Chat tab should NOT be visible immediately (deferred until MCP probe succeeds)
    const chatTab = page.locator('button[data-left-tab="chat"]');
    await expect(chatTab).toBeHidden({ timeout: 2_000 });

    // Terminal should be the active view initially
    const terminalEl = page.locator('.terminal-ui__terminal');
    await expect(terminalEl).toBeVisible({ timeout: 5_000 });

    // Wait for Agent Chat tab to become visible (after MCP probe succeeds)
    await expect(chatTab).toBeVisible({ timeout: 60_000 });

    // Point-in-time check: when the tab first appears, the iframe should
    // already show real chat content -- NOT the "Waiting for Agent Chat" placeholder.
    // We use page.evaluate (not Playwright's auto-retrying expect) so we get
    // a snapshot of the DOM right now, without waiting/retrying.
    const iframeEl = await page.locator('.terminal-ui__agent-chat-iframe').elementHandle();
    const iframeContent = await iframeEl.contentFrame();
    const bodyText = await iframeContent.evaluate(() => document.body.innerText);
    expect(bodyText).toContain('[system] Connected');

    await chatTab.click();

    // Wait for the agent-chat iframe to load
    const chatIframe = page.frameLocator('.terminal-ui__agent-chat-iframe');
    const chatInput = chatIframe.locator('#chat-input');
    await expect(chatInput).toBeEnabled({ timeout: 60_000 });

    // Send the prompt
    await chatInput.fill('use playwright to visit example.com and take a screenshot');
    await chatIframe.locator('#btn-send').click();

    // Poll: click first quick reply (confirmation), then wait for agent response
    const quickReplyBtn = chatIframe.locator('#quick-replies .chip');
    const anyImg = chatIframe.locator('.bubble.agent img');
    let confirmClicked = false;
    let found = false;

    for (let i = 0; i < 24; i++) {
      await page.waitForTimeout(5_000);

      // Click the first confirmation prompt only (e.g., "Yes, proceed")
      if (!confirmClicked) {
        const btnCount = await quickReplyBtn.count();
        if (btnCount > 0) {
          const firstBtn = quickReplyBtn.first();
          const text = await firstBtn.textContent();
          console.log(`[${(i+1)*5}s] Clicking confirmation: "${text}"`);
          await firstBtn.click();
          confirmClicked = true;
        }
      }

      // Check for inline screenshot image
      if (await anyImg.isVisible().catch(() => false)) {
        console.log(`[${(i+1)*5}s] Inline screenshot image found!`);
        found = true;
        break;
      }

      // Check if agent response mentions screenshot/example.com (file-based screenshot)
      const agentBubbles = chatIframe.locator('.bubble.agent:not(.loading)');
      const count = await agentBubbles.count();
      for (let j = 0; j < count; j++) {
        const text = await agentBubbles.nth(j).textContent();
        if (text && (text.includes('screenshot') || text.includes('Screenshot')) &&
            (text.includes('example.com') || text.includes('Example Domain') || text.includes('.png'))) {
          console.log(`[${(i+1)*5}s] Agent confirmed screenshot: "${text.substring(0, 100)}..."`);
          found = true;
          break;
        }
      }
      if (found) break;
    }

    expect(found).toBe(true);
  });
});
