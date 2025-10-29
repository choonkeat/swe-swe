import { test, expect, Page } from '@playwright/test';

/**
 * Comprehensive permission dialog test for swe-swe
 * Single test with multiple assertions to minimize token usage
 * 
 * IMPORTANT: Always use ./tmp (relative) not /tmp (absolute) for test files!
 * The permission system requires relative paths to work properly.
 */

// Common test utilities
async function sendMessage(page: Page, message: string) {
  const textbox = page.getByRole('textbox', { name: 'Type a message' });
  await textbox.waitFor({ state: 'visible', timeout: 5000 });
  await textbox.clear();
  await textbox.fill(message);
  await page.waitForTimeout(500);
  await textbox.press('Enter');
  console.log(`Sent message: ${message}`);
}

async function waitForPermissionDialog(page: Page) {
  const permissionDiv = page.locator('.permission-inline').first();
  await expect(permissionDiv).toBeVisible({ timeout: 15000 });
  return permissionDiv;
}

async function grantPermission(page: Page) {
  const allowButton = page.getByRole('button', { name: 'Y', exact: true });
  await allowButton.click();
  await expect(page.locator('.permission-inline')).not.toBeVisible({ timeout: 5000 });
}

async function denyPermission(page: Page) {
  const denyButton = page.getByRole('button', { name: 'N', exact: true });
  await denyButton.click();
  await expect(page.locator('.permission-inline')).not.toBeVisible({ timeout: 5000 });
}

test.describe('Permission System Tests', () => {
  test.beforeEach(async ({ page }) => {
    console.log('Navigating to http://localhost:7000');
    await page.goto('http://localhost:7000', { waitUntil: 'networkidle' });
    
    const textbox = page.getByRole('textbox', { name: 'Type a message' });
    await textbox.waitFor({ state: 'visible', timeout: 15000 });
    await page.waitForTimeout(1000);
    console.log('Page loaded and ready');
  });

  test('Permission deny flow with session context preservation', async ({ page }) => {
    const timestamp = Date.now();
    const filename = `deny-test-${timestamp}.txt`;
    const content = 'This will be denied';
    
    console.log('Testing permission deny flow...');
    await sendMessage(page, `Create a test file at ./tmp/${filename} with content "${content}"`);
    
    // Verify permission dialog appears
    await waitForPermissionDialog(page);
    
    // Deny the permission
    await denyPermission(page);
    
    // Wait for and verify "Denied" confirmation
    const deniedMessage = page.locator('text=/✗ Denied/i');
    await expect(deniedMessage).toBeVisible({ timeout: 5000 });
    console.log('✅ Permission deny flow works');
    
    // Wait for processing to complete
    const stopButton = page.locator('button:has-text("Stop")');
    await expect(stopButton).not.toBeVisible({ timeout: 10000 });
    
    // Test session context preservation after denial
    console.log('Testing session context preservation after denial...');
    await sendMessage(page, 'What was the exact filename I asked you to create in the previous request?');
    
    // Wait for agent to finish processing the follow-up question
    await expect(stopButton).not.toBeVisible({ timeout: 30000 });
    
    // Get the AI's response
    const botMessages = page.locator('.bot-message .message-content');
    const lastResponse = await botMessages.last().textContent();
    
    console.log(`AI response: "${lastResponse}"`);
    
    // Check if AI remembers the filename
    const remembersFilename = lastResponse?.includes(filename) || false;
    
    console.log(`Remembers filename: ${remembersFilename}`);
    
    // Assert session context is preserved
    expect(remembersFilename).toBe(true);
    console.log('✅ Session context preserved after permission denial');
  });

  test('Permission allow flow with session context preservation', async ({ page }) => {
    const timestamp = Date.now();
    const filename = `allow-test-${timestamp}.txt`;
    const content = 'This will be allowed';
    
    console.log('Testing permission allow flow...');
    await sendMessage(page, `Create a test file at ./tmp/${filename} with content "${content}"`);
    
    // Verify permission dialog appears
    await waitForPermissionDialog(page);
    
    // Allow the permission
    await grantPermission(page);
    
    // Wait for and verify "Allowed" confirmation
    const allowedMessage = page.locator('text=/✓ Allowed/i');
    await expect(allowedMessage).toBeVisible({ timeout: 10000 });
    console.log('✅ Permission allow flow works');
    
    // Wait for processing to complete
    const stopButton = page.locator('button:has-text("Stop")');
    await expect(stopButton).not.toBeVisible({ timeout: 30000 });
    
    // Test session context preservation after permission grant
    console.log('Testing session context preservation after permission grant...');
    await sendMessage(page, 'What was the exact filename and content of the file you just created?');
    
    // Wait for agent to finish processing the follow-up question
    const stopButton2 = page.locator('button:has-text("Stop")');
    await expect(stopButton2).not.toBeVisible({ timeout: 30000 });
    
    // Get the AI's response
    const botMessages = page.locator('.bot-message .message-content');
    const lastResponse = await botMessages.last().textContent();
    
    console.log(`AI response: "${lastResponse}"`);
    
    // Check if AI remembers both filename and content
    const remembersFilename = lastResponse?.includes(filename) || false;
    const remembersContent = lastResponse?.includes(content) || false;
    
    console.log(`Remembers filename: ${remembersFilename}, Remembers content: ${remembersContent}`);
    
    // Assert session context is preserved
    expect(remembersFilename).toBe(true);
    expect(remembersContent).toBe(true);
    console.log('✅ Session context preserved after permission grant');
  });
});

// Export utilities for potential reuse
export { sendMessage, waitForPermissionDialog, grantPermission, denyPermission };