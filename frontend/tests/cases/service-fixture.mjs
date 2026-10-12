// Browser contract fixture, not an alternative authentication implementation.
import { createServer } from "node:http";
import { randomUUID } from "node:crypto";
const tenant = "11111111-1111-4111-8111-111111111111";
const inbox = "22222222-2222-4222-8222-222222222222";
const id = "33333333-3333-4333-8333-333333333333";
const decision = "44444444-4444-4444-8444-444444444444";
const screening = "66666666-6666-4666-8666-666666666666";
let screeningLinks = [];
const transferTarget = "77777777-7777-4777-8777-777777777777";
let transfersEnabled = false, decisionTransferred = false, transferFailAck = false;
let decisionUnlinked = false, decisionAttachDenied = false, decisionCreateLostAck = false;
let item, events;
let reviews = new Map(), reviewReceipts = new Map(), reviewLostAck = false, reviewDenied = false;
let uploads, files, tags, operations, reports;
let revokedSessions = new Set();
let invitations = [];
let workflowRules = new Map(), workflowLostAck = false, workflowDenied = false;
let explorerAttached = false, explorerLostAck = false, explorerDenied = false;
const discoveredDecision = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
let revokedSessionIDs = new Set(), failSessionRevoke = false;
const currentSessionID = "11111111-1111-4111-8111-111111111111", otherSessionID = "22222222-2222-4222-8222-222222222222";
function reset() { reports = new Map(); uploads = new Map(); files = []; tags = []; operations = new Set(); item = { id, tenant_id: tenant, inbox_id: inbox, name: "Payment investigation", status: "investigating", outcome: "unset", type: "decision", assigned_to: "analyst", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", tags: [], contributors: [{ user_id: "analyst" }] }; events = []; }
reset();
function record(event_type, note) { events.unshift({ id: randomUUID(), event_type, additional_note: note, user_id: "analyst", created_at: new Date().toISOString(), new_value: "", previous_value: "" }); }
createServer(async (req, res) => {
  const url = new URL(req.url, "http://localhost");
  const send = (status, data) => { res.writeHead(status, { "Content-Type": "application/json" }); res.end(status === 204 ? undefined : JSON.stringify(data)); };
  if (url.pathname === "/health") return send(200, {});
  if (url.pathname === "/reset") { workflowRules = new Map(); workflowLostAck = false; workflowDenied = false; }
  if (url.pathname === "/fixture/workflow-lost-ack") { workflowLostAck = true; return send(204); }
  if (url.pathname === "/fixture/workflow-denied") { workflowDenied = true; return send(204); }
  if (url.pathname.startsWith("/internal/workflow-authoring/")) {
    if (req.headers.authorization !== "Bearer fixture-workflow-authoring-private-token-32" || req.headers["x-workflow-user"] !== "analyst" || req.headers["x-workflow-version"] !== "1") return send(401, {});
    if (workflowDenied) return send(403, { error: "Workflow access denied." });
    if (!url.pathname.startsWith(`/internal/workflow-authoring/tenants/${tenant}/scenarios/`)) return send(403, {});
    if (req.method === "GET") return send(200, { scenario_name: "Payment checks", rules: [...workflowRules.values()], other_rules: 0, legacy_workflows: false });
    let raw = ""; for await (const chunk of req) raw += chunk;
    const body = JSON.parse(raw);
    if (body.inbox_id !== inbox || body.action_type !== "create_case" || !body.outcomes.includes("review")) return send(400, { error: "Incorrect workflow contract" });
    const old = workflowRules.get(body.id);
    if (old && old.updated_at !== body.updated_at) return send(409, { error: "Workflow changed; refresh before saving." });
    const saved = { ...body, updated_at: new Date().toISOString() }; workflowRules.set(body.id, saved);
    if (workflowLostAck) { workflowLostAck = false; return send(503, { error: "Response lost" }); }
    return send(200, saved);
  }
  if (url.pathname === "/reset") { explorerAttached = false; explorerLostAck = false; explorerDenied = false; invitations = []; revokedSessions = new Set(); revokedSessionIDs = new Set(); failSessionRevoke = false; }
  if (url.pathname === "/fixture/explorer-lost-ack") { explorerLostAck = true; return send(204); }
  if (url.pathname === "/fixture/explorer-denied") { explorerDenied = true; return send(204); }
  if (url.pathname === "/fixture/session-error") { failSessionRevoke = true; return send(204); }
  if (url.pathname.startsWith("/internal/")) {
    if (req.headers.authorization !== "Bearer fixture-private-auth-proxy-token-32") return send(401, {});
    let raw = ""; for await (const chunk of req) raw += chunk;
    const body = JSON.parse(raw || "{}");
    if (url.pathname === "/internal/invitations/start") return body.token === "a".repeat(43) ? send(200, { url: "http://127.0.0.1:3199/provider-login", binding: "fixture-login-binding" }) : send(403, {});
    if (!["fixture-analyst", "fixture-admin", "fixture-platform", "fixture-workflow"].includes(body.session) || revokedSessions.has(body.session)) return send(401, { error: "Session expired" });
    if (url.pathname === "/internal/session" && body.session === "fixture-workflow") return send(200, { access_token: "fixture-admin", session: { id: body.session, user_id: "analyst", tenant_id: tenant, name: "Workflow administrator", roles: ["platform_admin", "case_manager_admin"], version: 1, expires_at: "2099-01-01T00:00:00Z" } });
    if (url.pathname === "/internal/logout") { revokedSessions.add(body.session); return send(204); }
    if (url.pathname === "/internal/session") return send(200, { access_token: body.session, session: { id: body.session, user_id: "analyst", tenant_id: tenant, name: "Test investigator", roles: body.session === "fixture-admin" ? ["tenant_admin", "case_manager_admin"] : ["case_investigator"], version: 1, expires_at: "2099-01-01T00:00:00Z" } });
    if (url.pathname === "/internal/sessions/list") return send(200, { items: [currentSessionID, otherSessionID].filter(id => !revokedSessionIDs.has(id)).map(id => ({ id, current: id === currentSessionID, created_at: "2026-10-06T12:00:00Z", expires_at: "2099-01-01T00:00:00Z", idle_until: "2099-01-01T00:00:00Z" })), next_after: "" });
    if (url.pathname === "/internal/sessions/revoke") {
      if (failSessionRevoke) return send(503, { error: "Unavailable" });
      if (!body.all && ![currentSessionID, otherSessionID].includes(body.target)) return send(403, {});
      if (body.target) revokedSessionIDs.add(body.target);
      const signed_out = !!body.all || body.target === currentSessionID;
      if (signed_out) revokedSessions.add(body.session);
      return send(200, { signed_out });
    }
    if (body.session !== "fixture-admin") return send(403, { error: "Administrator required" });
    if (url.pathname === "/internal/invitations/list") return send(200, { items: invitations, next_after: "" });
    if (url.pathname === "/internal/audit/list") return send(200, { items: [{ id: "audit-1", actor: "admin", action: "invitation.created", target: "invite-1", at: "2026-10-09", details: { roles: ["case_investigator"] } }], next_after: "" });
    if (url.pathname === "/internal/invitations/change") {
      if (body.action === "create") invitations.push({ id: body.id, email: body.email, roles: body.roles, status: "pending", delivery: "queued", version: 1, expires_at: "2099-01-01T00:00:00Z" });
      else { const item = invitations.find(v => v.id === body.id); if (!item || item.version !== body.version) return send(409, {}); item.version++; item.status = body.action === "cancel" ? "cancelled" : "pending"; }
      return send(204);
    }
    if (url.pathname === "/internal/members/list") return send(200, { items: [{ user_id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", name: "Test investigator", roles: ["case_investigator"], active: true, version: 1 }], next_after: "" });
    if (url.pathname === "/internal/members/change") return send(409, { error: "Account changed; refresh before retrying." });
    return send(404, {});
  }
  if (url.pathname === "/reset") { decisionUnlinked = false; decisionAttachDenied = false; decisionCreateLostAck = false; }
  if (url.pathname === "/fixture/decision-intake") { decisionUnlinked = true; decisionAttachDenied = url.searchParams.has("denied"); decisionCreateLostAck = url.searchParams.has("lost_ack"); return send(204); }
  if (url.pathname === "/reset") { reset(); reviews = new Map(); reviewReceipts = new Map(); reviewLostAck = false; reviewDenied = false; screeningLinks = []; transfersEnabled = false; decisionTransferred = false; transferFailAck = false; return send(204); }
  if (url.pathname === "/fixture/review") { reviewLostAck = url.searchParams.has("lost_ack"); reviewDenied = url.searchParams.has("denied"); return send(204); }
  if (url.pathname === "/fixture/transfers") { transfersEnabled = true; transferFailAck = url.searchParams.has("lost_ack"); return send(204); }
  if (url.pathname === "/fixture/screening") {
    item = { ...item, name: "Screening review", type: url.searchParams.has("decision") ? "decision" : "continuous_screening", status: "pending", assigned_to: null, contributors: [] };
    screeningLinks = [{ id: screening, screening_id: screening, match_id: url.searchParams.has("match") ? "pending-match-1" : null, status: url.searchParams.has("match") ? "pending" : "awaiting_review", partial: !url.searchParams.has("match") }];
    return send(204);
  }
  const token = req.headers.authorization;
  if (url.pathname.endsWith("/tables") && url.pathname.startsWith("/v1/tenants/")) {
    if (token !== "Bearer fixture-platform") return send(403, { error: { message: "Data Model permission required" } });
    if (url.pathname !== `/v1/tenants/${tenant}/tables`) return send(403, { error: { message: "Wrong tenant" } });
    return send(200, { tables: [] });
  }
  if (url.pathname === `/v1/investigator/tenants/${tenant}/cases/${id}/review`) {
    if (token !== "Bearer fixture-analyst") return send(401, { error: "Original human credential required" });
    if (reviewDenied) return send(403, { error: "Current case/resource access is required." });
    let data; if (req.method === "POST") { let body = ""; for await (const chunk of req) body += chunk; data = JSON.parse(body); }
    const resource = data?.resource_id ?? url.searchParams.get("resource_id");
    const state = reviews.get(resource) ?? { status: "pending", version: "0", eligible: item.status !== "closed", actions: resource === decision ? ["approve", "decline"] : ["confirmed_hit", "no_hit"], ...(resource === decision ? { machine_outcome: "review" } : {}) };
    if (!data) return send(200, state);
    const signature = JSON.stringify(data), receipt = reviewReceipts.get(data.operation_id);
    if (receipt) return receipt.signature === signature ? send(200, receipt.state) : send(409, { error: "Command conflicts." });
    if (!state.eligible || state.version !== data.expected_version) return send(409, { error: "Review changed; refresh before retrying." });
    if (!state.actions.includes(data.action) || !data.findings?.trim()) return send(400, { error: "Findings and a valid action are required." });
    const result = { ...state, status: data.action, version: "1", eligible: false }; reviews.set(resource, result); reviewReceipts.set(data.operation_id, { signature, state: result });
    record(resource === decision ? "decision_reviewed" : "screening_match_reviewed", data.findings);
    screeningLinks = screeningLinks.map(link => link.match_id === resource ? { ...link, status: data.action } : link);
    if (reviewLostAck) { reviewLostAck = false; return send(503, { error: "Review response unavailable. Retry the same operation." }); }
    return send(200, result);
  }
  if (url.pathname === `/v1/tenants/${tenant}/screenings/${screening}`) {
    if (token !== "Bearer fixture-owner-token") return send(401, { error: "Missing owner credential" });
    return send(200, { screening: { id: screening, partial: screeningLinks[0]?.partial, status: "awaiting_review", provider: "Test screening provider" }, matches: screeningLinks[0]?.match_id ? [{ id: "pending-match-1", status: "pending" }] : [] });
  }
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
    if (url.searchParams.has("decision_id")) {
      const cases = !decisionUnlinked && url.searchParams.get("decision_id") === decision ? [item] : [];
      return send(200, { cases, counts: cases.length ? { [item.status]: 1 } : {} });
    }
    if (transfersEnabled) {
      const candidates = [item, { ...item, id: transferTarget, name: "Correct investigation", status: "pending", assigned_to: null }];
      const cases = candidates.filter(c => !url.searchParams.has("related_to") && !url.searchParams.has("cursor") && (!url.searchParams.get("name") || c.name.includes(url.searchParams.get("name"))) && (!url.searchParams.getAll("status").length || url.searchParams.getAll("status").includes(c.status)));
      return send(200, { cases, counts: { pending: cases.filter(c => c.status === "pending").length, investigating: cases.filter(c => c.status === "investigating").length } });
    }
    const hidden = url.searchParams.has("related_to") || url.searchParams.has("cursor") || (url.searchParams.get("name") && !item.name.includes(url.searchParams.get("name"))) || (url.searchParams.getAll("status").length && !url.searchParams.getAll("status").includes(item.status));
    return send(200, { cases: hidden ? [] : [item], counts: hidden ? {} : { [item.status]: 1 } });
  }
  if (path.startsWith("cases/") && path.split("/")[1] !== id) return send(403, { error: "You cannot access this inbox." });
  if (path === `cases/${id}/overview`) return send(200, { case: item });
  if (path === `cases/${id}/evidence-access/decisions/${decision}`) return send(204);
  if (path === `cases/${id}/evidence-access/screenings/${screening}` && screeningLinks.length) return send(204);
  if (path.includes("/evidence-access/")) return send(404, { error: "Evidence is not linked to this case." });
  if (path.startsWith(`cases/${id}/links/`)) {
    const kind = path.split("/").at(-1);
    return send(200, { items: kind === "events" ? events : kind === "files" ? files : kind === "screenings" ? screeningLinks : kind === "decisions" && (!screeningLinks.length || item.type === "decision") && !decisionTransferred ? [{ id: decision, decision_id: decision, object_type: "customer", object_id: "customer-42" }] : [] });
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
  if (path === `cases/${id}/explore`) {
    if (explorerDenied) return send(403, { error: "Investigation access was revoked." });
    if (data.decision_id !== decision) return send(404, { error: "Decision is not linked" });
    if (data.path?.some(h => h.object_id !== "payment-1")) return send(403, { error: "Connection unavailable" });
    const child = !!data.path?.length;
    const decisions = child ? (data.decision_after ? [{ id: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", scenario_name: "Older velocity check", outcome: "approve", score: 5, triggered: false, created_at: "2026-01-01T00:00:00Z", can_attach: false }] : [{ id: discoveredDecision, scenario_name: "Velocity check", outcome: "review", score: 80, triggered: true, created_at: "2026-10-01T00:00:00Z", can_attach: !explorerAttached, ...(explorerAttached ? { case: { id, name: item.name, status: item.status } } : {}) }, { id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd", scenario_name: "Linked check", outcome: "decline", score: 90, triggered: true, created_at: "2026-09-01T00:00:00Z", can_attach: false, case: { id: transferTarget, name: "Related accessible case", status: "closed" } }]) : [];
    return send(200, { table: child ? "payment" : "customer", current: { object_id: child ? "payment-1" : "customer-42", data: child ? { object_id: "payment-1", amount: 250 } : { object_id: "customer-42", name: "Test customer" } }, connections: child ? [] : [{ id: "reverse:fixture-link", label: "Payments", target: "payment", reason: "customer.object_id = payment.customer_id" }], records: data.connection ? [{ object_id: "payment-1" }] : [], next_after: "", decisions, next_decision_after: child && !data.decision_after ? "fixture-next" : "" });
  }
  if (path === `cases/${id}/explore/attach`) {
    if (explorerDenied) return send(403, { error: "Investigation access was revoked." });
    if (data.decision_id !== decision || data.target_decision_id !== discoveredDecision || data.path?.[0]?.object_id !== "payment-1") return send(403, {});
    if (!explorerAttached) { explorerAttached = true; record("decision_added", discoveredDecision); }
    if (explorerLostAck) { explorerLostAck = false; return send(503, { error: "Attachment response unavailable. Refresh to check membership." }); }
    return send(200, { decision_id: discoveredDecision, case_id: id });
  }
  if (path === "cases" && req.method === "POST") {
    if (!decisionUnlinked) return send(409, { error: "Decision already belongs to a case." });
    if (data.type !== "decision" || data.inbox_id !== inbox || data.decision_ids?.length !== 1 || data.decision_ids[0] !== decision || !data.name?.trim()) return send(400, { error: "Invalid decision investigation." });
    if (decisionAttachDenied) return send(403, { error: "Inbox access was revoked." });
    decisionUnlinked = false; item.name = data.name; record("decision_added", decision);
    if (decisionCreateLostAck) { decisionCreateLostAck = false; return send(503, { error: "Creation response unavailable. Check the current case." }); }
    return send(201, { case: item });
  }
  if (path === `cases/${id}/decisions` && req.method === "POST") {
    if (decisionAttachDenied) return send(403, { error: "Inbox access was revoked." });
    if (data.decision_id !== decision) return send(400, { error: "Invalid decision." });
    if (decisionUnlinked) { decisionUnlinked = false; record("decision_added", decision); }
    return send(201, { decision: { decision_id: decision, case_id: id } });
  }
  if (path === `cases/${id}/decision-transfers`) {
    if (!transfersEnabled || data.to_case_id !== transferTarget) return send(403, { error: "You cannot access this destination." });
    if (!data.reason?.trim() || data.decision_id !== decision || !data.operation_id) return send(400, { error: "Invalid decision transfer" });
    if (!operations.has(data.operation_id)) { operations.add(data.operation_id); decisionTransferred = true; record("decision_transferred_out", data.reason); }
    if (transferFailAck) { transferFailAck = false; return send(503, { error: "Acknowledgment unavailable. Retry the transfer." }); }
    return send(204);
  }
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
  if (path === `cases/${id}/close`) {
    if (!data.comment?.trim() || Buffer.byteLength(data.comment) > 10000 || !["confirmed_risk", "false_positive", "valuable_alert"].includes(data.outcome)) return send(400, { error: "Closing findings and outcome required" });
    if (item.status === "closed") return send(409, { error: "request conflicts with current state" });
    item.status = "closed"; item.outcome = data.outcome; record("status_updated", data.comment.trim()); return send(200, { case: item });
  }
  if (path === `cases/${id}/comments`) { if (data.comment === "fail write") return send(503, { error: "Comment could not be saved. Please retry." }); record("comment_created", data.comment); return send(201, { comment: events[0] }); }
  if (path === `cases/${id}` && req.method === "PATCH") {
    if (data.status === "closed" || (data.outcome !== undefined && !(item.status === "closed" && data.status === "investigating" && data.outcome === "unset"))) return send(400, { error: "Use lifecycle actions" });
    if (item.status === "closed" && data.status === "investigating") item.outcome = "unset";
    Object.assign(item, data); record("status_updated", "Case updated"); return send(200, { case: item });
  }
  send(404, { error: "Fixture endpoint not implemented" });
}).listen(3199, "127.0.0.1");
