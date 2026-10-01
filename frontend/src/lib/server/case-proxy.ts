import "server-only";
import { cookies } from "next/headers";

export const privateHeaders = { "Cache-Control": "private, no-store" };
export function failure(message: string, status: number) {
  return Response.json({ error: message }, { status, headers: privateHeaders });
}

export async function caseToken() {
  return (await cookies()).get(process.env.CASE_SESSION_COOKIE ?? "__Host-case_session")?.value;
}

export async function caseFetch(path: string, init: RequestInit = {}, contentType = "application/json") {
  const token = await caseToken();
  if (!token) return failure("Sign in with your investigator account to access cases.", 401);
  const base = process.env.CASE_MANAGER_SERVICE_URL;
  if (!base) return failure("Case management is not configured.", 503);
  try {
    return await fetch(`${base.replace(/\/$/, "")}/v1/${path}`, {
      ...init, headers: { "Content-Type": contentType, Authorization: `Bearer ${token}` },
      cache: "no-store", redirect: "manual", signal: AbortSignal.timeout(15000),
    });
  } catch { return failure("Case service is unavailable. Please try again.", 503); }
}

export async function relay(response: Response) {
  if (response.status >= 300 && response.status < 400) return failure("Unexpected service redirect.", 502);
  return new Response(response.status === 204 ? null : await response.text(), {
    status: response.status, headers: { ...privateHeaders, "Content-Type": "application/json" },
  });
}
