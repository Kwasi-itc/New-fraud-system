import { caseToken, failure, relay } from "@/lib/server/case-proxy";

type Context = { params: Promise<{ tenant: string; caseId: string; screeningId: string }> };
async function handle(request: Request, context: Context) {
  const { tenant, caseId, screeningId } = await context.params;
  if (![tenant, caseId, screeningId].every(id => /^[0-9a-f-]{36}$/i.test(id))) return failure("Invalid screening context.", 400);
  if (request.method === "POST") {
    if (request.headers.get("origin") !== (process.env.APP_ORIGIN ?? new URL(request.url).origin)) return failure("Invalid request origin.", 403);
    if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) return failure("Expected application/json.", 415);
  }
  const token = await caseToken();
  if (!token) return failure("Sign in to refine screening results.", 401);
  const base = process.env.SCREENING_SERVICE_URL;
  if (!base) return failure("Screening is not configured.", 503);
  let body: string | undefined;
  if (request.method === "POST") {
    const reader = request.body?.getReader(); if (!reader) return failure("A refinement command is required.", 400);
    const chunks: Uint8Array[] = []; let size = 0;
    while (true) { const { done, value } = await reader.read(); if (done) break; size += value.byteLength; if (size > 8192) { await reader.cancel(); return failure("Refinement is too large.", 413); } chunks.push(value); }
    body = Buffer.concat(chunks).toString("utf8");
  }
  try { return relay(await fetch(`${base.replace(/\/$/, "")}/v1/investigator/tenants/${encodeURIComponent(tenant)}/cases/${encodeURIComponent(caseId)}/screenings/${encodeURIComponent(screeningId)}/refinement`, {
    method: request.method, body, headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" }, cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(15000),
  })); } catch { return failure("Refinement response unavailable. Retry the same operation to check whether it committed.", 503); }
}
export { handle as GET, handle as POST };
