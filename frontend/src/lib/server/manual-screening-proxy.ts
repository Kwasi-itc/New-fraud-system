import "server-only";
import { platformSession } from "./auth";
import { failure, relay } from "./case-proxy";

export async function manualScreeningProxy(request: Request, id?: string) {
  if (id && id !== "capabilities" && !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id)) return failure("Invalid search identifier.", 400);
  let body: string | undefined;
  if (request.method === "POST") {
    if (request.headers.get("origin") !== (process.env.APP_ORIGIN ?? new URL(request.url).origin)) return failure("Invalid request origin.", 403);
    if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) return failure("Expected application/json.", 415);
    const reader = request.body?.getReader(); const chunks: Uint8Array[] = []; let size = 0;
    if (reader) for (;;) {
      const { value, done } = await reader.read(); if (done) break;
      size += value.byteLength;
      if (size > 4096) { await reader.cancel(); return failure("Search request is too large.", 413); }
      chunks.push(value);
    }
    body = Buffer.concat(chunks).toString("utf8");
  }
  try {
    const session = await platformSession();
    if (!session) return failure("Sign in to screen entities.", 401);
    if (!session.session.roles.some(role => ["case_investigator", "case_manager_admin"].includes(role))) return failure("An investigator role is required.", 403);
    const base = process.env.SCREENING_SERVICE_URL;
    if (!base) return failure("Screening is not configured.", 503);
    const query = request.method === "GET" && !id ? new URL(request.url).search : "";
    return await relay(await fetch(`${base.replace(/\/$/, "")}/v1/manual-screenings${id ? `/${id}` : ""}${query}`, {
      method: request.method, body, headers: { Authorization: `Bearer ${session.access_token}`, "Content-Type": "application/json" },
      cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(15000),
    }));
  } catch { return failure("Search response unavailable. Retry to check the same search.", 503); }
}
