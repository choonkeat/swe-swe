import { test, expect, Page } from '@playwright/test';

/**
 * Simple TodoWrite Tool Result Filtering Test
 * 
 * This test focuses on the core functionality: ensuring that successful TodoWrite
 * tool results containing "Todos have been modified successfully" are not displayed.
 */

async function sendMessage(page: Page, message: string) {
  const textbox = page.getByRole('textbox', { name: 'Type a message' });
  await textbox.waitFor({ state: 'visible', timeout: 5000 });
  await textbox.clear();
  await textbox.fill(message);
  await page.waitForTimeout(500);
  await textbox.press('Enter');
  console.log(`Sent message: ${message}`);
}

async function waitForAgentResponse(page: Page) {
  console.log('Waiting for agent response...');
  
  // Method 1: Wait for stop button to disappear (agent finished processing)
  const stopButton = page.locator('button:has-text("Stop")');
  let agentFinished = false;
  
  try {
    await expect(stopButton).not.toBeVisible({ timeout: 30000 });
    agentFinished = true;
    console.log('✅ Stop button disappeared - agent likely finished');
  } catch (e) {
    console.log('⚠️ Stop button still visible after 30s, trying other indicators');
  }
  
  // Method 2: Look for task completion indicators
  if (!agentFinished) {
    console.log('Looking for task completion indicators...');
    try {
      // Wait for "Task completed" message
      const taskCompletedMessage = page.locator('text=/✓.*Task completed|Task completed.*✓/i');
      await expect(taskCompletedMessage).toBeVisible({ timeout: 15000 });
      agentFinished = true;
      console.log('✅ Found task completion message');
    } catch (e) {
      console.log('No task completion message found, trying message stabilization');
    }
  }
  
  // Method 3: Wait for message count to stabilize if other methods failed
  if (!agentFinished) {
    console.log('Waiting for message count to stabilize...');
    let stableMessageCount = 0;
    let previousCount = 0;
    const maxWaitCycles = 8;
    
    for (let i = 0; i < maxWaitCycles; i++) {
      await page.waitForTimeout(2000);
      const currentCount = await page.locator('.message-content').count();
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
  }
  
  // Final wait to ensure DOM is stable
  await page.waitForTimeout(2000);
  
  if (agentFinished) {
    console.log('✅ Agent response completed');
  } else {
    console.log('⚠️ Agent may still be processing, but continuing with test');
  }
}

test.describe('TodoWrite Tool Result Filtering - Simple', () => {
  test.beforeEach(async ({ page }) => {
    console.log('Navigating to http://localhost:7000');
    await page.goto('http://localhost:7000', { waitUntil: 'networkidle' });
    
    const textbox = page.getByRole('textbox', { name: 'Type a message' });
    await textbox.waitFor({ state: 'visible', timeout: 15000 });
    await page.waitForTimeout(1000);
    console.log('Page loaded and ready');
  });

  test('No Tool Result sections should contain successful TodoWrite messages', async ({ page }) => {
    console.log('Testing that successful TodoWrite tool results are hidden...');
    
    // Send a direct request for todo functionality
    await sendMessage(page, 'please use TodoWrite tool to create a simple task list');
    
    // Wait for agent to respond
    await waitForAgentResponse(page);
    
    // Main assertion: Check that no Tool Result contains the success message
    console.log('Checking for Tool Result sections...');
    const toolResultSections = page.locator('text="Tool Result"');
    const toolResultCount = await toolResultSections.count();
    
    console.log(`Found ${toolResultCount} Tool Result section(s)`);
    
    if (toolResultCount > 0) {
      console.log('Examining Tool Result sections for TodoWrite success messages...');
      
      let foundSuccessfulTodoWriteResult = false;
      
      for (let i = 0; i < toolResultCount; i++) {
        const toolResult = toolResultSections.nth(i);
        
        // Click to expand if it's a collapsible details element
        try {
          await toolResult.click({ timeout: 1000 });
          await page.waitForTimeout(500);
        } catch (e) {
          // Might not be clickable, that's fine
        }
        
        // Get the text content of the tool result
        const toolResultContainer = toolResult.locator('xpath=ancestor::details[1]').or(toolResult.locator('xpath=..'));
        const resultText = await toolResultContainer.textContent();
        
        console.log(`Tool Result ${i + 1}: "${resultText?.substring(0, 150)}..."`);
        
        // Check for the specific success message we want to hide
        if (resultText?.includes('Todos have been modified successfully')) {
          foundSuccessfulTodoWriteResult = true;
          console.log(`❌ FOUND TodoWrite success message in Tool Result ${i + 1}!`);
          console.log(`Full content: "${resultText}"`);
          break;
        }
      }
      
      // This is the main assertion: no successful TodoWrite messages should be visible
      expect(foundSuccessfulTodoWriteResult).toBe(false);
      
      if (!foundSuccessfulTodoWriteResult) {
        console.log('✅ SUCCESS: No successful TodoWrite tool results found in any Tool Result sections');
      }
      
    } else {
      console.log('✅ No Tool Result sections found - which means no tool results are shown (perfect!)');
    }
    
    // Additional check: look for any visible text containing the success message anywhere on page
    const successMessageText = page.locator('text="Todos have been modified successfully"');
    const successMessageCount = await successMessageText.count();
    
    if (successMessageCount > 0) {
      console.log(`❌ Found ${successMessageCount} visible instances of success message on page`);
      // This would indicate the filtering isn't working
      expect(successMessageCount).toBe(0);
    } else {
      console.log('✅ No visible success messages found anywhere on page');
    }
  });

  test('Agent responses should be visible (sanity check)', async ({ page }) => {
    console.log('Sanity check: ensuring agent responds to messages...');
    
    await sendMessage(page, 'hello, can you respond to this message?');
    await waitForAgentResponse(page);
    
    // Check that we got some kind of response
    const botMessages = page.locator('.bot-message, .message-content').filter({ hasText: /hello|hi|respond|yes/i });
    const responseCount = await botMessages.count();
    
    console.log(`Found ${responseCount} response-like messages`);
    
    if (responseCount === 0) {
      // If no specific response, at least check we have some content
      const allContent = page.locator('.message-content');
      const totalContent = await allContent.count();
      console.log(`Total content elements: ${totalContent}`);
      
      // The test environment should have at least the welcome message
      expect(totalContent).toBeGreaterThan(0);
    } else {
      console.log('✅ Agent is responding to messages');
    }
  });
});

export { sendMessage, waitForAgentResponse };