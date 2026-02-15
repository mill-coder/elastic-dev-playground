const http = require('http');
const https = require('https');
const fs = require('fs');
const path = require('path');

const STATIC_DIR = process.env.STATIC_DIR || './dist';
const PORT = parseInt(process.env.PORT || '3000', 10);

const MIME_TYPES = {
  '.html': 'text/html',
  '.js': 'application/javascript',
  '.css': 'text/css',
  '.json': 'application/json',
  '.wasm': 'application/wasm',
  '.png': 'image/png',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon',
};

function proxyRequest(req, res, prefix, headerName, label) {
  const targetBase = req.headers[headerName];
  if (!targetBase) {
    res.writeHead(400, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ error: `Missing ${headerName} header` }));
    return;
  }

  const urlPath = req.url.replace(prefix, '');
  const target = new URL(urlPath, targetBase);
  const isHttps = target.protocol === 'https:';
  const transport = isHttps ? https : http;

  const headers = { ...req.headers };
  delete headers[headerName];
  delete headers.host;
  headers.host = target.host;

  const proxyReq = transport.request(
    target.href,
    {
      method: req.method,
      headers,
      ...(isHttps ? { agent: new https.Agent({ rejectUnauthorized: false }) } : {}),
    },
    (proxyRes) => {
      res.writeHead(proxyRes.statusCode, proxyRes.headers);
      proxyRes.pipe(res);
    }
  );

  proxyReq.on('error', (err) => {
    console.error(`${label} proxy error:`, err.message);
    res.writeHead(502, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ error: `Cannot connect to ${label}: ${err.message}` }));
  });

  req.pipe(proxyReq);
}

function serveStatic(req, res) {
  let filePath = path.join(STATIC_DIR, req.url === '/' ? 'index.html' : req.url);
  // Strip query string
  filePath = filePath.split('?')[0];

  fs.stat(filePath, (err, stats) => {
    if (err || !stats.isFile()) {
      // SPA fallback: serve index.html for unknown routes
      const indexPath = path.join(STATIC_DIR, 'index.html');
      fs.readFile(indexPath, (err2, data) => {
        if (err2) {
          res.writeHead(404, { 'Content-Type': 'text/plain' });
          res.end('Not Found');
          return;
        }
        res.writeHead(200, { 'Content-Type': 'text/html' });
        res.end(data);
      });
      return;
    }

    const ext = path.extname(filePath);
    const contentType = MIME_TYPES[ext] || 'application/octet-stream';
    const stream = fs.createReadStream(filePath);
    res.writeHead(200, { 'Content-Type': contentType });
    stream.pipe(res);
    stream.on('error', () => {
      res.writeHead(500);
      res.end('Internal Server Error');
    });
  });
}

const server = http.createServer((req, res) => {
  if (req.url.startsWith('/kibana-api/')) {
    return proxyRequest(req, res, '/kibana-api', 'x-kibana-target', 'Kibana');
  }
  if (req.url.startsWith('/es-api/')) {
    return proxyRequest(req, res, '/es-api', 'x-es-target', 'Elasticsearch');
  }
  serveStatic(req, res);
});

server.listen(PORT, () => {
  console.log(`Server listening on port ${PORT}, serving ${path.resolve(STATIC_DIR)}`);
});
