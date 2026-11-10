import type { decisionEngineApi } from "./decision-engine-api";

export async function caseDecisionEvidence(tenant: string, caseId: string, decisionId: string) {
  const response = await fetch(`/api/case-evidence/${encodeURIComponent(tenant)}/${encodeURIComponent(caseId)}/decisions/${encodeURIComponent(decisionId)}`, { cache: "no-store" });
  const body = await response.json();
  if (!response.ok) throw new Error(typeof body.error === "string" ? body.error : "Decision evidence is unavailable.");
  type Evidence = Awaited<ReturnType<typeof decisionEngineApi.getDecision>>;
  return body as Evidence & { decision: Evidence["decision"] & { scenario_name?: string } };
}
