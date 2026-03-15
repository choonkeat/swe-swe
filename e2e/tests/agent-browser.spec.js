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

    // OpenCode may ask "Shall I proceed?" — click the quick reply to confirm
    // Poll for either a quick reply button or a screenshot image
    const quickReplyBtn = chatIframe.locator('#quick-replies .chip');
    const screenshotImg = chatIframe.locator('.bubble.agent.canvas-bubble img');

    for (let i = 0; i < 24; i++) {
      await page.waitForTimeout(5_000);

      // Click any quick reply buttons that appear (e.g., "Yes, proceed")
      const btnCount = await quickReplyBtn.count();
      if (btnCount > 0) {
        const firstBtn = quickReplyBtn.first();
        const text = await firstBtn.textContent();
        console.log(`[${(i+1)*5}s] Clicking quick reply: "${text}"`);
        await firstBtn.click();
      }

      // Check if screenshot appeared
      if (await screenshotImg.isVisible()) {
        console.log(`[${(i+1)*5}s] Screenshot image found!`);
        break;
      }
    }

    await expect(screenshotImg).toBeVisible({ timeout: 10_000 });

    // Verify the image has a valid src
    const src = await screenshotImg.getAttribute('src');
    expect(src).toBeTruthy();
    expect(src.length).toBeGreaterThan(100);
  });
});
