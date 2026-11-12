// Canonical local provider URLs; legacy URLs are accepted only for migration.
export function issuerURLs(value) {
  const url = new URL(value);
  if (url.protocol !== 'http:' || url.hostname !== 'localhost' || !url.port || Number(url.port) < 1024 || url.username || url.password || url.search || url.hash || !['/realms/fraud', '/identity/realms/fraud'].includes(url.pathname)) throw Error('Expected a local fraud issuer URL');
  const base = url.origin + (url.pathname.startsWith('/identity/') ? '/identity' : '');
  return {issuer: url.href, base, masterToken: base + '/realms/master/protocol/openid-connect/token', clients: base + '/admin/realms/fraud/clients', users: base + '/admin/realms/fraud/users'};
}
export function gatewayIssuer() {
  const origin = process.env.FRAUD_APP_ORIGIN || 'http://localhost:' + (process.env.MYSQL_GATEWAY_PORT || 8088);
  const url = new URL(origin);
  if (url.pathname !== '/' || url.search || url.hash || url.username || url.password) throw Error('Expected a local application origin');
  return issuerURLs(url.origin + '/identity/realms/fraud');
}
