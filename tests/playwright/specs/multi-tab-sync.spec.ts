import { test, expect } from '@playwright/test';

// Helper function to send a message in a page
async function sendMessage(page: any, message: string) {
    await page.getByRole('textbox', { name: 'Type a message...' }).fill(message);
    await page.getByRole('textbox', { name: 'Type a message...' }).press('Enter');
}

// Helper function to wait for an AI response
async function waitForAIResponse(page: any) {
    // Wait for swe-swe sender to appear (indicates AI is responding)
    await page.locator('text=swe-swe').first().waitFor({ state: 'visible', timeout: 10000 });
}

// Helper function to get the latest message content
async function getLatestMessageContent(page: any) {
    // Wait a bit for message to fully render
    await page.waitForTimeout(1000);
    
    // Get all content divs and return the last one
    const contentElements = await page.locator('[class*="content"], .message-content, div').filter({ hasText: /\w+/ }).all();
    if (contentElements.length > 0) {
        return await contentElements[contentElements.length - 1].textContent();
    }
    return '';
}

test.describe('Multi-tab Session Synchronization', () => {
    test('Basic multi-tab synchronization with same Claude session', async ({ context }) => {
        // Generate a unique test session ID
        const testSessionId = `test-session-${Date.now()}`;
        
        // Open two tabs with the same Claude session ID
        const page1 = await context.newPage();
        const page2 = await context.newPage();
        
        console.log(`Testing with session ID: ${testSessionId}`);
        
        await page1.goto(`http://localhost:7000/#claude=${testSessionId}`);
        await page2.goto(`http://localhost:7000/#claude=${testSessionId}`);
        
        // Wait for both pages to load
        await page1.waitForSelector('h1:has-text("swe-swe")', { timeout: 5000 });
        await page2.waitForSelector('h1:has-text("swe-swe")', { timeout: 5000 });
        
        console.log('Both tabs loaded successfully');
        
        // Send a simple message from tab 1
        const testMessage = 'Hello from tab 1 - multi-tab test';
        await sendMessage(page1, testMessage);
        
        console.log('Message sent from tab 1');
        
        // Wait for AI response to start in tab 1
        await waitForAIResponse(page1);
        
        // Check that both tabs show the user message
        const userMessage1 = await page1.locator('text=USER').first().waitFor({ state: 'visible', timeout: 5000 });
        const userMessage2 = await page2.locator('text=USER').first().waitFor({ state: 'visible', timeout: 5000 });
        
        expect(userMessage1).toBeTruthy();
        expect(userMessage2).toBeTruthy();
        
        console.log('✅ Both tabs show USER message - synchronization working!');
        
        // Verify both tabs show the AI response
        const aiResponse1 = await page1.locator('text=swe-swe').first().waitFor({ state: 'visible', timeout: 5000 });
        const aiResponse2 = await page2.locator('text=swe-swe').first().waitFor({ state: 'visible', timeout: 5000 });
        
        expect(aiResponse1).toBeTruthy();
        expect(aiResponse2).toBeTruthy();
        
        console.log('✅ Both tabs show swe-swe response - full synchronization confirmed!');
    });
    
    test('Session switching - tabs with different Claude sessions remain isolated', async ({ context }) => {
        const sessionA = `session-A-${Date.now()}`;
        const sessionB = `session-B-${Date.now()}`;
        
        const page1 = await context.newPage();
        const page2 = await context.newPage();
        
        // Start both tabs with session A
        await page1.goto(`http://localhost:7000/#claude=${sessionA}`);
        await page2.goto(`http://localhost:7000/#claude=${sessionA}`);
        
        await page1.waitForSelector('h1:has-text("swe-swe")', { timeout: 5000 });
        await page2.waitForSelector('h1:has-text("swe-swe")', { timeout: 5000 });
        
        // Switch page2 to session B
        await page2.goto(`http://localhost:7000/#claude=${sessionB}`);
        await page2.waitForSelector('h1:has-text("swe-swe")', { timeout: 5000 });
        
        console.log('Tabs now on different sessions');
        
        // Send message from page1 (session A)
        await sendMessage(page1, 'Message for session A only');
        
        // Wait for response in page1
        await waitForAIResponse(page1);
        
        // Verify page1 has the message
        const page1HasUser = await page1.locator('text=USER').first().isVisible();
        expect(page1HasUser).toBe(true);
        
        // Verify page2 does NOT have the message (different session)
        await page2.waitForTimeout(2000); // Give it time to receive if it would
        const page2HasUser = await page2.locator('text=USER').first().isVisible().catch(() => false);
        expect(page2HasUser).toBe(false);
        
        console.log('✅ Session isolation confirmed - different sessions remain separate');
    });
    
    test('New tab joining existing active session sees ongoing conversation', async ({ context }) => {
        const sessionId = `active-session-${Date.now()}`;
        
        // Start with one tab
        const page1 = await context.newPage();
        await page1.goto(`http://localhost:7000/#claude=${sessionId}`);
        await page1.waitForSelector('h1:has-text("swe-swe")', { timeout: 5000 });
        
        // Send initial message to establish conversation
        await sendMessage(page1, 'Initial message to start conversation');
        await waitForAIResponse(page1);
        
        console.log('Initial conversation started in tab 1');
        
        // Now open second tab with same session
        const page2 = await context.newPage();
        await page2.goto(`http://localhost:7000/#claude=${sessionId}`);
        await page2.waitForSelector('h1:has-text("swe-swe")', { timeout: 5000 });
        
        // Send follow-up message from tab 1
        await sendMessage(page1, 'Follow-up message - should appear in both tabs');
        
        // Both tabs should receive the new message
        await page1.locator('text=Follow-up message').first().waitFor({ state: 'visible', timeout: 5000 });
        await page2.locator('text=Follow-up message').first().waitFor({ state: 'visible', timeout: 5000 });
        
        console.log('✅ New tab successfully joined active session and sees live updates');
    });
});