// Browser contract fixture only. PostgreSQL integration tests separately verify
// current role checks, immutable receipts, publication and tenant isolation.
import { randomUUID } from "node:crypto";
let lists = new Map(), imports = new Map(), operations = new Map();
export async function handlePrivateLists(req, url, send) {
  if (url.pathname === "/reset") { lists.clear(); imports.clear(); operations.clear(); return false; }
  if (!url.pathname.startsWith("/v1/private-lists")) return false;
  const admin = req.headers.authorization === "Bearer fixture-private-reviewer";
  if (!admin && req.headers.authorization !== "Bearer fixture-private-importer") { send(403, { error: "Current membership required." }); return true; }
  const [, , , list, group, id, action] = url.pathname.split("/");
  const reply = (status, body) => { send(status, body); return true; };
  if (list === "capabilities") return reply(200, { configured: true, can_import: true, can_approve: true, actor_id: admin ? "reviewer" : "analyst" });
  let body;
  if (req.method === "POST") { let raw = ""; for await (const chunk of req) raw += chunk; body = JSON.parse(raw); }
  if (!list) {
    if (req.method === "GET") return reply(200, { lists: [...lists.values()], has_more: false });
    if (operations.has(body.operation_id)) return reply(200, { id: operations.get(body.operation_id) });
    const id = `private-${randomUUID().replaceAll("-", "")}`; lists.set(id, { id, ...body, archived: false, record_count: 0 }); operations.set(body.operation_id, id); imports.set(id, []); return reply(200, { id });
  }
  const current = lists.get(list); if (!current) return reply(404, { error: "Private list not found." });
  if (!group) return reply(200, { list: current, imports: imports.get(list), has_more: false });
  if (group === "recover") return reply(200, { recovered: 0 });
  if (!id) {
    if (operations.has(body.operation_id)) return reply(200, imports.get(list).find(item => item.id === operations.get(body.operation_id)));
    const item = { id: randomUUID(), generation: randomUUID(), action: body.action, status: "pending_review", created_by: admin ? "reviewer" : "analyst", created_by_name: "Fixture uploader", record_count: body.action === "snapshot" || body.action === "restore" ? 1 : 0, created_at: new Date().toISOString(), content_hash: "fixture-hash" };
    imports.get(list).unshift(item); operations.set(body.operation_id, item.id); return reply(200, item);
  }
  const item = imports.get(list).find(item => item.id === id); if (!item) return reply(404, { error: "Import not found." });
  if (action === "diff") return reply(200, { changes: [{ record_id: "001", kind: "added" }], has_more: false });
  if (action === "approve" || action === "reject") {
    if (!admin || item.created_by === "reviewer") return reply(403, { error: "A different authorized reviewer is required." });
    item.status = action === "approve" ? "published" : "rejected";
    if (action === "approve") { item.published_generation = item.generation; current.record_count = item.record_count; current.generation = item.generation; current.archived = item.action === "archive"; }
    return reply(200, item);
  }
  return reply(200, item);
}
