import { platformSession, privateHeaders } from "@/lib/server/auth";

export async function GET(_request: Request, context: { params: Promise<{ decisionId: string }> }) {
  const fail = (status: number, error: string) => Response.json({ error }, { status, headers: privateHeaders });
  try {
    const identity = await platformSession();
    if (!identity) return fail(401, "Sign in required.");
    const { decisionId } = await context.params;
    if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(decisionId)) return fail(400, "Invalid decision.");
    const base = process.env.DECISION_ENGINE_SERVICE_URL, token = process.env.SCREENING_AUTHORING_TOKEN;
    if (!base || !token || token.length < 32) return fail(503, "Screening access is unavailable.");
    const s = identity.session;
    const response = await fetch(`${base.replace(/\/$/, "")}/internal/screening-authoring/tenants/${encodeURIComponent(s.tenant_id)}/decisions/${decisionId}/screenings`, {
      headers: { Authorization: `Bearer ${token}`, "X-Screening-User": s.user_id, "X-Screening-Session": s.id, "X-Screening-Version": String(s.version) },
      cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(10000),
    });
    if (!response.ok) return fail([401,403].includes(response.status) ? 403 : 503, "Screening results are unavailable.");
    return Response.json(await response.json(), { headers: privateHeaders });
  } catch { return fail(503, "Screening results are unavailable."); }
}
