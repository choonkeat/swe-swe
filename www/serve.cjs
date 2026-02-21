// Simple SPA static file server for local testing
// Mirrors Netlify _redirects: serves static files, falls back to tdspec.html for /packages/*
const http = require('http');
const fs = require('fs');
const path = require('path');

const PORT = parseInt(process.env.PORT || '8000', 10);
const ROOT = __dirname;

const MIME = {
  '.html': 'text/html',
  '.css': 'text/css',
  '.js': 'application/javascript',
  '.json': 'application/json',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
  '.ico': 'image/x-icon',
  '.md': 'text/plain; charset=utf-8',
};

http.createServer((req, res) => {
  const url = new URL(req.url, 'http://localhost');
  let filePath = path.join(ROOT, url.pathname);

  // Serve directory index
  if (filePath.endsWith('/')) filePath += 'index.html';

  // Try static file first
  if (fs.existsSync(filePath) && fs.statSync(filePath).isFile()) {
    const ext = path.extname(filePath);
    res.writeHead(200, { 'Content-Type': MIME[ext] || 'application/octet-stream' });
    fs.createReadStream(filePath).pipe(res);
    return;
  }

  // SPA fallback for /packages/* routes
  if (url.pathname.startsWith('/packages/')) {
    const spa = path.join(ROOT, 'tdspec.html');
    res.writeHead(200, { 'Content-Type': 'text/html' });
    fs.createReadStream(spa).pipe(res);
    return;
  }

  res.writeHead(404);
  res.end('Not found');
}).listen(PORT, '0.0.0.0', () => {
  console.log(`Serving ${ROOT} on http://0.0.0.0:${PORT}`);
});
