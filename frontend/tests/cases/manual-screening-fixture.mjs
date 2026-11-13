// Contract-only owner stub: no database, JWT verification or provider execution.
import { randomUUID } from "node:crypto";
const runs = new Map(), operations = new Map(), lost = new Set();
let historyError = false;
export async function handleManualScreening(req, url, send) {
  if (url.pathname === "/reset") { runs.clear(); operations.clear(); lost.clear(); historyError = false; return false; }
  if (url.pathname === "/fixture/manual-history-error") { historyError = true; send(204); return true; }
  if (url.pathname === "/fixture/manual-history") { for (let i = 0; i < 25; i++) runs.set(randomUUID(), { input: { name: `Historical ${String(i).padStart(2, "0")}`, entity_type: i === 24 ? "Organization" : "Person", threshold: 70, limit: 10 }, created_at: new Date(Date.UTC(2026, 0, i + 1)).toISOString() }); send(204); return true; }
  if (url.pathname === "/v1/tenants/11111111-1111-4111-8111-111111111111/manual-screening-investigations") {
    if (req.headers.authorization !== "Bearer fixture-analyst") { send(403, { error: "Access denied" }); return true; }
    let raw = ""; for await (const chunk of req) raw += chunk;
    const body = JSON.parse(raw);
    if (!runs.has(body.screening_id) || !body.candidate_id || body.inbox_id !== "22222222-2222-4222-8222-222222222222") { send(404, { error: "Unavailable investigation" }); return true; }
    send(200, { case: { id: "33333333-3333-4333-8333-333333333333" } }); return true;
  }
  if (!url.pathname.startsWith("/v1/manual-screenings")) return false;
  if (req.headers.authorization !== "Bearer fixture-analyst") { send(403, { error: "Investigator access denied." }); return true; }
  if (url.pathname === "/v1/manual-screenings/capabilities") { send(200, { id: "opensanctions", name: "OpenSanctions", configured: true, health: "unknown", dataset_selection: true, entity_types: ["Person", "Organization"], threshold_min: 0, threshold_max: 100, limit_max: 50, fields: { Person: ["name", "birth_date", "nationality", "address"], Organization: ["name", "country", "registration_number", "address"] }, limitations: ["Dataset access is checked by the provider."] }); return true; }
  if (req.method === "GET" && url.pathname === "/v1/manual-screenings") {
    if (historyError) { historyError = false; send(503, { error: "History unavailable." }); return true; }
    let items = [...runs].map(([id, run]) => ({ id, name: run.input.name, entity_type: run.input.entity_type, status: run.input.name === "Failure" ? "failed" : run.input.name === "Nobody" ? "no_hit" : "awaiting_review", partial: run.input.name === "Partial", created_at: run.created_at ?? "2026-10-19T12:00:00Z" })).sort((a,b) => b.created_at.localeCompare(a.created_at) || b.id.localeCompare(a.id));
    for (const key of ["status", "entity_type"]) if (url.searchParams.get(key)) items = items.filter(item => item[key] === url.searchParams.get(key));
    if (url.searchParams.get("from")) items = items.filter(item => item.created_at.slice(0,10) >= url.searchParams.get("from"));
    if (url.searchParams.get("to")) items = items.filter(item => item.created_at.slice(0,10) <= url.searchParams.get("to"));
    const start = Number(url.searchParams.get("cursor") || 0), limit = Number(url.searchParams.get("limit") || 20);
    send(200, { items: items.slice(start,start+limit), next_cursor: items.length > start+limit ? String(start+limit) : "" }); return true;
  }
  if (req.method === "POST") {
    let raw = ""; for await (const chunk of req) raw += chunk;
    const input = JSON.parse(raw);
    if (!input.name || !["Person", "Organization"].includes(input.entity_type) || input.threshold < 0 || input.threshold > 100 || (!Number.isInteger(input.limit) || input.limit < 1 || input.limit > 50) || !input.operation_id || input.tenant_id) { send(400, { error: "Incorrect search contract" }); return true; }
    if (input.name === "Denied") { send(403, { error: "Investigator access denied." }); return true; }
    let id = operations.get(input.operation_id);
    if (id && JSON.stringify(runs.get(id).input) !== JSON.stringify(input)) { send(409, { error: "Operation changed." }); return true; }
    if (!id) { id = randomUUID(); operations.set(input.operation_id, id); runs.set(id, { input }); }
    if (input.name === "Lost response" && !lost.has(input.operation_id)) { lost.add(input.operation_id); send(503, { error: "Response lost. Retry the same request." }); return true; }
    send(202, { id, status: "pending" }); return true;
  }
  const id = url.pathname.split("/").at(-1), run = runs.get(id);
  if (!run) { send(404, { error: "Search not found." }); return true; }
  const name = run.input.name;
  const matches = ["Nobody", "Partial", "Failure"].includes(name) ? [] : [
    { id: "m1", status: "pending", entity_id: "provider-1", name: "Candidate One", score: .94, schema: run.input.entity_type, datasets: ["Test sanctions"], properties: { country: ["gb"], birthDate: ["1980-01-01"], alias: ["Other name"] } },
    { id: "m2", status: "no_hit", entity_id: "provider-2", name: "Candidate Two", score: .8, schema: run.input.entity_type, datasets: [], properties: {} },
  ];
  send(200, { id, provider: "opensanctions", status: name === "Failure" ? "failed" : (matches.length || name === "Partial") ? "awaiting_review" : "no_hit", partial: name === "Partial", matches, query: run.input, created_at: run.created_at ?? "2026-10-19T12:00:00Z" });
  return true;
}
