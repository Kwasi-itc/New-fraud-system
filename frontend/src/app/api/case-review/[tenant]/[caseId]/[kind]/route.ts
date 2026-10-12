import { caseToken, failure, relay } from "@/lib/server/case-proxy";

type Context = { params: Promise<{ tenant: string; caseId: string; kind: string }> };
async function handle(request: Request, context: Context) {
  const { tenant, caseId, kind } = await context.params;
  if (![tenant, caseId].every(id => /^[0-9a-f-]{36}$/i.test(id)) || !["decisions", "matches"].includes(kind)) return failure("Invalid review resource.", 400);
  const token = await caseToken();
  if (!token) return failure("Investigator sign-in is required.", 401);
  const url = new URL(request.url);
  let body: string | undefined;
  if (request.method === "POST") {
    if (request.headers.get("origin") !== (process.env.APP_ORIGIN ?? url.origin)) return failure("Invalid request origin.", 403);
    if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) return failure("Expected application/json.", 415);
    const reader = request.body?.getReader(); const chunks: Uint8Array[] = []; let size = 0;
    if (reader) for (;;) { const { value, done } = await reader.read(); if (done) break; size += value.byteLength; if (size > 16384) { await reader.cancel(); return failure("Review is too large.", 413); } chunks.push(value); }
    body = Buffer.concat(chunks).toString("utf8");
  }
  const base = kind === "decisions" ? process.env.DECISION_ENGINE_SERVICE_URL : process.env.SCREENING_SERVICE_URL;
  if (!base) return failure("The review service is not configured.", 503);
  const query = new URLSearchParams({ resource_id: url.searchParams.get("resource_id") ?? "" });
  try {
    return relay(await fetch(`${base.replace(/\/$/, "")}/v1/investigator/tenants/${tenant}/cases/${caseId}/review?${query}`, {
      method: request.method, body, headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
      cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(15000),
    }));
  } catch { return failure("Review response unavailable. Retry the same operation to check whether it committed.", 503); }
}
export { handle as GET, handle as POST };
