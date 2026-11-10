import { caseFetch, failure, relay } from "@/lib/server/case-proxy";
import { screeningEvidence } from "@/lib/server/screening-evidence";

export async function GET(request: Request, context: { params: Promise<{ tenant: string; caseId: string; kind: string; resource: string }> }) {
  const { tenant, caseId, kind, resource } = await context.params;
  if (![tenant, caseId, resource].every(id => /^[0-9a-f-]{36}$/i.test(id)) || !["decisions", "screenings"].includes(kind)) return failure("Invalid evidence reference.", 400);
  const access = await caseFetch(`tenants/${tenant}/cases/${caseId}/evidence-access/${kind}/${resource}`);
  if (access.status !== 204) return relay(access);
  const base = kind === "decisions" ? process.env.DECISION_ENGINE_SERVICE_URL : process.env.SCREENING_SERVICE_URL;
  if (!base) return failure("The evidence service is not configured.", 503);
  const token = kind === "decisions" ? process.env.DECISION_ENGINE_AUTH_TOKEN : process.env.SCREENING_AUTH_TOKEN;
  const operator = kind === "screenings" ? process.env.SCREENING_OPERATOR_TOKEN : undefined;
  if (kind === "screenings" && (!operator || operator.trim().length < 32 || operator === token)) return failure("Screening evidence is not configured.", 503);
  try {
    const headers: Record<string, string> = token ? { Authorization: `Bearer ${token}` } : {};
    if (operator) headers["X-Screening-Operator-Token"] = operator;
    const response = await fetch(`${base.replace(/\/$/, "")}/v1/tenants/${tenant}/${kind}/${resource}`, {
      headers, cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(15000),
    });
    if (kind === "screenings") {
      if (!response.ok) return failure("Screening evidence is unavailable.", response.status >= 400 && response.status < 500 ? response.status : 503);
      return Response.json(screeningEvidence(await response.json(), resource), { headers: { "Cache-Control": "private, no-store" } });
    }
    if (!response.ok) return relay(response);
    const data = await response.json();
    const decision = data.decision;
    if (decision?.id !== resource) return failure("Invalid decision evidence.", 502);
    const uuid = /^[0-9a-f-]{36}$/i;
    if (uuid.test(decision.scenario_id ?? "")) {
      const parent = `${base.replace(/\/$/, "")}/v1/tenants/${tenant}/scenarios/${decision.scenario_id}`;
      const scenario = await fetch(parent, { headers, cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(10000) });
      if (scenario.ok) decision.scenario_name = (await scenario.json()).scenario?.name;
      if (new URL(request.url).searchParams.get("rules") === "1" && uuid.test(decision.scenario_iteration_id ?? "")) {
        const rules = await fetch(`${parent}/iterations/${decision.scenario_iteration_id}/rules`, { headers, cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(10000) });
        if (!rules.ok) return failure("Rule evidence is unavailable.", 503);
        data.rules = (await rules.json()).rules;
      }
    }
    return Response.json(data, { headers: { "Cache-Control": "private, no-store" } });
  } catch { return failure("Evidence is temporarily unavailable. Please retry.", 503); }
}
