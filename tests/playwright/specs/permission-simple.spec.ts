import { test, expect, Page } from '@playwright/test';

/**
 * Simple permission dialog tests for swe-swe
 * Tests the core permission handling behaviors
 */

// Helper to send a message and press Enter
async function sendMessage(page: Page, message: string) {
  // Find the textbox using role selector
  const textbox = page.getByRole('textbox', { name: 'Type a message' });
  
  // Wait for textbox to be ready
  await textbox.waitFor({ state: 'visible', timeout: 5000 });
  
  // Clear any existing text and type the message
  await textbox.clear();
  await textbox.fill(message);
  
  // Small delay to ensure text is entered
  await page.waitForTimeout(500);
  
  // Press Enter to send
  await textbox.press('Enter');
  
  console.log(`Sent message: ${message}`);
}

// Helper to wait for permission dialog
async function waitForPermissionDialog(page: Page) {
  // Look for the permission inline container
  const permissionDiv = page.locator('.permission-inline').first();
  await expect(permissionDiv).toBeVisible({ timeout: 15000 });
  return permissionDiv;
}

test.describe('Permission Dialog Tests', () => {
  test.beforeEach(async ({ page }) => {
    // Enable console logging for debugging
    page.on('console', msg => {
      if (msg.type() === 'error') {
        console.error('Browser console error:', msg.text());
      }
    });
    
    // Navigate to the app
    console.log('Navigating to http://localhost:7000');
    await page.goto('http://localhost:7000', { waitUntil: 'networkidle' });
    
    // Wait for the app to load (textbox should be visible)
    console.log('Waiting for textbox to be visible...');
    const textbox = page.getByRole('textbox', { name: 'Type a message' });
    await textbox.waitFor({ state: 'visible', timeout: 15000 });
    
    // Give the app a moment to fully initialize
    await page.waitForTimeout(1000);
    console.log('Page loaded and ready');
  });

  test('Test 1: Basic permission detection - Write file command', async ({ page }) => {
    console.log('Starting Test 1: Basic permission detection');
    
    // Send a command that requires Write permission
    const timestamp = Date.now();
    await sendMessage(page, `Create a test file at ./tmp/swe-swe-test-${timestamp}.txt with the content 'Hello from Playwright test'`);
    
    // Wait for permission dialog to appear
    const permissionDiv = await waitForPermissionDialog(page);
    
    // Verify the permission dialog contains expected elements
    await expect(permissionDiv).toContainText('Allow', { ignoreCase: true });
    
    // Check for Allow/Deny buttons using role selectors
    const allowButton = page.getByRole('button', { name: 'Y', exact: true });
    const denyButton = page.getByRole('button', { name: 'N', exact: true });
    
    await expect(allowButton).toBeVisible();
    await expect(denyButton).toBeVisible();
    
    console.log('✅ Test 1 passed: Permission dialog detected successfully');
  });

  test('Test 2: Permission grant flow', async ({ page }) => {
    console.log('Starting Test 2: Permission grant flow');
    
    // Send a Write command that reliably triggers permission
    const timestamp = Date.now();
    await sendMessage(page, `Create a test file at ./tmp/grant-flow-test-${timestamp}.txt with content "Permission grant test"`);
    
    // Wait for permission dialog
    await waitForPermissionDialog(page);
    
    // Count chat items before granting permission
    const chatItemsBefore = await page.locator('.chat-item').count();
    
    // Click Allow button
    const allowButton = page.getByRole('button', { name: 'Y', exact: true });
    await allowButton.click();
    
    // Verify permission dialog disappears
    await expect(page.locator('.permission-inline')).not.toBeVisible({ timeout: 5000 });
    
    // Wait for Claude to continue and complete the task
    await page.waitForTimeout(5000);
    
    // Look for "Allowed" confirmation
    const allowedMessage = page.locator('text=/✓ Allowed/i');
    await expect(allowedMessage).toBeVisible({ timeout: 5000 });
    
    console.log('✅ Test 2 passed: Permission grant flow works correctly');
  });

  test('Test 3: Permission deny flow', async ({ page }) => {
    console.log('Starting Test 3: Permission deny flow');
    
    // Send a Write command that reliably triggers permission
    const timestamp = Date.now();
    await sendMessage(page, `Create a test file at ./tmp/deny-flow-test-${timestamp}.txt with content "Permission deny test"`);
    
    // Wait for permission dialog
    await waitForPermissionDialog(page);
    
    // Click Deny button
    const denyButton = page.getByRole('button', { name: 'N', exact: true });
    await denyButton.click();
    
    // Verify permission dialog disappears
    await expect(page.locator('.permission-inline')).not.toBeVisible({ timeout: 5000 });
    
    // Wait for error message to appear
    await page.waitForTimeout(2000);
    
    // Look for denial indicator (✗ Denied message)
    const denialMessage = page.locator('text=/✗ Denied/i');
    await expect(denialMessage).toBeVisible({ timeout: 5000 });
    
    console.log('✅ Test 3 passed: Permission deny flow works correctly');
  });

  test('Test 4: No duplicate permission dialogs', async ({ page }) => {
    console.log('Starting Test 4: No duplicate permission dialogs');
    
    // Send a Write command that reliably triggers permission
    const timestamp = Date.now();
    await sendMessage(page, `Create a test file at ./tmp/duplicate-check-${timestamp}.txt with content "No duplicates test"`);
    
    // Wait for first permission dialog
    await waitForPermissionDialog(page);
    
    // Count permission dialogs (should be exactly 1)
    const permissionDialogCount = await page.locator('.permission-inline').count();
    expect(permissionDialogCount).toBe(1);
    
    // Wait a bit to ensure no duplicate dialogs appear
    await page.waitForTimeout(3000);
    
    // Check again - should still be 1
    const permissionDialogCountAfter = await page.locator('.permission-inline').count();
    expect(permissionDialogCountAfter).toBe(1);
    
    // Grant permission to clean up
    const allowButton = page.getByRole('button', { name: 'Y', exact: true });
    await allowButton.click();
    
    console.log('✅ Test 4 passed: No duplicate permission dialogs');
  });

  test('Test 5: Process stops during permission wait', async ({ page }) => {
    console.log('Starting Test 5: Process stops during permission wait');
    
    // Count initial chat items
    const initialChatCount = await page.locator('.chat-item').count();
    
    // Send a Write command that reliably triggers permission
    const timestamp = Date.now();
    await sendMessage(page, `Create a test file at ./tmp/wait-test-${timestamp}.txt with content "Process stop test"`);
    
    // Wait for permission dialog
    await waitForPermissionDialog(page);
    
    // Record chat item count when dialog appears
    const countAtDialog = await page.locator('.chat-item').count();
    
    // Wait 5 seconds without interacting with the dialog
    console.log('Waiting 5 seconds to verify process is stopped...');
    await page.waitForTimeout(5000);
    
    // Count chat items again - should be the same (process stopped)
    const countAfterWait = await page.locator('.chat-item').count();
    expect(countAfterWait).toBe(countAtDialog);
    
    // No new output should have appeared while waiting
    console.log(`Chat items: Initial=${initialChatCount}, AtDialog=${countAtDialog}, AfterWait=${countAfterWait}`);
    
    // Clean up - deny the permission
    const denyButton = page.getByRole('button', { name: 'N', exact: true });
    await denyButton.click();
    
    console.log('✅ Test 5 passed: Process stops while waiting for permission');
  });
});

// Export for potential reuse
export { sendMessage, waitForPermissionDialog };