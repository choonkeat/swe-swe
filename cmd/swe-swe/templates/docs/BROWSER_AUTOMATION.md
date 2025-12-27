# Browser Automation Guide

This project has browser automation enabled. Claude can interact with web UIs using Playwright connected to a remote Chrome instance running in Docker.

## Quick Start

### 1. Start the Services
```bash
swe-swe up
# This starts both swe-swe-server and the chrome service with noVNC
```

### 2. Access the Browser
Open your browser and navigate to:
```
http://localhost:6080
```

You'll see a VNC viewer showing the Chrome window. Claude will interact with this browser in real-time. Watch as Claude:
- Navigates to websites
- Fills out forms
- Clicks buttons
- Takes screenshots
- Submits data

### 3. Inspect the Browser
If you need to inspect what Claude is doing:
- **Visual inspection**: Use the noVNC viewer at `http://localhost:6080`
- **DevTools inspection**: Chrome DevTools Protocol is exposed at `localhost:9222`

## How Claude Uses It

Claude automatically connects to the remote browser using:

```javascript
const playwright = require('playwright');

const browser = await playwright.chromium.connectOverCDP(
  process.env.BROWSER_WS_ENDPOINT
);
const page = await browser.newPage();

// Now use standard Playwright APIs
await page.goto('https://example.com');
await page.fill('input[type="email"]', 'user@example.com');
await page.click('button[type="submit"]');
const screenshot = await page.screenshot();
```

## Common Patterns

### Waiting for Elements
```javascript
// Wait up to 5 seconds for element to appear
await page.waitForSelector('button.submit', { timeout: 5000 });
await page.click('button.submit');
```

### Handling Timeouts
```javascript
// Increase timeout if navigating to slow page
await page.goto('https://example.com', { waitUntil: 'networkidle2', timeout: 30000 });
```

### Taking Strategic Screenshots
```javascript
// Capture state at important points for debugging
await page.screenshot({ path: '/tmp/form-filled.png' });
await page.click('button[type="submit"]');
await page.screenshot({ path: '/tmp/after-submit.png' });
```

### Waiting for Navigation
```javascript
// Wait for page to load after click
await Promise.all([
  page.waitForNavigation({ waitUntil: 'networkidle2' }),
  page.click('a.next-page')
]);
```

### Extracting Data
```javascript
const data = await page.evaluate(() => {
  return {
    title: document.title,
    url: window.location.href,
    text: document.body.innerText
  };
});
```

## Observability

### noVNC Keyboard Shortcuts
- **Ctrl+Alt+Del**: Send to browser
- **Clipboard**: Click on clipboard icon in noVNC toolbar
- **Zoom**: Use toolbar buttons or browser zoom

### Checking Browser Logs
The browser console output is visible through Chrome DevTools at `localhost:9222`:
```bash
# In your host Chrome:
# 1. Navigate to chrome://inspect
# 2. Point to localhost:9222
# 3. Inspect the page to see console logs
```

### Network Requests
View all network requests in Chrome DevTools to debug API calls, timeouts, or blocked resources.

### Screenshots for Debugging
Ask Claude to take screenshots at key points:
```javascript
// Good practice: screenshot before and after important actions
await page.screenshot({ path: '/tmp/step-1.png' });
await page.click('button.proceed');
await page.waitForNavigation();
await page.screenshot({ path: '/tmp/step-2.png' });
```

## Troubleshooting

### Browser Connection Fails
```
Error: connect ECONNREFUSED 127.0.0.1:9222
```
- Ensure `docker-compose up` is running
- Check that chrome service started: `docker-compose ps`
- Check chrome logs: `docker-compose logs chrome`

### noVNC Viewer Blank
- Wait 5-10 seconds for Xvfb to initialize
- Try refreshing the page
- Check chrome logs for Xvfb errors: `docker-compose logs chrome`

### Slow Interactions
- Increase wait timeouts if pages load slowly
- Check browser network tab in DevTools (chrome://inspect)
- Verify docker resource allocation (CPU, RAM)

### Form Fills Not Working
- Use `await page.waitForSelector()` before interacting
- Try `page.fill()` instead of typing characters
- Check that selectors are correct: `await page.$(selector)`

### Screenshots Not Saving
- Verify /tmp is writable
- Use absolute paths for screenshot files
- Check docker logs for permission errors

## Architecture Notes

- Chrome runs in a separate Docker container (`chrome` service)
- Xvfb provides virtual X11 display (`:99`)
- noVNC exposes the framebuffer at port 6080
- Chrome DevTools Protocol listens on port 9222
- Both services communicate via Docker network (service name: `chrome`)

## Disabling Browser Automation

If you no longer need browser automation:
1. Remove the `chrome` service from `docker-compose.yml`
2. Remove `BROWSER_WS_ENDPOINT` from environment variables
3. Claude will gracefully skip browser-based tasks

## Performance Considerations

- Xvfb adds ~50-100MB RAM overhead
- Browser interactions ~50-100ms slower than native due to container overhead
- Acceptable for most automation tasks
- Not recommended for performance-critical UI testing

## Advanced: Custom Chrome Flags

If you need custom Chrome startup flags, edit `chrome/Dockerfile`:

```dockerfile
DISPLAY=:99 chromium-browser \
  --remote-debugging-port=9222 \
  --no-sandbox \
  --disable-blink-features=AutomationControlled \
  --disable-dev-shm-usage
```

Common flags:
- `--disable-dev-shm-usage`: Reduce memory usage
- `--disable-gpu`: Disable GPU acceleration
- `--single-process`: Run in single process (less secure, use only for testing)

## Further Reading

- [Playwright Documentation](https://playwright.dev)
- [Chrome DevTools Protocol](https://chromedevtools.github.io/devtools-protocol/)
- [noVNC GitHub](https://github.com/novnc/noVNC)
