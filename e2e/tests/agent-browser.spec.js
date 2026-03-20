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

    // Agent Chat tab should be visible immediately for ?session=chat,
    // showing a loading indicator while the MCP probe runs.
    const chatTab = page.locator('button[data-left-tab="chat"]');
    await expect(chatTab).toBeVisible({ timeout: 5_000 });

    // Tab should show loading text while probe is in progress
    const loadingSpan = chatTab.locator('.terminal-ui__chat-tab-loading');
    await expect(loadingSpan).toBeVisible({ timeout: 2_000 });

    // Terminal should be the active view initially (chat tab not auto-activated)
    const terminalEl = page.locator('.terminal-ui__terminal');
    await expect(terminalEl).toBeVisible({ timeout: 5_000 });

    // Wait for loading indicator to disappear (probe succeeded, iframe loaded)
    await expect(loadingSpan).toBeHidden({ timeout: 60_000 });

    // After loading completes, the tab should auto-activate
    await expect(chatTab).toHaveClass(/active/, { timeout: 5_000 });

    // The iframe should show real chat content
    const iframeEl = await page.locator('.terminal-ui__agent-chat-iframe').elementHandle();
    const iframeContent = await iframeEl.contentFrame();
    const bodyText = await iframeContent.evaluate(() => document.body.innerText);
    expect(bodyText).toContain('[system] Connected');

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
