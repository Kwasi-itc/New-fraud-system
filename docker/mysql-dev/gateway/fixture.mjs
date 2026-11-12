import {createServer} from 'node:http';
import {gzipSync} from 'node:zlib';

for (const port of [8080, 8081, 8082, 8085, 8086, 8087]) {
  createServer(async (req, res) => {
    const path = req.url.split('?')[0];
    if (path === '/redirect') {
      res.writeHead(307, {Location: '/next?from=redirect', 'Set-Cookie': 'fixture=value; Path=/; HttpOnly'});
      return res.end();
    }
    if (path === '/identity/redirect') {
      res.writeHead(303, {Location: '/identity/next?from=redirect', 'Set-Cookie': 'identity=value; Path=/identity/realms/fraud/; HttpOnly'});
      return res.end();
    }
    if (path === '/unavailable') return req.socket.destroy();
    if (path === '/healthz') return res.end('ok');
    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    const body = JSON.stringify({port, method: req.method, url: req.url, headers: req.headers, body: Buffer.concat(chunks).toString('utf8')});
    res.writeHead(201, {'Content-Type': 'application/json', 'Content-Encoding': 'gzip', 'X-Fixture': 'upstream'});
    res.end(gzipSync(body));
  }).listen(port, '0.0.0.0');
}
