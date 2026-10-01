import { caseFetch, failure, relay } from "@/lib/server/case-proxy";

export async function GET(_request: Request, context: { params: Promise<{ tenant: string; caseId: string; kind: string; resource: string }> }) {
  const { tenant, caseId, kind, resource } = await context.params;
  if (![tenant, caseId, resource].every(id => /^[0-9a-f-]{36}$/i.test(id)) || !["decisions", "screenings"].includes(kind)) return failure("Invalid evidence reference.", 400);
  const access = await caseFetch(`tenants/${tenant}/cases/${caseId}/evidence-access/${kind}/${resource}`);
  if (access.status !== 204) return relay(access);
  const base = kind === "decisions" ? process.env.DECISION_ENGINE_SERVICE_URL : process.env.SCREENING_SERVICE_URL;
  if (!base) return failure("The evidence service is not configured.", 503);
  const token = kind === "decisions" ? process.env.DECISION_ENGINE_AUTH_TOKEN : process.env.SCREENING_AUTH_TOKEN;
  try {
    return relay(await fetch(`${base.replace(/\/$/, "")}/v1/tenants/${tenant}/${kind}/${resource}`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {}, cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(15000),
    }));
  } catch { return failure("Evidence is temporarily unavailable. Please retry.", 503); }
}
