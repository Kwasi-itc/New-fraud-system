// Test-only identity fixture; screening requests go to the actual disposable
// PostgreSQL API + worker. This module is never imported by application code.
import { readFileSync } from "node:fs";

export async function handleControlledProvider(req, url, send) {
  const file = process.env.CONTROLLED_BROWSER_STATE_FILE;
  if (!file) return false;
  const state = JSON.parse(readFileSync(file, "utf8"));
  if (url.pathname === "/internal/session") {
    if (req.headers.authorization !== "Bearer fixture-private-auth-proxy-token-32") { send(401, {}); return true; }
    let raw = ""; for await (const chunk of req) raw += chunk;
    if (JSON.parse(raw).session !== "fixture-analyst") { send(401, {}); return true; }
    send(200, { access_token: state.token, session: { id: "fixture-analyst", user_id: state.actor, tenant_id: state.tenant, name: "Controlled data investigator", roles: ["case_investigator"], version: 1, expires_at: "2099-01-01T00:00:00Z" } });
    return true;
  }
  if (!url.pathname.startsWith("/v1/manual-screenings") && !(url.pathname.startsWith("/v1/investigator/") && url.pathname.endsWith("/enrich"))) return false;
  // Preserve the genuine test JWT from the BFF; do not invent tenant headers.
  let body; if (req.method === "POST") { const chunks = []; for await (const chunk of req) chunks.push(chunk); body = Buffer.concat(chunks); }
  try {
    const response = await fetch(`${state.backend}${url.pathname}${url.search}`, { method: req.method, body, headers: { Authorization: req.headers.authorization ?? "", "Content-Type": "application/json" }, redirect: "error", signal: AbortSignal.timeout(15000) });
    send(response.status, await response.json());
  } catch { send(503, { error: "Isolated screening backend unavailable." }); }
  return true;
}
