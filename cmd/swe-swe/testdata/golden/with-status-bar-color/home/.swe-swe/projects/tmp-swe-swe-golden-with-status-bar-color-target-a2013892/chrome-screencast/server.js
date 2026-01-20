const http = require('http');
const fs = require('fs');
const path = require('path');
const { WebSocketServer } = require('ws');
const { chromium } = require('playwright-core');

const PORT = process.env.PORT || 6080;
const CHROME_CDP_URL = process.env.CHROME_CDP_URL || 'http://127.0.0.1:9222';

// Default viewport size
let viewportWidth = 1280;
let viewportHeight = 720;

// Connected WebSocket clients
const clients = new Set();

// Browser and page references
let browser = null;
let page = null;
let cdpSession = null;

// Serve static files
function serveStatic(req, res) {
  let filePath = req.url === '/' ? '/index.html' : req.url;
  filePath = path.join(__dirname, 'static', filePath);

  const ext = path.extname(filePath);
  const contentTypes = {
    '.html': 'text/html',
    '.js': 'application/javascript',
    '.css': 'text/css',
  };

  fs.readFile(filePath, (err, data) => {
    if (err) {
      res.writeHead(404);
      res.end('Not found');
      return;
    }
    res.writeHead(200, { 'Content-Type': contentTypes[ext] || 'text/plain' });
    res.end(data);
  });
}

// Initialize browser connection
async function initBrowser() {
  console.log(`Connecting to Chrome at ${CHROME_CDP_URL}...`);

  try {
    browser = await chromium.connectOverCDP(CHROME_CDP_URL);
    console.log('Connected to Chrome');

    // Get existing context or create new one
    const contexts = browser.contexts();
    const context = contexts.length > 0 ? contexts[0] : await browser.newContext();

    // Get existing page or create new one
    const pages = context.pages();
    page = pages.length > 0 ? pages[0] : await context.newPage();

    // Set initial viewport
    await page.setViewportSize({ width: viewportWidth, height: viewportHeight });

    // Navigate to a default page if blank
    if (page.url() === 'about:blank') {
      await page.goto('https://example.com');
    }

    // Create CDP session for screencast
    cdpSession = await page.context().newCDPSession(page);

    // Start screencast
    await startScreencast();

    console.log('Browser initialized and screencast started');
  } catch (err) {
    console.error('Failed to connect to Chrome:', err.message);
    // Retry connection after delay
    setTimeout(initBrowser, 5000);
  }
}

// Start CDP screencast
async function startScreencast() {
  if (!cdpSession) return;

  // Handle screencast frames
  cdpSession.on('Page.screencastFrame', async (params) => {
    const { data, sessionId } = params;

    // Acknowledge the frame
    try {
      await cdpSession.send('Page.screencastFrameAck', { sessionId });
    } catch (err) {
      // Ignore ack errors
    }

    // Broadcast to all connected clients
    const message = JSON.stringify({ type: 'frame', data });
    for (const client of clients) {
      if (client.readyState === 1) { // WebSocket.OPEN
        client.send(message);
      }
    }
  });

  // Start the screencast
  await cdpSession.send('Page.startScreencast', {
    format: 'jpeg',
    quality: 80,
    maxWidth: viewportWidth,
    maxHeight: viewportHeight,
    everyNthFrame: 1,
  });
}

// Handle viewport resize
async function handleResize(width, height) {
  if (!page) return;

  viewportWidth = width;
  viewportHeight = height;

  try {
    await page.setViewportSize({ width, height });

    // Restart screencast with new dimensions
    if (cdpSession) {
      await cdpSession.send('Page.stopScreencast');
      await cdpSession.send('Page.startScreencast', {
        format: 'jpeg',
        quality: 80,
        maxWidth: width,
        maxHeight: height,
        everyNthFrame: 1,
      });
    }

    console.log(`Viewport resized to ${width}x${height}`);
  } catch (err) {
    console.error('Failed to resize viewport:', err.message);
  }
}

// Handle navigation request
async function handleNavigate(url) {
  if (!page) return;

  try {
    await page.goto(url);
    console.log(`Navigated to ${url}`);
  } catch (err) {
    console.error('Failed to navigate:', err.message);
  }
}

// Create HTTP server
const server = http.createServer(serveStatic);

// Create WebSocket server
const wss = new WebSocketServer({ server, path: '/websockify' });

wss.on('connection', (ws) => {
  console.log('Client connected');
  clients.add(ws);

  ws.on('message', async (message) => {
    try {
      const msg = JSON.parse(message);

      switch (msg.type) {
        case 'resize':
          await handleResize(msg.width, msg.height);
          break;
        case 'navigate':
          await handleNavigate(msg.url);
          break;
        default:
          console.log('Unknown message type:', msg.type);
      }
    } catch (err) {
      console.error('Failed to parse message:', err.message);
    }
  });

  ws.on('close', () => {
    console.log('Client disconnected');
    clients.delete(ws);
  });

  ws.on('error', (err) => {
    console.error('WebSocket error:', err.message);
    clients.delete(ws);
  });
});

// Start server
server.listen(PORT, () => {
  console.log(`Screencast server listening on port ${PORT}`);
  initBrowser();
});

// Graceful shutdown
process.on('SIGTERM', async () => {
  console.log('Shutting down...');
  if (cdpSession) {
    try {
      await cdpSession.send('Page.stopScreencast');
    } catch (err) {
      // Ignore
    }
  }
  if (browser) {
    await browser.close();
  }
  server.close();
  process.exit(0);
});
