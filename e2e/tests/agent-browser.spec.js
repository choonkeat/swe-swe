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

    // Wait for Agent Chat tab to become visible and click it
    const chatTab = page.locator('button[data-left-tab="chat"]');
    await expect(chatTab).toBeVisible({ timeout: 30_000 });
    await chatTab.click();

    // Wait for the agent-chat iframe to load
    const chatIframe = page.frameLocator('.terminal-ui__agent-chat-iframe');
    const chatInput = chatIframe.locator('#chat-input');
    await expect(chatInput).toBeEnabled({ timeout: 60_000 });

    // Send the prompt
    await chatInput.fill('use playwright to visit example.com and take a screenshot');
    await chatIframe.locator('#btn-send').click();

    // Poll: click quick replies, dump bubble content, look for screenshot
    const quickReplyBtn = chatIframe.locator('#quick-replies .chip');
    const canvasImg = chatIframe.locator('.bubble.agent.canvas-bubble img');
    const anyImg = chatIframe.locator('.bubble.agent img');

    let found = false;
    for (let i = 0; i < 24; i++) {
      await page.waitForTimeout(5_000);

      // Click any quick reply buttons that appear
      const btnCount = await quickReplyBtn.count();
      if (btnCount > 0) {
        const firstBtn = quickReplyBtn.first();
        const text = await firstBtn.textContent();
        console.log(`[${(i+1)*5}s] Clicking quick reply: "${text}"`);
        await firstBtn.click();
      }

      // Dump agent bubble HTML for debugging
      const agentBubbles = chatIframe.locator('.bubble.agent');
      const bubbleCount = await agentBubbles.count();
      if (bubbleCount > 0) {
        const lastBubble = agentBubbles.last();
        const html = await lastBubble.innerHTML();
        const preview = html.substring(0, 200);
        console.log(`[${(i+1)*5}s] ${bubbleCount} bubbles. Last: ${preview}...`);
      }

      // Check for screenshot image (canvas-bubble or any img in agent bubble)
      if (await canvasImg.isVisible().catch(() => false)) {
        console.log(`[${(i+1)*5}s] Canvas screenshot found!`);
        found = true;
        break;
      }
      if (await anyImg.isVisible().catch(() => false)) {
        console.log(`[${(i+1)*5}s] Image found in agent bubble!`);
        found = true;
        break;
      }
    }

    // Assert: either canvas-bubble img or any img in agent bubble
    if (!found) {
      // Final dump of all bubbles for debugging
      const allBubbles = chatIframe.locator('.bubble');
      const total = await allBubbles.count();
      for (let j = 0; j < total; j++) {
        const b = allBubbles.nth(j);
        const cls = await b.getAttribute('class');
        const html = await b.innerHTML();
        console.log(`Bubble[${j}] class="${cls}" html=${html.substring(0, 300)}`);
      }
    }

    expect(found).toBe(true);
  });
});
