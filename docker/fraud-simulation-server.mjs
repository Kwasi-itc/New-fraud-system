import { createServer } from 'node:http';
import { randomBytes, timingSafeEqual } from 'node:crypto';
import { readFile } from 'node:fs/promises';

function matches(value, expected) {
  if (typeof value !== 'string') return false;
  const bytes = Buffer.from(value || '');
  return bytes.length === expected.length && timingSafeEqual(bytes, expected);
}

async function jsonBody(req) {
  let size = 0;
  const chunks = [];
  for await (const chunk of req) {
    size += chunk.length;
    if (size > 32768) throw Object.assign(new Error('Dashboard request is too large.'), { status: 413 });
    chunks.push(chunk);
  }
  try { return JSON.parse(Buffer.concat(chunks).toString('utf8')); }
  catch { throw Object.assign(new Error('Expected a JSON request body.'), { status: 400 }); }
}

export async function startDashboard(monitor, { port = 8099, host = '0.0.0.0', publicUrl, lab } = {}) {
  const assets = new Map(await Promise.all([
    ['/', 'index.html', 'text/html'], ['/app.js', 'app.js', 'text/javascript'], ['/style.css', 'style.css', 'text/css'],
  ].map(async ([route, file, type]) => [route, { body: await readFile(new URL(`./fraud-simulation-ui/${file}`, import.meta.url)), type }])));
  const token = randomBytes(32).toString('hex');
  const expected = Buffer.from(token);
  const cookieName = `fraud_dashboard_${token.slice(0, 10)}`;
  const clients = new Set();
  const snapshot = () => ({ ...monitor.snapshot(), lab: lab?.snapshot() ?? null });
  const authorized = req => matches(req.headers.authorization?.replace(/^Bearer /, ''), expected) ||
    (req.headers.cookie || '').split(';').some(cookie => {
      const [name, value] = cookie.trim().split('=');
      return name === cookieName && matches(value, expected);
    });
  const json = (res, status, body) => res.writeHead(status, { 'Content-Type': 'application/json' }).end(JSON.stringify(body));
  const handle = async (req, res) => {
    res.setHeader('Cache-Control', 'no-store');
    res.setHeader('X-Content-Type-Options', 'nosniff');
    res.setHeader('Referrer-Policy', 'no-referrer');
    res.setHeader('Content-Security-Policy', "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'");
    const authority = req.headers.host || '';
    if (!/^[a-z\d.:[\]-]+$/i.test(authority)) {
      json(res, 403, { error: 'Use the dashboard’s own address.' });
      return;
    }
    const url = new URL(req.url, `http://${authority}`);
    if (url.pathname.startsWith('/api/') && (req.headers['sec-fetch-site'] === 'cross-site' ||
        (req.headers.origin && ![`http://${authority}`, `https://${authority}`].includes(req.headers.origin)))) {
      json(res, 403, { error: 'Use the dashboard’s own address.' });
      return;
    }
    if (req.method === 'POST' && url.pathname === '/api/session') {
      const body = await jsonBody(req);
      if (!matches(body?.token, expected)) { json(res, 401, { error: 'Open the complete dashboard link printed by the script.' }); return; }
      res.setHeader('Set-Cookie', `${cookieName}=${token}; HttpOnly; SameSite=Strict; Path=/`);
      json(res, 200, { connected: true });
      return;
    }
    if (!url.pathname.startsWith('/api/')) {
      if (req.method !== 'GET') { res.writeHead(405, { Allow: 'GET' }).end(); return; }
      const asset = assets.get(url.pathname);
      if (asset) res.writeHead(200, { 'Content-Type': `${asset.type}; charset=utf-8` }).end(asset.body);
      else res.writeHead(404).end('Not found');
      return;
    }
    if (!authorized(req)) { json(res, 401, { error: 'Open the complete dashboard link printed by the script.' }); return; }
    if (req.method === 'GET' && url.pathname === '/api/snapshot') json(res, 200, snapshot());
    else if (req.method === 'GET' && url.pathname === '/api/events') {
      if (clients.size >= 8) { json(res, 503, { error: 'Too many dashboard viewers' }); return; }
      res.writeHead(200, { 'Content-Type': 'text/event-stream', Connection: 'keep-alive', 'X-Accel-Buffering': 'no' });
      clients.add(res);
      res.write(`data: ${JSON.stringify(snapshot())}\n\n`);
      res.on('close', () => clients.delete(res));
    } else if (req.method === 'GET' && url.pathname.startsWith('/api/transactions/')) {
      const id = decodeURIComponent(url.pathname.slice('/api/transactions/'.length));
      const detail = monitor.detail(id);
      json(res, detail ? 200 : 404, detail || { error: 'Details are no longer retained for this request.' });
    } else if (lab && url.pathname === '/api/lab' && req.method === 'GET') json(res, 200, lab.snapshot(true));
    else if (lab && url.pathname === '/api/lab/generate' && req.method === 'POST') json(res, 200, lab.create(await jsonBody(req)));
    else if (lab && url.pathname === '/api/lab/submit' && req.method === 'POST') json(res, 202, lab.submit(await jsonBody(req)));
    else if (url.pathname === '/api/lab/generate' || url.pathname === '/api/lab/submit') json(res, 409, { error: 'Start the script with --deterministic to use the case builder.' });
    else if (req.method !== 'GET') res.writeHead(405, { Allow: 'GET' }).end();
    else json(res, 404, { error: 'Not found' });
  };
  const server = createServer((req, res) => {
    handle(req, res).catch(error => {
      if (!res.headersSent) json(res, error.status || 500, { error: error.status ? error.message : 'Dashboard request failed.' });
      else res.destroy();
      if (!error.status) monitor.note(`Dashboard request failed: ${error.message}`, 'error');
    });
  });
  server.requestTimeout = 15000;
  server.headersTimeout = 10000;
  const ticker = setInterval(() => {
    if (!clients.size) return;
    const frame = `data: ${JSON.stringify(snapshot())}\n\n`;
    for (const client of clients) {
      if (client.writableLength > 256 * 1024) client.destroy();
      else client.write(frame);
    }
  }, 500);
  ticker.unref();
  try {
    await new Promise((resolve, reject) => { server.once('error', reject); server.listen(port, host, resolve); });
  } catch (error) { clearInterval(ticker); throw error; }
  const address = server.address();
  const localHost = host === '0.0.0.0' || host === '::' ? '127.0.0.1' : host.includes(':') ? `[${host}]` : host;
  const origin = publicUrl || `http://${localHost}:${address.port}`;
  let closing;
  return {
    origin, token, url: `${origin}/#${token}`, host, port: address.port,
    close() {
      closing ??= (async () => {
        clearInterval(ticker);
        await lab?.stop();
        for (const client of clients) client.end();
        await new Promise((resolve, reject) => {
          server.close(error => error ? reject(error) : resolve());
          server.closeIdleConnections();
        });
      })();
      return closing;
    },
  };
}
