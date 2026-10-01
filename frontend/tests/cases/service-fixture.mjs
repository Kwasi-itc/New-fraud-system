// Browser contract fixture, not an alternative authentication implementation.
import { createServer } from "node:http";
import { randomUUID } from "node:crypto";
const tenant = "11111111-1111-4111-8111-111111111111";
const inbox = "22222222-2222-4222-8222-222222222222";
const id = "33333333-3333-4333-8333-333333333333";
const decision = "44444444-4444-4444-8444-444444444444";
let item, events;
let uploads, files, tags, operations, reports;
function reset() { reports = new Map(); uploads = new Map(); files = []; tags = []; operations = new Set(); item = { id, tenant_id: tenant, inbox_id: inbox, name: "Payment investigation", status: "investigating", outcome: "unset", type: "decision", assigned_to: "analyst", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", tags: [], contributors: [{ user_id: "analyst" }] }; events = []; }
reset();
function record(event_type, note) { events.unshift({ id: randomUUID(), event_type, additional_note: note, user_id: "analyst", created_at: new Date().toISOString(), new_value: "", previous_value: "" }); }
createServer(async (req, res) => {
  const url = new URL(req.url, "http://localhost");
  const send = (status, data) => { res.writeHead(status, { "Content-Type": "application/json" }); res.end(status === 204 ? undefined : JSON.stringify(data)); };
  if (url.pathname === "/health") return send(200, {});
  if (url.pathname === "/reset") { reset(); return send(204); }
  const token = req.headers.authorization;
  if (url.pathname === `/v1/tenants/${tenant}/decisions/${decision}`) { if (token !== "Bearer fixture-owner-token") return send(401, { error: "Missing owner credential" }); return send(200, { decision: { id: decision, outcome: "review", score: 35 }, rule_executions: [{ name: "Velocity", result: true, score_modifier: 35 }] }); }
  if (!["Bearer fixture-analyst", "Bearer fixture-admin"].includes(token)) return send(401, { error: "Authentication required" });
  if (!url.pathname.startsWith(`/v1/tenants/${tenant}/`)) return send(403, { error: "You cannot access this tenant." });
  const path = url.pathname.slice(`/v1/tenants/${tenant}/`.length);
  if (path === "case-session") return send(200, { subject: "analyst", tenant_id: tenant, admin: token === "Bearer fixture-admin" });
  if (path === "inboxes") return send(200, { inboxes: [{ id: inbox, name: "Fraud review", status: "active" }] });
  if (path === "tags" && req.method === "GET") return send(200, { tags: tags.filter(t => !t.deleted_at) });
  if (path === "case-analytics") {
    const from = url.searchParams.get("from"); const to = url.searchParams.get("to");
    const filterInbox = url.searchParams.get("inbox_id");
    if (filterInbox && filterInbox !== inbox) return send(403, { error: "You cannot access this inbox." });
    if (!from || !to || !(Date.parse(to) > Date.parse(from))) return send(400, { error: "Invalid analytics date interval" });
    const included = item.created_at >= from && item.created_at < to;
    const counts = { total: included ? 1 : 0, pending: included && item.status === "pending" ? 1 : 0, investigating: included && item.status === "investigating" ? 1 : 0, closed: included && item.status === "closed" ? 1 : 0,
      false_positive: included && item.outcome === "false_positive" ? 1 : 0, valuable_alert: included && item.outcome === "valuable_alert" ? 1 : 0, confirmed_risk: included && item.outcome === "confirmed_risk" ? 1 : 0, unset: included && item.outcome === "unset" ? 1 : 0,
      snoozed: 0, sla_configured: 0, overdue: 0, escalations: 0, snooze_events: 0, measured_closures: 0, average_close_seconds: null };
    return send(200, { as_of: new Date().toISOString(), from, to, totals: counts,
      inboxes: included ? [{ inbox_id: inbox, inbox_name: "Fraud review", ...counts }] : [],
      daily: included ? [{ date: "2026-01-01", total: 1, pending: counts.pending, investigating: counts.investigating, closed: counts.closed }] : [],
      assignments: included && item.status !== "closed" ? [{ inbox_id: inbox, assignee: item.assigned_to ?? null, open: 1 }] : [] });
  }
  if (path === "case-queue") {
    const hidden = url.searchParams.has("related_to") || url.searchParams.has("cursor") || (url.searchParams.get("name") && !item.name.includes(url.searchParams.get("name"))) || (url.searchParams.getAll("status").length && !url.searchParams.getAll("status").includes(item.status));
    return send(200, { cases: hidden ? [] : [item], counts: hidden ? {} : { [item.status]: 1 } });
  }
  if (path.startsWith("cases/") && path.split("/")[1] !== id) return send(403, { error: "You cannot access this inbox." });
  if (path === `cases/${id}/overview`) return send(200, { case: item });
  if (path === `cases/${id}/evidence-access/decisions/${decision}`) return send(204);
  if (path.includes("/evidence-access/")) return send(404, { error: "Evidence is not linked to this case." });
  if (path.startsWith(`cases/${id}/links/`)) {
    const kind = path.split("/").at(-1);
    return send(200, { items: kind === "events" ? events : kind === "files" ? files : kind === "decisions" ? [{ id: decision, decision_id: decision, object_type: "customer", object_id: "customer-42" }] : [] });
  }
  if (path.startsWith(`cases/${id}/files/`) && path.endsWith("/download")) {
    const file = uploads.get(path.split("/")[3]);
    if (!file?.finalized) return send(404, { error: "Not attached" });
    res.writeHead(200, { "Content-Type": file.content_type, "Content-Disposition": `attachment; filename="${file.file_name}"` }); return res.end(file.bytes);
  }
  if (path.startsWith(`cases/${id}/uploads/`) && path.endsWith("/content")) {
    const upload = uploads.get(path.split("/")[3]); if (!upload) return send(404, { error: "Missing upload" });
    const chunks = []; for await (const chunk of req) chunks.push(chunk); upload.bytes = Buffer.concat(chunks); return send(204);
  }
  let body = ""; for await (const chunk of req) body += chunk; const data = body ? JSON.parse(body) : {};
  if (path === `cases/${id}/reports`) {
    if (req.method === "GET") return send(200, { reports: [...reports.values()] });
    const report = { id: data.id, status: "draft", version: 1, created_by: "analyst", payload: { format: "internal_sar_v1", content: data.content } };
    reports.set(report.id, report); record("report_created", "Draft created"); return send(201, { report });
  }
  if (path.startsWith(`cases/${id}/reports/`)) {
    const report = reports.get(path.split("/")[3]); if (!report) return send(404, { error: "Report not found" });
    if (path.endsWith("/export")) { res.writeHead(200, { "Content-Type": "application/json", "Content-Disposition": `attachment; filename="report-${report.id}.json"` }); return res.end(JSON.stringify(report)); }
    if (req.method === "GET") return send(200, { report });
    if (report.status !== "draft" || data.version !== report.version) return send(409, { error: "Report version conflict. Reload before editing." });
    if (path.endsWith("/complete")) {
      const c = report.payload.content;
      if (!c.subject || !c.narrative || !c.activity_from || !c.activity_to) return send(400, { error: "Completion requires subject, narrative, and activity start/end" });
      report.status = "completed"; report.completed_by = "analyst"; report.completed_at = new Date().toISOString();
    } else { report.payload.content = data.content; }
    report.version++; record("report_updated", report.status); return send(200, { report });
  }
  if (path === `cases/${id}/uploads`) {
    const upload = { ...data, id: randomUUID(), created_at: new Date().toISOString(), uploaded_by: "analyst" }; uploads.set(upload.id, upload); return send(201, { upload });
  }
  if (path.startsWith(`cases/${id}/uploads/`) && path.endsWith("/finalize")) {
    const upload = uploads.get(path.split("/")[3]); if (!upload?.bytes) return send(409, { error: "Upload incomplete" });
    if (!upload.finalized) { upload.finalized = true; files.push({ ...upload, storage_key: `case-db/${upload.id}`, bytes: undefined }); record("evidence_attached", upload.file_name); }
    return send(200, { file: upload });
  }
  if (path === "case-bulk") {
    const replayed = operations.has(data.operation_id); operations.add(data.operation_id);
    if (!replayed) { if (data.action === "close") item.status = "closed"; record(`bulk_${data.action}`, data.comment); }
    return send(200, { results: data.case_ids.map(case_id => ({ case_id, success: case_id === id, replayed, error: case_id === id ? undefined : "forbidden" })) });
  }
  if (path === "tags" && req.method === "POST") { const tag = { ...data, id: randomUUID() }; tags.push(tag); return send(201, { tag }); }
  if (path.startsWith("tags/")) {
    const tag = tags.find(t => t.id === path.split("/")[1]); if (!tag) return send(404, { error: "Not found" });
    if (req.method === "DELETE") { tag.deleted_at = new Date().toISOString(); return send(204); }
    Object.assign(tag, data); return send(200, { tag });
  }
  if (path === `cases/${id}/close`) { if (!data.comment || !data.outcome) return send(400, { error: "Closing findings and outcome required" }); item.status = "closed"; item.outcome = data.outcome; record("status_updated", data.comment); return send(200, { case: item }); }
  if (path === `cases/${id}/comments`) { if (data.comment === "fail write") return send(503, { error: "Comment could not be saved. Please retry." }); record("comment_created", data.comment); return send(201, { comment: events[0] }); }
  if (path === `cases/${id}` && req.method === "PATCH") { Object.assign(item, data); record("status_updated", "Case updated"); return send(200, { case: item }); }
  send(404, { error: "Fixture endpoint not implemented" });
}).listen(3199, "127.0.0.1");
