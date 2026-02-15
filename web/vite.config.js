import { defineConfig } from 'vite';
import http from 'node:http';
import https from 'node:https';

function kibanaProxyPlugin() {
  return {
    name: 'kibana-proxy',
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        if (!req.url.startsWith('/kibana-api/')) return next();

        const targetBase = req.headers['x-kibana-target'];
        if (!targetBase) {
          res.writeHead(400, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({ error: 'Missing X-Kibana-Target header' }));
          return;
        }

        const path = req.url.replace('/kibana-api', '');
        const target = new URL(path, targetBase);
        const isHttps = target.protocol === 'https:';
        const transport = isHttps ? https : http;

        const headers = { ...req.headers };
        delete headers['x-kibana-target'];
        delete headers.host;
        headers.host = target.host;

        const proxyReq = transport.request(
          target.href,
          {
            method: req.method,
            headers,
            rejectAuthorized: false,
            ...(isHttps ? { agent: new https.Agent({ rejectUnauthorized: false }) } : {}),
          },
          (proxyRes) => {
            res.writeHead(proxyRes.statusCode, proxyRes.headers);
            proxyRes.pipe(res);
          }
        );

        proxyReq.on('error', (err) => {
          console.error('Kibana proxy error:', err.message);
          res.writeHead(502, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({ error: `Cannot connect to Kibana: ${err.message}` }));
        });

        req.pipe(proxyReq);
      });
    },
  };
}

function esProxyPlugin() {
  return {
    name: 'es-proxy',
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        if (!req.url.startsWith('/es-api/')) return next();

        const targetBase = req.headers['x-es-target'];
        if (!targetBase) {
          res.writeHead(400, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({ error: 'Missing X-ES-Target header' }));
          return;
        }

        const path = req.url.replace('/es-api', '');
        const target = new URL(path, targetBase);
        const isHttps = target.protocol === 'https:';
        const transport = isHttps ? https : http;

        const headers = { ...req.headers };
        delete headers['x-es-target'];
        delete headers.host;
        headers.host = target.host;

        const proxyReq = transport.request(
          target.href,
          {
            method: req.method,
            headers,
            rejectAuthorized: false,
            ...(isHttps ? { agent: new https.Agent({ rejectUnauthorized: false }) } : {}),
          },
          (proxyRes) => {
            res.writeHead(proxyRes.statusCode, proxyRes.headers);
            proxyRes.pipe(res);
          }
        );

        proxyReq.on('error', (err) => {
          console.error('ES proxy error:', err.message);
          res.writeHead(502, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({ error: `Cannot connect to Elasticsearch: ${err.message}` }));
        });

        req.pipe(proxyReq);
      });
    },
  };
}

export default defineConfig({
  root: '.',
  publicDir: 'public',
  plugins: [kibanaProxyPlugin(), esProxyPlugin()],
  build: {
    outDir: '../dist',
    emptyOutDir: true,
  },
});
