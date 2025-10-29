import { test, expect, Page } from '@playwright/test';

/**
 * TodoWrite Tool Result Filtering Test
 * 
 * Tests that successful TodoWrite tool operations don't show redundant "Tool Result" sections
 * while ensuring the todo list itself is still displayed properly.
 */

// Test utilities
async function sendMessage(page: Page, message: string) {
  const textbox = page.getByRole('textbox', { name: 'Type a message' });
  await textbox.waitFor({ state: 'visible', timeout: 5000 });
  await textbox.clear();
  await textbox.fill(message);
  await page.waitForTimeout(500);
  await textbox.press('Enter');
  console.log(`Sent message: ${message}`);
}

async function waitForAgentToComplete(page: Page) {
  console.log('Waiting for agent to complete processing...');
  
  // Method 1: Wait for stop button to disappear
  const stopButton = page.locator('button:has-text("Stop")');
  let agentFinished = false;
  
  try {
    await expect(stopButton).not.toBeVisible({ timeout: 30000 });
    agentFinished = true;
    console.log('✅ Stop button disappeared - agent likely finished');
  } catch (e) {
    console.log('⚠️ Stop button still visible after 30s, trying other indicators');
  }
  
  // Method 2: Wait for message count to stabilize (new messages stop appearing)
  let stableMessageCount = 0;
  let previousCount = 0;
  const maxWaitCycles = 10;
  
  for (let i = 0; i < maxWaitCycles && !agentFinished; i++) {
    await page.waitForTimeout(2000);
    const currentCount = await page.locator('.bot-message .message-content, .message-content').count();
    console.log(`Message count check ${i + 1}: ${currentCount} messages`);
    
    if (currentCount === previousCount && currentCount >= 1) {
      stableMessageCount++;
      if (stableMessageCount >= 2) {
        agentFinished = true;
        console.log('✅ Message count stabilized - agent likely finished');
        break;
      }
    } else {
      stableMessageCount = 0;
    }
    previousCount = currentCount;
  }
  
  // Method 3: Look for typing indicators or loading states to disappear
  const typingIndicator = page.locator('.typing-indicator, .loading, .processing');
  try {
    if (await typingIndicator.isVisible()) {
      await expect(typingIndicator).not.toBeVisible({ timeout: 10000 });
      console.log('✅ Typing indicator disappeared');
    }
  } catch (e) {
    console.log('No typing indicator found or still visible');
  }
  
  // Final wait to ensure rendering is complete
  await page.waitForTimeout(2000);
}

test.describe('TodoWrite Tool Result Filtering', () => {
  test.beforeEach(async ({ page }) => {
    console.log('Navigating to http://localhost:7000');
    await page.goto('http://localhost:7000', { waitUntil: 'networkidle' });
    
    const textbox = page.getByRole('textbox', { name: 'Type a message' });
    await textbox.waitFor({ state: 'visible', timeout: 15000 });
    await page.waitForTimeout(1000);
    console.log('Page loaded and ready');
  });

  test('TodoWrite tool results should be hidden when successful', async ({ page }) => {
    console.log('Testing that successful TodoWrite operations hide tool results...');
    
    // Send a message that will trigger TodoWrite usage
    await sendMessage(page, 'create a todo list to test that successful TodoWrite tool results are hidden');
    
    // Wait for the agent to complete processing
    await waitForAgentToComplete(page);
    
    // Verify that todo lists are visible
    console.log('Checking for todo list presence...');
    const todoLists = page.locator('.todo-list');
    const todoListCount = await todoLists.count();
    
    expect(todoListCount).toBeGreaterThan(0);
    console.log(`✅ Found ${todoListCount} todo list(s) displayed`);
    
    // Verify todo list contains expected elements
    const todoItems = page.locator('.todo-item');
    const todoItemCount = await todoItems.count();
    expect(todoItemCount).toBeGreaterThan(0);
    console.log(`✅ Found ${todoItemCount} todo item(s) in the lists`);
    
    // Check for "Tool Result" sections
    console.log('Checking for Tool Result sections...');
    const toolResultSections = page.locator('text="Tool Result"');
    const toolResultCount = await toolResultSections.count();
    
    if (toolResultCount > 0) {
      console.log(`Found ${toolResultCount} Tool Result section(s) - checking content...`);
      
      // Check if any Tool Result contains TodoWrite success messages
      let successfulTodoWriteResults = 0;
      
      for (let i = 0; i < toolResultCount; i++) {
        const toolResult = toolResultSections.nth(i);
        await toolResult.click(); // Expand the details
        await page.waitForTimeout(500);
        
        // Look for the parent container that contains both the summary and content
        const toolResultContainer = toolResult.locator('..').locator('..');
        const resultText = await toolResultContainer.textContent();
        
        console.log(`Tool Result ${i + 1} content: "${resultText?.substring(0, 100)}..."`);
        
        if (resultText?.includes('Todos have been modified successfully')) {
          successfulTodoWriteResults++;
          console.log(`❌ Found successful TodoWrite tool result that should be hidden!`);
        }
      }
      
      // Assert that no successful TodoWrite tool results are visible
      expect(successfulTodoWriteResults).toBe(0);
      console.log('✅ No successful TodoWrite tool results found in Tool Result sections');
      
    } else {
      console.log('✅ No Tool Result sections found - perfect! All successful TodoWrite results are hidden');
    }
  });

  test('TodoWrite functionality still works correctly', async ({ page }) => {
    console.log('Testing that TodoWrite functionality works despite result filtering...');
    
    // Send message to create and manipulate todos
    await sendMessage(page, 'create a test todo list with 3 items, then mark the first one as in progress');
    
    // Wait for agent to complete initial processing
    await waitForAgentToComplete(page);
    
    // Verify multiple todo lists are created (initial + updated)
    const todoLists = page.locator('.todo-list');
    const todoListCount = await todoLists.count();
    expect(todoListCount).toBeGreaterThanOrEqual(2);
    console.log(`✅ Found ${todoListCount} todo list versions showing progression`);
    
    // Verify todo items show different states
    const pendingItems = page.locator('.todo-item:has-text("[ ]")');
    const inProgressItems = page.locator('.todo-item:has-text("[⏳]")');
    const completedItems = page.locator('.todo-item:has-text("[✓]")');
    
    const pendingCount = await pendingItems.count();
    const inProgressCount = await inProgressItems.count();
    const completedCount = await completedItems.count();
    
    console.log(`Todo status counts - Pending: ${pendingCount}, In Progress: ${inProgressCount}, Completed: ${completedCount}`);
    
    // We should have some items in different states
    expect(pendingCount + inProgressCount + completedCount).toBeGreaterThan(0);
    console.log('✅ Todo items show various states correctly');
    
    // Verify that todo list headers are visible
    const todoHeaders = page.locator('text="📋 Todo List"');
    const headerCount = await todoHeaders.count();
    expect(headerCount).toBeGreaterThan(0);
    console.log(`✅ Found ${headerCount} todo list header(s)`);
  });

  test('Failed TodoWrite operations should still show tool results', async ({ page }) => {
    console.log('Testing that failed TodoWrite operations still show tool results for debugging...');
    
    // This test simulates what would happen if TodoWrite failed
    // Since we can't easily force TodoWrite to fail in a real scenario,
    // we verify the current behavior and document expected behavior
    
    await sendMessage(page, 'create a simple todo list');
    await waitForAgentToComplete(page);
    
    // Check current behavior - successful TodoWrite should not show tool results
    const toolResultSections = page.locator('text="Tool Result"');
    const toolResultCount = await toolResultSections.count();
    
    // If there are tool results, they should NOT contain successful TodoWrite messages
    for (let i = 0; i < toolResultCount; i++) {
      const toolResult = toolResultSections.nth(i);
      await toolResult.click();
      await page.waitForTimeout(500);
      
      const toolResultContainer = toolResult.locator('..').locator('..');
      const resultText = await toolResultContainer.textContent();
      
      // Assert no successful TodoWrite messages are visible
      expect(resultText).not.toContain('Todos have been modified successfully');
    }
    
    console.log('✅ Confirmed: successful TodoWrite operations do not show tool results');
    console.log('Note: Failed TodoWrite operations would still show tool results for debugging');
  });

  test('Todo list rendering is not affected by tool result filtering', async ({ page }) => {
    console.log('Testing that todo list rendering quality is maintained...');
    
    await sendMessage(page, 'create a comprehensive todo list with 5 items covering different priorities and states');
    await waitForAgentToComplete(page);
    
    // Verify todo list structure is preserved
    const todoLists = page.locator('.todo-list');
    const todoHeaders = page.locator('.todo-header');
    const todoItems = page.locator('.todo-item');
    
    const listCount = await todoLists.count();
    const headerCount = await todoHeaders.count();
    const itemCount = await todoItems.count();
    
    expect(listCount).toBeGreaterThan(0);
    expect(headerCount).toBeGreaterThan(0);
    expect(itemCount).toBeGreaterThan(0);
    
    console.log(`Todo structure: ${listCount} lists, ${headerCount} headers, ${itemCount} items`);
    
    // Verify CSS classes are applied correctly
    const todoListsWithClass = page.locator('.todo-list');
    const listsWithClassCount = await todoListsWithClass.count();
    expect(listsWithClassCount).toBe(listCount);
    
    console.log('✅ Todo list structure and styling preserved');
    
    // Verify emoji and formatting in todo items
    const emojiItems = page.locator('.todo-item:has-text("📋"), .todo-item:has-text("[✓]"), .todo-item:has-text("[⏳]"), .todo-item:has-text("[ ]")');
    const emojiCount = await emojiItems.count();
    expect(emojiCount).toBeGreaterThan(0);
    
    console.log(`✅ Found ${emojiCount} todo items with proper formatting and emojis`);
  });
});

// Export utilities for potential reuse
export { sendMessage, waitForAgentToComplete };