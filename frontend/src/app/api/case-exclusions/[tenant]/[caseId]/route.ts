import { caseToken, failure, relay } from "@/lib/server/case-proxy";

export async function GET(request: Request, context: { params: Promise<{ tenant: string; caseId: string }> }) {
  const { tenant, caseId } = await context.params;
  if (![tenant, caseId].every(id => /^[0-9a-f-]{36}$/i.test(id))) return failure("Invalid case.", 400);
  const token = await caseToken();
  if (!token) return failure("Investigator sign-in is required.", 401);
  const base = process.env.SCREENING_SERVICE_URL;
  if (!base) return failure("Screening is not configured.", 503);
  const input = new URL(request.url).searchParams;
  const query = new URLSearchParams();
  for (const key of ["cursor", "operation_id", "limit"]) {
    const value = input.get(key);
    if (value !== null) query.set(key, value);
  }
  if (query.toString().length > 4096) return failure("History query is too large.", 400);
  try {
    return relay(await fetch(`${base.replace(/\/$/, "")}/v1/investigator/tenants/${tenant}/cases/${caseId}/exclusions?${query}`, {
      headers: { Authorization: `Bearer ${token}` }, cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(15000),
    }));
  } catch { return failure("Exclusion history is unavailable. Please retry.", 503); }
}
