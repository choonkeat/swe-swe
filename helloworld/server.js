const http = require('http');
const fs = require('fs');
const path = require('path');
const WebSocket = require('ws');
const chokidar = require('chokidar');

const PORT = process.env.PORT || 3000;
const PUBLIC_DIR = path.join(__dirname, 'public');

// Create HTTP server
const server = http.createServer((req, res) => {
    let filePath = path.join(PUBLIC_DIR, req.url === '/' ? 'index.html' : req.url);
    
    // Security check to prevent directory traversal
    if (!filePath.startsWith(PUBLIC_DIR)) {
        res.writeHead(403);
        res.end('Forbidden');
        return;
    }
    
    const extname = path.extname(filePath);
    let contentType = 'text/html';
    
    switch (extname) {
        case '.css':
            contentType = 'text/css';
            break;
        case '.js':
            contentType = 'text/javascript';
            break;
        case '.json':
            contentType = 'application/json';
            break;
        case '.png':
            contentType = 'image/png';
            break;
        case '.jpg':
            contentType = 'image/jpg';
            break;
        case '.ico':
            contentType = 'image/x-icon';
            break;
    }
    
    fs.readFile(filePath, (err, content) => {
        if (err) {
            if (err.code === 'ENOENT') {
                res.writeHead(404);
                res.end('File not found');
            } else {
                res.writeHead(500);
                res.end('Server error');
            }
        } else {
            res.writeHead(200, { 'Content-Type': contentType });
            res.end(content, 'utf-8');
        }
    });
});

// Create WebSocket server
const wss = new WebSocket.Server({ server, path: '/ws' });

// Store connected clients
const clients = new Set();

wss.on('connection', (ws) => {
    console.log('🔌 Client connected to hot reload WebSocket');
    clients.add(ws);
    
    ws.on('close', () => {
        console.log('❌ Client disconnected from hot reload WebSocket');
        clients.delete(ws);
    });
    
    ws.on('error', (error) => {
        console.error('WebSocket error:', error);
        clients.delete(ws);
    });
});

// File watcher for hot reload
const watcher = chokidar.watch(PUBLIC_DIR, {
    ignored: /[\/\\]\./,
    persistent: true,
    ignoreInitial: true
});

watcher.on('change', (filePath) => {
    console.log(`🔄 File changed: ${filePath}`);
    
    // Notify all connected clients to reload
    clients.forEach(client => {
        if (client.readyState === WebSocket.OPEN) {
            client.send('reload');
        }
    });
});

watcher.on('add', (filePath) => {
    console.log(`➕ File added: ${filePath}`);
    
    // Notify all connected clients to reload
    clients.forEach(client => {
        if (client.readyState === WebSocket.OPEN) {
            client.send('reload');
        }
    });
});

watcher.on('unlink', (filePath) => {
    console.log(`🗑️  File deleted: ${filePath}`);
    
    // Notify all connected clients to reload
    clients.forEach(client => {
        if (client.readyState === WebSocket.OPEN) {
            client.send('reload');
        }
    });
});

// Start server
server.listen(PORT, '0.0.0.0', () => {
    console.log(`🚀 Hello World server running on http://0.0.0.0:${PORT}`);
    console.log(`📁 Serving files from: ${PUBLIC_DIR}`);
    console.log(`👀 Watching for file changes...`);
});

// Graceful shutdown
process.on('SIGTERM', () => {
    console.log('📴 Shutting down server...');
    watcher.close();
    server.close(() => {
        console.log('✅ Server shut down gracefully');
        process.exit(0);
    });
});

process.on('SIGINT', () => {
    console.log('📴 Shutting down server...');
    watcher.close();
    server.close(() => {
        console.log('✅ Server shut down gracefully');
        process.exit(0);
    });
});