import { platformSession } from "@/lib/server/auth";
import { caseFetch, failure, relay } from "@/lib/server/case-proxy";
import { screeningEvidence } from "@/lib/server/screening-evidence";

export async function GET(_request: Request, context: { params: Promise<{ tenant: string; caseId: string; kind: string; resource: string }> }) {
  const { tenant, caseId, kind, resource } = await context.params;
  if (![tenant, caseId, resource].every(id => /^[0-9a-f-]{36}$/i.test(id)) || !["decisions", "screenings"].includes(kind)) return failure("Invalid evidence reference.", 400);
  const access = await caseFetch(`tenants/${tenant}/cases/${caseId}/evidence-access/${kind}/${resource}`);
  if (access.status !== 204) return relay(access);
  const base = kind === "decisions" ? process.env.DECISION_ENGINE_SERVICE_URL : process.env.SCREENING_SERVICE_URL;
  if (!base) return failure("The evidence service is not configured.", 503);
  const identity = await platformSession();
  if (!identity) return failure("Sign in required.",401);
  if (identity.session.tenant_id !== tenant) return failure("Tenant access denied.",403);
  try {
    const headers = { Authorization: `Bearer ${identity.access_token}` };
    const response = await fetch(`${base.replace(/\/$/, "")}/v1/tenants/${tenant}/cases/${caseId}/evidence/${kind}/${resource}`, {
      headers, cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(15000),
    });
    if (kind === "screenings") {
      if (!response.ok) return failure("Screening evidence is unavailable.", response.status >= 400 && response.status < 500 ? response.status : 503);
      return Response.json(screeningEvidence(await response.json(), resource), { headers: { "Cache-Control": "private, no-store" } });
    }
    return relay(response);
  } catch { return failure("Evidence is temporarily unavailable. Please retry.", 503); }
}
