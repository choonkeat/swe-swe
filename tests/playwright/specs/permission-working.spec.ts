import { test, expect, Page } from '@playwright/test';

/**
 * Permission dialog tests using Write commands that reliably trigger permissions
 * All tests use variations of "Create a test file" which we know works
 */

// Helper to send a message and press Enter
async function sendMessage(page: Page, message: string) {
  const textbox = page.getByRole('textbox', { name: 'Type a message' });
  await textbox.waitFor({ state: 'visible', timeout: 5000 });
  await textbox.clear();
  await textbox.fill(message);
  await page.waitForTimeout(500);
  await textbox.press('Enter');
  console.log(`Sent message: ${message}`);
}

// Helper to wait for permission dialog
async function waitForPermissionDialog(page: Page) {
  const permissionDiv = page.locator('.permission-inline').first();
  await expect(permissionDiv).toBeVisible({ timeout: 15000 });
  return permissionDiv;
}

test.describe('Permission Dialog Tests - All Using Write Commands', () => {
  test.beforeEach(async ({ page }) => {
    console.log('Navigating to http://localhost:7000');
    await page.goto('http://localhost:7000', { waitUntil: 'networkidle' });
    
    const textbox = page.getByRole('textbox', { name: 'Type a message' });
    await textbox.waitFor({ state: 'visible', timeout: 15000 });
    await page.waitForTimeout(1000);
    console.log('Page loaded and ready');
  });

  test('Test 1: Basic permission detection', async ({ page }) => {
    console.log('Test 1: Verifying permission dialog appears');
    
    // Use Write command that we know triggers permission
    await sendMessage(page, `Create a test file at /tmp/test-${Date.now()}.txt with content "Test 1"`);
    
    // Verify permission dialog appears
    const permissionDiv = await waitForPermissionDialog(page);
    await expect(permissionDiv).toContainText('Allow', { ignoreCase: true });
    
    // Verify buttons exist
    const allowButton = page.getByRole('button', { name: 'Y', exact: true });
    const denyButton = page.getByRole('button', { name: 'N', exact: true });
    await expect(allowButton).toBeVisible();
    await expect(denyButton).toBeVisible();
    
    console.log('✅ Test 1 passed: Permission dialog detected');
  });

  test('Test 2: Permission grant flow', async ({ page }) => {
    console.log('Test 2: Verifying grant flow works');
    
    // Use Write command 
    await sendMessage(page, `Create a test file at /tmp/grant-test-${Date.now()}.txt with content "Grant test"`);
    
    // Wait for dialog
    await waitForPermissionDialog(page);
    
    // Grant permission
    const allowButton = page.getByRole('button', { name: 'Y', exact: true });
    await allowButton.click();
    
    // Verify dialog disappears
    await expect(page.locator('.permission-inline')).not.toBeVisible({ timeout: 5000 });
    
    // Wait for success message (Claude should continue)
    await page.waitForTimeout(3000);
    
    // Look for "Allowed" confirmation
    const allowedMessage = page.locator('text=/✓ Allowed/i');
    await expect(allowedMessage).toBeVisible({ timeout: 5000 });
    
    console.log('✅ Test 2 passed: Grant flow works');
  });

  test('Test 3: Permission deny flow', async ({ page }) => {
    console.log('Test 3: Verifying deny flow works');
    
    // Use Write command
    await sendMessage(page, `Create a test file at /tmp/deny-test-${Date.now()}.txt with content "Deny test"`);
    
    // Wait for dialog
    await waitForPermissionDialog(page);
    
    // Deny permission
    const denyButton = page.getByRole('button', { name: 'N', exact: true });
    await denyButton.click();
    
    // Verify dialog disappears
    await expect(page.locator('.permission-inline')).not.toBeVisible({ timeout: 5000 });
    
    // Look for "Denied" confirmation
    await page.waitForTimeout(2000);
    const deniedMessage = page.locator('text=/✗ Denied/i');
    await expect(deniedMessage).toBeVisible({ timeout: 5000 });
    
    console.log('✅ Test 3 passed: Deny flow works');
  });

  test('Test 4: No duplicate permission dialogs', async ({ page }) => {
    console.log('Test 4: Verifying no duplicate dialogs');
    
    // Use Write command
    await sendMessage(page, `Create a test file at /tmp/duplicate-test-${Date.now()}.txt with content "No duplicates"`);
    
    // Wait for dialog
    await waitForPermissionDialog(page);
    
    // Count dialogs - should be exactly 1
    let dialogCount = await page.locator('.permission-inline').count();
    expect(dialogCount).toBe(1);
    
    // Wait to ensure no duplicates appear
    await page.waitForTimeout(3000);
    dialogCount = await page.locator('.permission-inline').count();
    expect(dialogCount).toBe(1);
    
    // Clean up - grant permission
    const allowButton = page.getByRole('button', { name: 'Y', exact: true });
    await allowButton.click();
    
    console.log('✅ Test 4 passed: No duplicate dialogs');
  });

  test('Test 5: Process stops during permission wait', async ({ page }) => {
    console.log('Test 5: Verifying process stops while waiting');
    
    // Get initial message count
    const initialCount = await page.locator('.chat-item, .message-content').count();
    
    // Use Write command
    await sendMessage(page, `Create a test file at /tmp/wait-test-${Date.now()}.txt with content "Process should stop"`);
    
    // Wait for dialog
    await waitForPermissionDialog(page);
    
    // Record count when dialog appears
    const countAtDialog = await page.locator('.chat-item, .message-content').count();
    
    // Wait 5 seconds WITHOUT interacting
    console.log('Waiting 5 seconds to verify process is stopped...');
    await page.waitForTimeout(5000);
    
    // Count should be the same (no new messages)
    const countAfterWait = await page.locator('.chat-item, .message-content').count();
    expect(countAfterWait).toBe(countAtDialog);
    
    console.log(`Message counts - Initial: ${initialCount}, At dialog: ${countAtDialog}, After wait: ${countAfterWait}`);
    
    // Clean up - deny
    const denyButton = page.getByRole('button', { name: 'N', exact: true });
    await denyButton.click();
    
    console.log('✅ Test 5 passed: Process stops during wait');
  });

});