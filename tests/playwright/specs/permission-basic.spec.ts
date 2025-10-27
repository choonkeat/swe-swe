import { test, expect } from '@playwright/test';

/**
 * Minimal test to verify permission dialog appears
 */

test('Permission dialog appears for Write command', async ({ page }) => {
  // Navigate and wait for app
  await page.goto('http://localhost:7000');
  
  // Wait for the textbox using its role and placeholder text
  const textbox = await page.getByRole('textbox', { name: 'Type a message' });
  await textbox.waitFor({ state: 'visible', timeout: 20000 });
  
  console.log('✓ Textbox found');
  
  // Type a command that requires Write permission
  await textbox.fill('Please create a test file at ./tmp/playwright-test.txt with content "Hello"');
  
  // Press Enter to send
  await textbox.press('Enter');
  
  console.log('✓ Message sent');
  
  // Wait for permission dialog to appear
  // Look for the permission inline container
  await page.waitForSelector('.permission-inline', {
    timeout: 30000,
    state: 'visible'
  });
  
  console.log('✓ Permission dialog appeared');
  
  // Verify the dialog contains permission buttons using role selectors
  const allowButton = await page.getByRole('button', { name: 'Y', exact: true });
  const denyButton = await page.getByRole('button', { name: 'N', exact: true });
  
  await expect(allowButton).toBeVisible();
  await expect(denyButton).toBeVisible();
  
  console.log('✓ Permission buttons found');
  
  // Verify we also have the YOLO button for bulk permission
  const yoloButton = await page.getByRole('button', { name: 'YOLO' });
  await expect(yoloButton).toBeVisible();
  
  console.log('✅ Test passed: Permission dialog works correctly');
});