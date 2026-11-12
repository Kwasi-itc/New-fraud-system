// Local authenticated runtime only. Keep callback and CSRF origins identical.
export function localOrigin(value) {
  const url = new URL(value);
  if (url.protocol !== 'http:' || url.hostname !== 'localhost' || !url.port ||
      Number(url.port) < 1024 || url.username || url.password || url.pathname !== '/' || url.search || url.hash) {
    throw Error('Expected a local application origin such as http://localhost:8088');
  }
  return url.origin;
}
export function frontendOrigin() {
  return localOrigin(process.env.FRAUD_APP_ORIGIN || 'http://localhost:3002');
}
