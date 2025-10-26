import { test, expect, Page } from '@playwright/test';

/**
 * Tests for permission dialog handling to prevent regression of bugs:
 * - bug-permission-detection.md: Permission error not being recognized
 * - bug-permission-dialog-process-continues.md: Process continues while dialog shown
 * - bug-permission-persistence.md: Permission state not persisting
 * - bug-permission-session-retry-cascade.md: Retry cascade on permission errors
 * - bug-permission-granted-full-restart.md: Full restart after permission granted
 */

test.describe('Permission Dialog Handling', () => {
  let page: Page;

  test.beforeEach(async ({ browser }) => {
    // Start with a fresh context for each test
    const context = await browser.newContext();
    page = await context.newPage();
    await page.goto('/');
  });

  test('should detect permission error and show dialog', async () => {
    // Send a message that triggers a permission-required tool
    await page.fill('[data-testid="message-input"]', 'Please write to the file test.txt');
    await page.click('[data-testid="send-button"]');

    // Verify permission dialog appears
    const dialog = page.locator('[data-testid="permission-dialog"]');
    await expect(dialog).toBeVisible({ timeout: 10000 });
    
    // Verify the dialog contains the tool name and details
    await expect(dialog).toContainText('requires approval');
    
    // Verify Claude process output stopped (no new messages while dialog is shown)
    const messageCount = await page.locator('[data-testid="message"]').count();
    await page.waitForTimeout(2000); // Wait to see if more messages appear
    const newMessageCount = await page.locator('[data-testid="message"]').count();
    expect(newMessageCount).toBe(messageCount);
  });

  test('should not retry with other sessions on permission error', async () => {
    // Start a conversation
    await page.fill('[data-testid="message-input"]', 'Hello Claude');
    await page.click('[data-testid="send-button"]');
    await page.waitForSelector('[data-testid="assistant-message"]');

    // Send another message that triggers permission
    await page.fill('[data-testid="message-input"]', 'Please edit the file config.json');
    await page.click('[data-testid="send-button"]');

    // Wait for permission dialog
    const dialog = page.locator('[data-testid="permission-dialog"]');
    await expect(dialog).toBeVisible({ timeout: 10000 });

    // Check console logs to ensure no session retry attempts
    const consoleLogs: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'log') {
        consoleLogs.push(msg.text());
      }
    });

    await page.waitForTimeout(3000);
    
    // Verify no retry messages in logs
    const retryLogs = consoleLogs.filter(log => 
      log.includes('Retrying with older session ID') || 
      log.includes('session retry')
    );
    expect(retryLogs).toHaveLength(0);
  });

  test('should handle permission grant correctly', async () => {
    // Trigger permission dialog
    await page.fill('[data-testid="message-input"]', 'Please create a new file example.md');
    await page.click('[data-testid="send-button"]');

    // Wait for dialog
    const dialog = page.locator('[data-testid="permission-dialog"]');
    await expect(dialog).toBeVisible({ timeout: 10000 });

    // Grant permission
    await page.click('[data-testid="grant-permission-button"]');

    // Verify dialog disappears
    await expect(dialog).not.toBeVisible();

    // Verify Claude continues execution
    await expect(page.locator('[data-testid="assistant-message"]').last()).toContainText(
      /(created|wrote|file)/i, 
      { timeout: 15000 }
    );

    // Verify no duplicate permission dialogs appear
    await page.waitForTimeout(2000);
    await expect(dialog).not.toBeVisible();
  });

  test('should handle permission deny correctly', async () => {
    // Trigger permission dialog
    await page.fill('[data-testid="message-input"]', 'Please delete the file old.txt');
    await page.click('[data-testid="send-button"]');

    // Wait for dialog
    const dialog = page.locator('[data-testid="permission-dialog"]');
    await expect(dialog).toBeVisible({ timeout: 10000 });

    // Deny permission
    await page.click('[data-testid="deny-permission-button"]');

    // Verify dialog disappears
    await expect(dialog).not.toBeVisible();

    // Verify error message appears
    await expect(page.locator('[data-testid="error-message"]')).toContainText(
      /permission.*denied/i,
      { timeout: 10000 }
    );
  });

  test('should handle multiple sequential permission requests', async () => {
    // First permission request
    await page.fill('[data-testid="message-input"]', 'Create file1.txt');
    await page.click('[data-testid="send-button"]');
    
    let dialog = page.locator('[data-testid="permission-dialog"]');
    await expect(dialog).toBeVisible({ timeout: 10000 });
    await page.click('[data-testid="grant-permission-button"]');
    await expect(dialog).not.toBeVisible();

    // Wait for completion
    await page.waitForTimeout(2000);

    // Second permission request
    await page.fill('[data-testid="message-input"]', 'Create file2.txt');
    await page.click('[data-testid="send-button"]');
    
    await expect(dialog).toBeVisible({ timeout: 10000 });
    
    // Verify only one dialog appears (not multiple)
    const dialogCount = await page.locator('[data-testid="permission-dialog"]').count();
    expect(dialogCount).toBe(1);
    
    await page.click('[data-testid="grant-permission-button"]');
    await expect(dialog).not.toBeVisible();
  });

  test('should terminate process immediately on permission error', async () => {
    // Monitor network activity to track Claude process
    const responses: string[] = [];
    page.on('response', response => {
      if (response.url().includes('/api/') && response.status() === 200) {
        responses.push(response.url());
      }
    });

    // Trigger permission
    await page.fill('[data-testid="message-input"]', 'Write to protected.txt');
    await page.click('[data-testid="send-button"]');

    // Wait for permission dialog
    const dialog = page.locator('[data-testid="permission-dialog"]');
    await expect(dialog).toBeVisible({ timeout: 10000 });

    // Record responses after dialog appears
    const responsesBeforeDialog = responses.length;
    await page.waitForTimeout(3000);
    const responsesAfterWait = responses.length;

    // Verify no new API calls after permission dialog
    expect(responsesAfterWait).toBe(responsesBeforeDialog);
  });

  test('should maintain conversation context after permission grant', async () => {
    // Start conversation
    await page.fill('[data-testid="message-input"]', 'Remember the number 42');
    await page.click('[data-testid="send-button"]');
    await page.waitForSelector('[data-testid="assistant-message"]');

    // Trigger permission
    await page.fill('[data-testid="message-input"]', 'Write that number to file.txt');
    await page.click('[data-testid="send-button"]');

    // Handle permission
    const dialog = page.locator('[data-testid="permission-dialog"]');
    await expect(dialog).toBeVisible({ timeout: 10000 });
    await page.click('[data-testid="grant-permission-button"]');

    // Verify context maintained (should write 42, not something else)
    await expect(page.locator('[data-testid="assistant-message"]').last()).toContainText(
      '42',
      { timeout: 15000 }
    );
  });
});