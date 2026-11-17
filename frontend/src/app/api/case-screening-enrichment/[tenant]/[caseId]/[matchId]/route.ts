import { platformSession } from "@/lib/server/auth";
import { failure, relay } from "@/lib/server/case-proxy";

export async function POST(request: Request, context: { params: Promise<{ tenant: string; caseId: string; matchId: string }> }) {
  const { tenant, caseId, matchId } = await context.params;
  if (![tenant, caseId].every(id => /^[0-9a-f-]{36}$/i.test(id)) || !/^[A-Za-z0-9_-]{1,200}$/.test(matchId)) return failure("Invalid evidence reference.", 400);
  if (request.headers.get("origin") !== (process.env.APP_ORIGIN ?? new URL(request.url).origin)) return failure("Invalid request origin.", 403);
  if (!(request.headers.get("content-type") ?? "").startsWith("application/json")) return failure("Expected application/json.", 415);
  const identity = await platformSession();
  if (!identity) return failure("Investigator sign-in is required.", 401);
  if (identity.session.tenant_id !== tenant) return failure("Current case access is required.", 403);
  const base = process.env.SCREENING_SERVICE_URL;
  if (!base) return failure("Screening is not configured.", 503);
  try {
    return relay(await fetch(`${base.replace(/\/$/, "")}/v1/investigator/tenants/${tenant}/cases/${caseId}/matches/${encodeURIComponent(matchId)}/enrich`, {
      method: "POST", body: "{}", headers: { Authorization: `Bearer ${identity.access_token}`, "Content-Type": "application/json" },
      cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(15000),
    }));
  } catch { return failure("Source details are temporarily unavailable.", 503); }
}
